//go:build linux && !android

package linuxcapture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type journal struct {
	Version  int    `json:"version"`
	Boot     string `json:"boot"`
	NetNS    uint64 `json:"netNS"`
	Token    string `json:"token"`
	Realm    uint32 `json:"realm"`
	Loopback int    `json:"loopback"`
	Plan     Plan   `json:"plan"`
	// Intent is fsynced BEFORE each exclusive create. Recovery verifies the
	// entire kernel object, including private realm/protocol, never just its name.
	Intent int `json:"intent"`
}

type stateDir struct{ dir, lock *os.File }

func openState(path string) (*stateDir, error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errors.New("requires a root-owned absolute private state directory")
	}
	fd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(fd)
			return nil, errors.New("state path must have root-owned non-writable ancestors")
		}
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&0077 != 0 {
		unix.Close(fd)
		return nil, errors.New("state directory must be private")
	}
	s := &stateDir{dir: os.NewFile(uintptr(fd), path)}
	f, err := s.open("lock", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		s.close()
		return nil, err
	}
	s.lock = f
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *stateDir) open(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(s.dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Nlink != 1 {
		unix.Close(fd)
		return nil, errors.New("invalid private state file")
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (s *stateDir) read() (*journal, error) {
	f, err := s.open("session.json", unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(b) > 16384 {
		return nil, errors.New("invalid capture journal length")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var j journal
	if err = d.Decode(&j); err != nil {
		return nil, err
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, errors.New("trailing capture journal data")
	}
	if j.Version != 1 || len(j.Token) != 32 || strings.Trim(j.Token, "0123456789abcdef") != "" || j.Realm == 0 || j.Loopback < 1 || j.Intent < 0 || j.Intent > 3 || j.Boot == "" || j.NetNS == 0 {
		return nil, errors.New("invalid capture journal")
	}
	if err = j.Plan.Validate(); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *stateDir) write(j *journal) error {
	// The directory is private and locked. A stale temporary file from our own
	// crash may be unlinked, but is never followed or truncated.
	if err := unix.Unlinkat(int(s.dir.Fd()), "pending.json", 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	f, err := s.open("pending.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = unix.Renameat(int(s.dir.Fd()), "pending.json", int(s.dir.Fd()), "session.json"); err != nil {
		return err
	}
	return s.dir.Sync()
}

func (s *stateDir) remove() error {
	if err := unix.Unlinkat(int(s.dir.Fd()), "session.json", 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return s.dir.Sync()
}

func (s *stateDir) close() {
	if s.lock != nil {
		s.lock.Close()
	}
	if s.dir != nil {
		s.dir.Close()
	}
}

func kernelIdentity() (string, uint64, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", 0, err
	}
	var st unix.Stat_t
	if err = unix.Stat("/proc/self/ns/net", &st); err != nil {
		return "", 0, err
	}
	if len(strings.TrimSpace(string(b))) != 36 {
		return "", 0, fmt.Errorf("invalid boot identity")
	}
	return strings.TrimSpace(string(b)), st.Ino, nil
}
