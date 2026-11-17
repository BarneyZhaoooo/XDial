//go:build linux && !android

// Package linuxmaintenance coordinates root installation and user connection
// transactions. It contains no Profile, DNS, route or forwarding policy.
package linuxmaintenance

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Lock uses the same kernel lock as the reviewed package shell entry. A shared
// lease covers a complete connection mutation; installers take it exclusively.
func Lock(shared bool) (*os.File, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("maintenance requires root")
	}
	dir, e := unix.Open("/run", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if e = unix.Fstat(dir, &st); e != nil || st.Uid != 0 || st.Mode&0022 != 0 {
		return nil, errors.New("unsafe maintenance lock directory")
	}
	fd, e := unix.Openat(dir, "xdial-service.install.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "xdial-service.install.lock")
	if e = unix.Fstat(fd, &st); e != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Nlink != 1 {
		f.Close()
		return nil, errors.New("unsafe maintenance lock")
	}
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	if e = unix.Flock(fd, mode|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

func ConnectionLease(state string) (*os.File, error) {
	f, e := stateLock(state, true)
	if e != nil {
		return nil, e
	}
	// Even after an installer crash releases flock, durable unfinished work must
	// be recovered explicitly before a new connection can start.
	_, e = os.Lstat(filepath.Join(state, "upgrade", "journal.json"))
	if !os.IsNotExist(e) {
		f.Close()
		return nil, errors.New("installation recovery required")
	}
	return f, nil
}

// The service can write only its private state directory under ProtectSystem.
// Keep the shared connection lease there; the root installer additionally takes
// the legacy /run lock so older reviewed package scripts cannot run concurrently.
type installationLease struct{ global, state *os.File }

func (l *installationLease) Close() error { return errors.Join(l.state.Close(), l.global.Close()) }
func InstallationLease(state string) (io.Closer, error) {
	global, e := Lock(false)
	if e != nil {
		return nil, e
	}
	local, e := stateLock(state, false)
	if e != nil {
		global.Close()
		return nil, e
	}
	return &installationLease{global, local}, nil
}
func stateLock(path string, shared bool) (*os.File, error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errors.New("private installation state required")
	}
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(fd)
			return nil, errors.New("unsafe state ancestry")
		}
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || st.Mode&0077 != 0 {
		return nil, errors.New("state is not private")
	}
	file, e := unix.Openat(fd, "installation.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(file), "installation.lock")
	if e = unix.Fstat(file, &st); e != nil || st.Uid != 0 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe state lock")
	}
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	if e = unix.Flock(file, mode|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
