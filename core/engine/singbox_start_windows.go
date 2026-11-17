//go:build windows

package engine

import (
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"
)

// PROC_THREAD_ATTRIBUTE_JOB_LIST is available on Windows 10+. Creating directly
// inside the Job also closes the suspended-create/assign parent-crash window.
const coreJobListAttribute = 0x0002000D

type windowsProcessLease struct {
	once sync.Once
	job  windows.Handle
	err  error
}

func (l *windowsProcessLease) Close() error {
	l.once.Do(func() { l.err = windows.CloseHandle(l.job) })
	return l.err
}
func (l *windowsProcessLease) ProcessExited() { l.Close() }

func startSingBoxCmd(binary, configPath, workDir string, output io.Writer) (*singBoxCommand, io.Closer, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	application, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, nil, err
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{path, "run", "-c", configPath, "-D", workDir}))
	if err != nil {
		return nil, nil, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create core Job: %w", err)
	}
	lease := &windowsProcessLease{job: job}
	success := false
	defer func() {
		if !success {
			lease.Close()
		}
	}()
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, nil, fmt.Errorf("configure core Job: %w", err)
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, nil, err
	}
	defer attributes.Delete()
	jobs := []windows.Handle{job}
	if err = attributes.Update(coreJobListAttribute, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
		return nil, nil, fmt.Errorf("atomic Job creation unavailable: %w", err)
	}
	// Only these explicitly duplicated stdin/stdout handles are inherited; never
	// the Job handle (which would keep the core alive after its owner crashed).
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer writer.Close()
	defer func() {
		if !success {
			reader.Close()
		}
	}()
	null, err := os.Open(os.DevNull)
	if err != nil {
		return nil, nil, err
	}
	defer null.Close()
	inherit := func(file *os.File) (windows.Handle, error) {
		var h windows.Handle
		err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &h, 0, true, windows.DUPLICATE_SAME_ACCESS)
		return h, err
	}
	input, err := inherit(null)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(input)
	out, err := inherit(writer)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(out)
	handles := []windows.Handle{input, out}
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return nil, nil, err
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput = input
	startup.StdOutput = out
	startup.StdErr = out
	var created windows.ProcessInformation
	err = windows.CreateProcess(application, command, nil, nil, true, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_NO_WINDOW, nil, nil, &startup.StartupInfo, &created)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(handles)
	if err != nil {
		return nil, nil, fmt.Errorf("create core in Job: %w", err)
	}
	defer windows.CloseHandle(created.Thread)
	// Keep the original process handle until FindProcess has opened its own;
	// Windows cannot recycle this PID while the original handle remains held.
	process, err := os.FindProcess(int(created.ProcessId))
	if err != nil {
		windows.TerminateProcess(created.Process, 1)
		windows.CloseHandle(created.Process)
		return nil, nil, err
	}
	windows.CloseHandle(created.Process)
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		defer reader.Close()
		if output != nil {
			_, _ = io.Copy(output, reader)
		} else {
			_, _ = io.Copy(io.Discard, reader)
		}
	}()
	wait := func() error {
		state, err := process.Wait()
		// Descendants may inherit stdout. Retire this Job before waiting for EOF.
		lease.Close()
		<-copied
		if err != nil {
			return err
		}
		if !state.Success() {
			return &exec.ExitError{ProcessState: state}
		}
		return nil
	}
	success = true
	return &singBoxCommand{Process: process, wait: wait}, lease, nil
}
