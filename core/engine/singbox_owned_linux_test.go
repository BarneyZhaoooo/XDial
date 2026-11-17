//go:build linux

package engine

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func init() {
	prepareOwnedFixture = func(mode, path string) {
		switch mode {
		case "graceful":
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, syscall.SIGTERM)
			go func() {
				<-stop
				if os.WriteFile(path+".stopped", []byte("flushed"), 0600) != nil {
					os.Exit(92)
				}
				os.Exit(0)
			}()
		case "ignore-stop":
			signal.Ignore(syscall.SIGTERM, syscall.SIGINT)
		}
	}
}

func TestOwnedLinuxLeaseAllowsGracefulExit(t *testing.T) {
	path := fixturePath(t, "graceful")
	cmd, lease, ready := ownedStart(t, path)
	lease.Close()
	if err := waitOwnedCommand(t, cmd); err != nil {
		t.Fatal("normal shutdown was forcibly interrupted", err)
	}
	if data, err := os.ReadFile(path + ".stopped"); err != nil || string(data) != "flushed" {
		t.Fatal("core did not complete its cleanup", err)
	}
	waitOwnedClosed(t, ready.Address)
}

func TestOwnedLinuxStopBoundsUnresponsiveCore(t *testing.T) {
	path := fixturePath(t, "ignore-stop")
	cmd, lease, ready := ownedStart(t, path)
	exited := make(chan struct{})
	s := &SingBoxProcess{cmd: cmd, pipeW: lease, exited: exited, configPath: path}
	go s.watch(cmd, exited, &singBoxLogWriter{})
	started := time.Now()
	s.stop(80 * time.Millisecond)
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("bounded stop took %v", elapsed)
	}
	waitOwnedClosed(t, ready.Address)
}
