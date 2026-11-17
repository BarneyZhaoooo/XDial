//go:build linux

package engine

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type linuxProcessLease struct {
	once     sync.Once
	stop     chan struct{}
	exitOnce sync.Once
	exited   chan struct{}
}

func (l *linuxProcessLease) Close() error   { l.once.Do(func() { close(l.stop) }); return nil }
func (l *linuxProcessLease) ProcessExited() { l.exitOnce.Do(func() { close(l.exited) }) }

// Linux parent-death notification follows the creating OS thread, not merely
// the process. Keep that thread alive for the entire child lease. No shell,
// process-name sweep, shared PID file or system network mutation is involved.
func startSingBoxCmd(binary, configPath, workDir string, output io.Writer) (*singBoxCommand, io.Closer, error) {
	type started struct {
		cmd   *exec.Cmd
		lease *linuxProcessLease
		err   error
	}
	ready := make(chan started, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		cmd := exec.Command(binary, "run", "-c", configPath, "-D", workDir)
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.WaitDelay = time.Second
		if err := cmd.Start(); err != nil {
			ready <- started{err: fmt.Errorf("start owned core: %w", err)}
			return
		}
		lease := &linuxProcessLease{stop: make(chan struct{}), exited: make(chan struct{})}
		ready <- started{cmd: cmd, lease: lease}
		select {
		case <-lease.stop:
			// Give the core its ordinary shutdown opportunity. SingBoxProcess.Stop
			// owns the bounded forced-kill fallback. Keep the creator thread alive
			// until Wait completes so thread retirement cannot interrupt cleanup.
			_ = cmd.Process.Signal(syscall.SIGTERM)
			<-lease.exited
		case <-lease.exited:
		}
	}()
	result := <-ready
	if result.err != nil {
		return nil, nil, result.err
	}
	return &singBoxCommand{Process: result.cmd.Process, wait: func() error {
		err := result.cmd.Wait()
		result.lease.ProcessExited()
		return err
	}}, result.lease, nil
}
