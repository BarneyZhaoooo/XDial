package controlpipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	winio "github.com/tailscale/go-winio"
	"golang.org/x/sys/windows"
)

const clientAccess = windows.FILE_READ_DATA | windows.FILE_WRITE_DATA | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
const ioTimeout = 5 * time.Second

var impersonatePipe = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")

// Handler runs as the service identity, after peer authentication AND successful
// RevertToSelf. It must honor ctx, perform its own state/CAS checks and avoid
// logging payloads. Only operations explicitly registered at Listen are exposed.
type Handler func(context.Context, Identity, json.RawMessage) (json.RawMessage, error)

type Server struct {
	listener    net.Listener
	policy      Identity
	handlers    map[string]Handler
	mu          sync.Mutex
	closed      bool
	serving     bool
	cancel      context.CancelFunc
	connections map[*pipeConn]struct{}
	wg          sync.WaitGroup
}
type pipeConn struct {
	net.Conn
	mu     sync.Mutex
	closed bool
}

func (c *pipeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.Conn.Close()
}

func validateName(name string) error {
	if len(name) < 1 || len(name) > 100 {
		return fmt.Errorf("invalid pipe name")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return fmt.Errorf("invalid pipe name")
		}
	}
	return nil
}
func pipePath(name string) string { return `\\.\pipe\xdial.` + name }
func validSID(s string) bool      { v, e := windows.StringToSid(s); return e == nil && v.String() == s }
func validIdentity(p Identity) bool {
	return validSID(p.UserSID) && validSID(p.LogonSID) && strings.HasPrefix(p.LogonSID, "S-1-5-5-")
}

func tokenIdentity(token windows.Token) (Identity, error) {
	u, e := token.GetTokenUser()
	if e != nil {
		return Identity{}, e
	}
	g, e := token.GetTokenGroups()
	if e != nil {
		return Identity{}, e
	}
	id := Identity{UserSID: u.User.Sid.String()}
	for _, group := range g.AllGroups() {
		if group.Attributes&windows.SE_GROUP_LOGON_ID == windows.SE_GROUP_LOGON_ID {
			id.LogonSID = group.Sid.String()
			break
		}
	}
	return id, nil
}

// CurrentIdentity returns an installer/interactive process's kernel identity.
// Do not replace installer-owned admission policy with a client's claimed SID.
func CurrentIdentity() (Identity, error) { return tokenIdentity(windows.GetCurrentProcessToken()) }

func Listen(name string, policy Identity, handlers map[string]Handler) (*Server, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if !validIdentity(policy) {
		return nil, fmt.Errorf("explicit user and logon SID policy required")
	}
	server, e := CurrentIdentity()
	if e != nil {
		return nil, e
	}
	// Individual rights deliberately omit FILE_CREATE_PIPE_INSTANCE (bit 4).
	// Never use GENERIC_WRITE for the unprivileged client ACE.
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)(A;;0x%x;;;%s)", server.UserSID, clientAccess, policy.LogonSID)
	l, e := winio.ListenPipe(pipePath(name), &winio.PipeConfig{SecurityDescriptor: sddl, InputBufferSize: MaxFrame, OutputBufferSize: MaxFrame})
	if e != nil {
		return nil, e
	}
	h := make(map[string]Handler, len(handlers))
	for op, handler := range handlers {
		if op == "" || handler == nil {
			l.Close()
			return nil, fmt.Errorf("invalid registered operation")
		}
		h[op] = handler
	}
	return &Server{listener: l, policy: policy, handlers: h, connections: make(map[*pipeConn]struct{})}, nil
}

func (c *pipeConn) authenticate() (Identity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Identity{}, net.ErrClosed
	}
	f, ok := c.Conn.(interface{ Fd() uintptr })
	if !ok {
		return Identity{}, fmt.Errorf("not a native pipe")
	}
	handle := windows.Handle(f.Fd())
	type result struct {
		identity Identity
		err      error
	}
	done := make(chan result, 1)
	// An isolated locked goroutine lets Go destroy the OS thread if reverting
	// fails. Such a thread must never rejoin the runtime or run a handler.
	go func() {
		runtime.LockOSThread()
		r, _, err := impersonatePipe.Call(uintptr(handle))
		if r == 0 {
			runtime.UnlockOSThread()
			done <- result{err: fmt.Errorf("pipe identity unavailable: %w", err)}
			return
		}
		var token windows.Token
		err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token)
		var id Identity
		if err == nil {
			var level, size uint32
			err = windows.GetTokenInformation(token, windows.TokenImpersonationLevel, (*byte)(unsafe.Pointer(&level)), 4, &size)
			if err == nil && level < windows.SecurityIdentification {
				err = fmt.Errorf("anonymous pipe identity")
			}
			if err == nil {
				id, err = tokenIdentity(token)
			}
			token.Close()
		}
		if revert := windows.RevertToSelf(); revert != nil {
			done <- result{err: fmt.Errorf("pipe identity restoration failed")}
			return
		}
		runtime.UnlockOSThread()
		done <- result{id, err}
	}()
	r := <-done
	return r.identity, r.err
}

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.closed || s.serving {
		s.mu.Unlock()
		return fmt.Errorf("control server already serving or closed")
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.serving = true
	s.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	defer func() { s.Close(); s.wg.Wait() }()
	for {
		raw, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return err
		}
		c := &pipeConn{Conn: raw}
		s.mu.Lock()
		if s.closed || len(s.connections) >= 32 {
			s.mu.Unlock()
			c.Close()
			continue
		}
		s.connections[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serveConnection(ctx, c)
	}
}
func (s *Server) serveConnection(ctx context.Context, c *pipeConn) {
	defer s.wg.Done()
	defer func() { c.Close(); s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
	var lastID uint64
	for {
		if err := c.SetReadDeadline(time.Now().Add(ioTimeout)); err != nil {
			return
		}
		var request Request
		if err := readFrame(c, &request); err != nil {
			return
		}
		// Impersonation is defined for the last message READ, so it must happen
		// here on every frame, not before reading or merely once per connection.
		peer, err := c.authenticate()
		if err != nil || peer != s.policy {
			return
		}
		if ctx.Err() != nil {
			return
		}
		response := Response{Version: Version, ID: request.ID}
		handler, known := s.handlers[request.Operation]
		if request.Version != Version || request.ID == 0 || request.ID <= lastID {
			response.Code = "invalid_request"
		} else if !known {
			lastID = request.ID
			response.Code = "unknown_operation"
		} else {
			lastID = request.ID
			callCtx, cancel := context.WithTimeout(ctx, ioTimeout)
			payload, err := handler(callCtx, peer, request.Payload)
			cancel()
			if err != nil {
				response.Code = "request_failed"
			} else {
				response.Code = "ok"
				response.Payload = payload
			}
		}
		if err := c.SetWriteDeadline(time.Now().Add(ioTimeout)); err != nil {
			return
		}
		if err := writeFrame(c, response); err != nil {
			return
		}
	}
}
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	connections := make([]*pipeConn, 0, len(s.connections))
	for c := range s.connections {
		connections = append(connections, c)
	}
	s.mu.Unlock()
	err := s.listener.Close()
	for _, c := range connections {
		c.Close()
	}
	return err
}

type Client struct {
	conn *pipeConn
	gate chan struct{}
	next uint64
}

// Dial pins the installer-owned server account before sending any payload. The
// PID is used only to open a kernel process token, never as an authorization ID.
func Dial(ctx context.Context, name, expectedServerSID string) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()
	if err := validateName(name); err != nil {
		return nil, err
	}
	if !validSID(expectedServerSID) {
		return nil, fmt.Errorf("server SID required")
	}
	raw, err := winio.DialPipeAccessImpLevel(ctx, pipePath(name), clientAccess, winio.PipeImpLevelIdentification)
	if err != nil {
		return nil, err
	}
	c := &pipeConn{Conn: raw}
	f, ok := raw.(interface{ Fd() uintptr })
	if !ok {
		c.Close()
		return nil, fmt.Errorf("not a native pipe")
	}
	var pid uint32
	err = windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid)
	var process windows.Handle
	if err == nil {
		process, err = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	}
	if err == nil {
		var token windows.Token
		err = windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token)
		if err == nil {
			var user *windows.Tokenuser
			user, err = token.GetTokenUser()
			if err == nil && user.User.Sid.String() != expectedServerSID {
				err = fmt.Errorf("unexpected pipe server identity")
			}
			token.Close()
		}
		windows.CloseHandle(process)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return &Client{conn: c, gate: make(chan struct{}, 1)}, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) Call(ctx context.Context, operation string, payload json.RawMessage) (json.RawMessage, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline := time.Now().Add(ioTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		c.Close()
		return nil, err
	}
	c.next++
	request := Request{Version: Version, ID: c.next, Operation: operation, Payload: payload}
	if err := writeFrame(c.conn, request); err != nil {
		c.Close()
		return nil, err
	}
	var response Response
	if err := readFrame(c.conn, &response); err != nil {
		c.Close()
		return nil, err
	}
	if response.Version != Version || response.ID != request.ID {
		c.Close()
		return nil, fmt.Errorf("unexpected control response")
	}
	if response.Code != "ok" {
		return nil, errors.New(response.Code)
	}
	return response.Payload, nil
}
