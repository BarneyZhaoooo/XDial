//go:build linux || windows

package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type ownedFixture struct {
	Mode string `json:"mode"`
}
type ownedReady struct {
	PID     int      `json:"pid"`
	Address string   `json:"address"`
	Args    []string `json:"args"`
}

// A native executable fixture with a real loopback listener. No VPN, adapter,
// system DNS, external server, production config or process-name kill is used.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Exit(runOwnedFixture())
	}
	if len(os.Args) == 4 && os.Args[1] == "xdial-owned-parent" {
		binary, _ := os.Executable()
		cmd, lease, err := startSingBoxCmd(binary, os.Args[2], os.Args[3], io.Discard)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(90)
		}
		defer lease.Close()
		err = cmd.Wait()
		if err != nil {
			os.Exit(91)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func runOwnedFixture() int {
	if len(os.Args) != 6 || os.Args[2] != "-c" || os.Args[4] != "-D" {
		return 80
	}
	raw, err := os.ReadFile(os.Args[3])
	if err != nil {
		return 81
	}
	var config ownedFixture
	if json.Unmarshal(raw, &config) != nil {
		return 82
	}
	if config.Mode == "exit" {
		fmt.Fprintln(os.Stderr, "fixture startup failure")
		return 17
	}
	if config.Mode == "spawn" {
		binary, _ := os.Executable()
		child := os.Args[3] + ".grand"
		if os.WriteFile(child, []byte(`{"mode":"echo"}`), 0600) != nil {
			return 83
		}
		cmd := exec.Command(binary, "run", "-c", child, "-D", os.Args[5])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Start() != nil {
			return 84
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 85
	}
	defer listener.Close()
	ready, _ := json.Marshal(ownedReady{os.Getpid(), listener.Addr().String(), os.Args[1:]})
	if os.WriteFile(os.Args[3]+".ready", ready, 0600) != nil {
		return 86
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return 0
		}
		go func() { defer conn.Close(); io.Copy(conn, conn) }()
	}
}
func fixturePath(t *testing.T, mode string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "owned core 测试 ; &' paths")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config's & data.json")
	raw, _ := json.Marshal(ownedFixture{mode})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func readOwnedReady(t *testing.T, path string) ownedReady {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path + ".ready")
		var ready ownedReady
		if err == nil && json.Unmarshal(raw, &ready) == nil && ready.PID > 0 && ready.Address != "" {
			return ready
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned fixture did not become ready")
	return ownedReady{}
}
func echoOwned(t *testing.T, address string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err = conn.Write([]byte("owned")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 5)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "owned" {
		t.Fatal("loopback response", err)
	}
}
func waitOwnedClosed(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned fixture survived retirement")
}
func ownedStart(t *testing.T, path string) (*singBoxCommand, io.Closer, ownedReady) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd, lease, err := startSingBoxCmd(binary, path, filepath.Dir(path), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Close(); cmd.Process.Kill() })
	return cmd, lease, readOwnedReady(t, path)
}
func waitOwnedCommand(t *testing.T, cmd *singBoxCommand) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("owned Wait stuck")
		return nil
	}
}
func TestOwnedCoreArgumentsAndIndependentOwners(t *testing.T) {
	path := fixturePath(t, "echo")
	first, lease, ready := ownedStart(t, path)
	second, other, sibling := ownedStart(t, fixturePath(t, "echo"))
	want := []string{"run", "-c", path, "-D", filepath.Dir(path)}
	if !reflect.DeepEqual(ready.Args, want) {
		t.Fatalf("argv changed: %q", ready.Args)
	}
	echoOwned(t, ready.Address)
	echoOwned(t, sibling.Address)
	lease.Close()
	lease.Close()
	waitOwnedCommand(t, first)
	waitOwnedClosed(t, ready.Address)
	echoOwned(t, sibling.Address)
	other.Close()
	waitOwnedCommand(t, second)
}
func TestOwnedCoreParentCrashRetiresChild(t *testing.T) {
	path := fixturePath(t, "echo")
	binary, _ := os.Executable()
	parent := exec.Command(binary, "xdial-owned-parent", path, filepath.Dir(path))
	parent.Stdout = os.Stdout
	parent.Stderr = os.Stderr
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parent.Process.Kill() })
	ready := readOwnedReady(t, path)
	child, err := os.FindProcess(ready.PID)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Release()
	defer child.Kill()
	echoOwned(t, ready.Address)
	if err = parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	parent.Wait()
	waitOwnedClosed(t, ready.Address)
}
func TestOwnedCoreNaturalFailureAndMissingBinary(t *testing.T) {
	binary, _ := os.Executable()
	path := fixturePath(t, "exit")
	cmd, lease, err := startSingBoxCmd(binary, path, filepath.Dir(path), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err = waitOwnedCommand(t, cmd); err == nil {
		t.Fatal("startup failure swallowed")
	}
	cmd, lease, err = startSingBoxCmd(filepath.Join(t.TempDir(), "missing-core"), path, filepath.Dir(path), io.Discard)
	if err == nil || cmd != nil || lease != nil {
		t.Fatal("missing executable accepted")
	}
}
