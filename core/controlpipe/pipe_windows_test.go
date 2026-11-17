package controlpipe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	winio "github.com/tailscale/go-winio"
	"golang.org/x/sys/windows"
)

func testName() string { var b [8]byte; rand.Read(b[:]); return "test-" + hex.EncodeToString(b[:]) }
func testIdentity(t *testing.T) Identity {
	t.Helper()
	id, e := CurrentIdentity()
	if e != nil || !validIdentity(id) {
		t.Fatal("kernel identity unavailable", e)
	}
	return id
}
func testServer(t *testing.T, policy Identity, h Handler) (string, *Server) {
	t.Helper()
	name := testName()
	s, e := Listen(name, policy, map[string]Handler{"status": h})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		s.Close()
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, context.Canceled) {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not exit")
		}
	})
	return name, s
}
func TestWindowsPipeAuthenticatesKernelIdentity(t *testing.T) {
	id := testIdentity(t)
	var calls atomic.Int32
	name, s := testServer(t, id, func(ctx context.Context, peer Identity, payload json.RawMessage) (json.RawMessage, error) {
		if peer != id {
			t.Error("wrong kernel identity")
		}
		calls.Add(1)
		return json.RawMessage(`{"state":"unconfigured"}`), nil
	})
	if second, e := Listen(name, id, nil); e == nil {
		second.Close()
		t.Fatal("duplicate pipe owner accepted")
	}
	c, e := Dial(context.Background(), name, id.UserSID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		b, e := c.Call(context.Background(), "status", nil)
		if e != nil || string(b) != `{"state":"unconfigured"}` {
			t.Fatal(string(b), e)
		}
	}
	if calls.Load() != 3 {
		t.Fatal(calls.Load())
	}
	if _, e = c.Call(context.Background(), "run-shell", json.RawMessage(`"whoami"`)); e == nil || e.Error() != "unknown_operation" {
		t.Fatal("unknown operation", e)
	}
	if calls.Load() != 3 {
		t.Fatal("unknown operation executed")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Call(context.Background(), "status", nil); e == nil {
		t.Fatal("call after service close")
	}
}
func TestWindowsPipeRejectsAnonymousAndWrongLogon(t *testing.T) {
	id := testIdentity(t)
	for _, anonymous := range []bool{true, false} {
		t.Run(map[bool]string{true: "anonymous", false: "wrong-logon"}[anonymous], func(t *testing.T) {
			policy := id
			if !anonymous {
				policy.LogonSID = "S-1-5-5-0-1"
				if policy.LogonSID == id.LogonSID {
					policy.LogonSID = "S-1-5-5-0-2"
				}
			}
			var calls atomic.Int32
			name, _ := testServer(t, policy, func(context.Context, Identity, json.RawMessage) (json.RawMessage, error) {
				calls.Add(1)
				return nil, nil
			})
			level := winio.PipeImpLevelIdentification
			if anonymous {
				level = winio.PipeImpLevelAnonymous
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, e := winio.DialPipeAccessImpLevel(ctx, pipePath(name), clientAccess, level)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			if e = writeFrame(c, Request{Version: Version, ID: 1, Operation: "status"}); e != nil {
				t.Fatal(e)
			}
			e = readFrame(c, new(Response))
			if e == nil {
				t.Fatal("unauthenticated request succeeded")
			}
			var timeout net.Error
			if errors.As(e, &timeout) && timeout.Timeout() {
				t.Fatal("rejection timed out")
			}
			if calls.Load() != 0 {
				t.Fatal("unauthenticated handler executed")
			}
		})
	}
}
func TestWindowsClientPinsServerBeforeSending(t *testing.T) {
	id := testIdentity(t)
	var calls atomic.Int32
	name, _ := testServer(t, id, func(context.Context, Identity, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return nil, nil
	})
	wrong := "S-1-5-18"
	if id.UserSID == wrong {
		wrong = "S-1-5-19"
	}
	if c, e := Dial(context.Background(), name, wrong); e == nil {
		c.Close()
		t.Fatal("accepted wrong server identity")
	}
	if calls.Load() != 0 {
		t.Fatal("payload reached untrusted server")
	}
	if clientAccess&4 != 0 {
		t.Fatal("client can create pipe instances")
	}
	for _, name := range []string{`\\other\pipe\x`, `bad/name`, `UPPER`, `../x`, ""} {
		if c, e := Dial(context.Background(), name, id.UserSID); e == nil {
			c.Close()
			t.Fatal("unsafe pipe name")
		}
	}
}
func TestWindowsPipeCancelClosesBlockedRequest(t *testing.T) {
	id := testIdentity(t)
	entered := make(chan struct{})
	exited := make(chan struct{})
	name, s := testServer(t, id, func(ctx context.Context, _ Identity, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	c, e := Dial(context.Background(), name, id.UserSID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { _, e := c.Call(context.Background(), "status", nil); done <- e }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not enter")
	}
	s.Close()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("service close did not cancel handler")
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("stopped request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("client remained blocked")
	}
}

func TestWindowsPipeChild(t *testing.T) {
	name := os.Getenv("XDIAL_CONTROLPIPE_CHILD")
	if name == "" {
		t.Skip("child only")
	}
	id := testIdentity(t)
	c, e := Dial(context.Background(), name, id.UserSID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, e = c.Call(context.Background(), "status", json.RawMessage(`{"probe":"child"}`)); e != nil {
		t.Fatal(e)
	}
}
func TestWindowsPipeSeparateProcessAndACL(t *testing.T) {
	id := testIdentity(t)
	var calls atomic.Int32
	name, _ := testServer(t, id, func(_ context.Context, peer Identity, payload json.RawMessage) (json.RawMessage, error) {
		if peer != id || string(payload) != `{"probe":"child"}` {
			t.Error("bad child identity/payload")
		}
		calls.Add(1)
		return json.RawMessage(`{"accepted":true}`), nil
	})
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWindowsPipeChild$")
	cmd.Env = append(os.Environ(), "XDIAL_CONTROLPIPE_CHILD="+name)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("separate client: %v %s", e, b)
	}
	if calls.Load() != 1 {
		t.Fatal("child request missing")
	}
	// Read back the actual kernel DACL, not just the SDDL string we supplied.
	c, e := winio.DialPipeAccessImpLevel(ctx, pipePath(name), clientAccess|windows.READ_CONTROL, winio.PipeImpLevelIdentification)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	f := c.(interface{ Fd() uintptr })
	sd, e := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		t.Fatal("missing DACL", e)
	}
	found := false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &ace); e != nil {
			t.Fatal(e)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if sid == id.LogonSID {
			found = true
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || uint32(ace.Mask) != clientAccess || uint32(ace.Mask)&4 != 0 {
				t.Fatal("overbroad client DACL")
			}
		}
		if sid != id.LogonSID && sid != id.UserSID {
			t.Fatal("unexpected DACL principal")
		}
	}
	if !found {
		t.Fatal("missing logon-specific ACE")
	}
}
func TestWindowsPipeRejectsReplay(t *testing.T) {
	id := testIdentity(t)
	var calls atomic.Int32
	name, _ := testServer(t, id, func(context.Context, Identity, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return nil, nil
	})
	c, e := winio.DialPipeAccessImpLevel(context.Background(), pipePath(name), clientAccess, winio.PipeImpLevelIdentification)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 2; i++ {
		if e = writeFrame(c, Request{Version: Version, ID: 1, Operation: "status"}); e != nil {
			t.Fatal(e)
		}
		var r Response
		if e = readFrame(c, &r); e != nil {
			t.Fatal(e)
		}
		want := "ok"
		if i == 1 {
			want = "invalid_request"
		}
		if r.Code != want {
			t.Fatal(r.Code)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("replayed request executed")
	}
}

func TestWindowsClientCancelsQueuedCall(t *testing.T) {
	id := testIdentity(t)
	entered := make(chan struct{})
	name, s := testServer(t, id, func(ctx context.Context, _ Identity, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	c, e := Dial(context.Background(), name, id.UserSID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { _, e := c.Call(context.Background(), "status", nil); done <- e }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first call not active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, e = c.Call(ctx, "status", nil)
	if !errors.Is(e, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("queued cancellation waited for active call", e)
	}
	s.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active call did not close")
	}
}
