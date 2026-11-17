//go:build linux && !android

// Package linuxresolver captures an OS resolver dependency, not DNS policy.
// Only the verified system service's cgroup is exempted from capture. Its stub
// continues to own split DNS, server choice, DNSSEC and its own cache.
package linuxresolver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const resolverName = "org.freedesktop.resolve1"
const managerName = "org.freedesktop.systemd1"
const Stub = "127.0.0.53"

type Lease struct {
	bus             *dbus.Conn
	owner           string
	pid             uint32
	group           *os.File
	pidfd           int
	wakeR, wakeW    *os.File
	level           uint32
	id              uint64
	invalid, stop   chan struct{}
	once, closeOnce sync.Once
	wg              sync.WaitGroup
}

func (l *Lease) Invalid() <-chan struct{} { return l.invalid }
func (l *Lease) Identity() string         { return fmt.Sprintf("%s/%d/%d", l.owner, l.pid, l.id) }
func (l *Lease) Scope() (uint64, uint32, error) {
	select {
	case <-l.invalid:
		return 0, 0, errors.New("resolver dependency retired")
	default:
	}
	return l.id, l.level, nil
}
func (l *Lease) invalidate() { l.once.Do(func() { close(l.invalid) }) }
func (l *Lease) Close() error {
	l.closeOnce.Do(func() {
		l.invalidate()
		close(l.stop)
		if l.wakeW != nil {
			l.wakeW.Close()
		}
		if l.bus != nil {
			l.bus.Close()
		}
		l.wg.Wait()
		if l.wakeR != nil {
			l.wakeR.Close()
		}
		if l.pidfd >= 0 {
			unix.Close(l.pidfd)
		}
		if l.group != nil {
			l.group.Close()
		}
	})
	return nil
}

// Open never activates or configures a resolver. Failure leaves the current OS
// configuration intact. Addresses, UID, PID and cgroup are not caller inputs.
func Open(ctx context.Context) (l *Lease, err error) {
	l = &Lease{pidfd: -1, invalid: make(chan struct{}), stop: make(chan struct{})}
	defer func() {
		if err != nil {
			l.Close()
		}
	}()
	if os.Geteuid() != 0 {
		return l, errors.New("system resolver lease requires root service")
	}
	// Explicit protected system-bus endpoint, never DBUS_SYSTEM_BUS_ADDRESS.
	d, e := protectedDir("/run/dbus")
	if e != nil {
		return l, e
	}
	defer d.Close()
	var st unix.Stat_t
	if e = unix.Fstatat(int(d.Fd()), "system_bus_socket", &st, unix.AT_SYMLINK_NOFOLLOW); e != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return l, errors.New("untrusted system bus endpoint")
	}
	c, e := net.DialTimeout("unix", "/run/dbus/system_bus_socket", 3*time.Second)
	if e != nil {
		return l, e
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	l.bus, e = dbus.NewConn(c, dbus.WithContext(ctx))
	if e != nil {
		c.Close()
		return l, e
	}
	if e = l.bus.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Geteuid()))}); e != nil {
		return l, e
	}
	if e = l.bus.Hello(); e != nil {
		return l, e
	}
	c.SetDeadline(time.Time{})
	var after unix.Stat_t
	if e = unix.Fstatat(int(d.Fd()), "system_bus_socket", &after, unix.AT_SYMLINK_NOFOLLOW); e != nil || after.Ino != st.Ino || after.Dev != st.Dev {
		return l, errors.New("system bus endpoint changed")
	}
	signals := make(chan *dbus.Signal, 16)
	l.bus.Signal(signals)
	callctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, name := range []string{resolverName, managerName} {
		if e = l.bus.AddMatchSignalContext(callctx, dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, name)); e != nil {
			return l, e
		}
	}
	owner, pid, cgroup, e := snapshot(callctx, l.bus)
	if e != nil {
		return l, e
	}
	l.owner, l.pid = owner, pid
	l.pidfd, e = unix.PidfdOpen(int(pid), 0)
	if e != nil {
		return l, e
	}
	if e = validateProcess(pid, cgroup); e != nil {
		return l, e
	}
	l.group, e = protectedDir("/sys/fs/cgroup" + cgroup)
	if e != nil {
		return l, e
	}
	var fs unix.Statfs_t
	if e = unix.Fstatfs(int(l.group.Fd()), &fs); e != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return l, errors.New("resolver requires unified cgroup v2")
	}
	// The kernel's 64-bit cgroup ID is its kernfs file handle, not a PID/UID.
	h, _, e := unix.NameToHandleAt(int(l.group.Fd()), "", unix.AT_EMPTY_PATH)
	if e != nil || len(h.Bytes()) != 8 {
		return l, errors.New("resolver cgroup identity unavailable")
	}
	l.id = binary.NativeEndian.Uint64(h.Bytes())
	l.level = uint32(strings.Count(cgroup, "/"))
	if l.id == 0 || l.level == 0 || l.level > 255 {
		return l, errors.New("invalid resolver cgroup scope")
	}
	if e = l.Validate(callctx); e != nil {
		return l, e
	}
	l.wakeR, l.wakeW, e = os.Pipe()
	if e != nil {
		return l, e
	}
	l.wg.Add(2)
	go func() {
		defer l.wg.Done()
		fds := []unix.PollFd{{Fd: int32(l.pidfd), Events: unix.POLLIN}, {Fd: int32(l.wakeR.Fd()), Events: unix.POLLIN}}
		for {
			_, e := unix.Poll(fds, -1)
			if e == unix.EINTR {
				continue
			}
			if e != nil || fds[0].Revents != 0 {
				l.invalidate()
			}
			return
		}
	}()
	go func() {
		defer l.wg.Done()
		for {
			select {
			case <-l.stop:
				return
			case <-l.bus.Context().Done():
				l.invalidate()
				return
			case sig, ok := <-signals:
				if !ok || sig == nil {
					l.invalidate()
					return
				}
				if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
					l.invalidate()
					return
				}
			}
		}
	}()
	return l, nil
}

func (l *Lease) Validate(ctx context.Context) error {
	if _, _, e := l.Scope(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	owner, pid, cgroup, e := snapshot(ctx, l.bus)
	if e != nil || owner != l.owner || pid != l.pid {
		return errors.New("resolver identity changed")
	}
	if e = validateProcess(pid, cgroup); e != nil {
		return e
	}
	fds := []unix.PollFd{{Fd: int32(l.pidfd), Events: unix.POLLIN}}
	if _, e = unix.Poll(fds, 0); e != nil || fds[0].Revents != 0 {
		return errors.New("resolver process exited")
	}
	d, e := protectedDir("/sys/fs/cgroup" + cgroup)
	if e != nil {
		return e
	}
	defer d.Close()
	h, _, e := unix.NameToHandleAt(int(d.Fd()), "", unix.AT_EMPTY_PATH)
	if e != nil || len(h.Bytes()) != 8 || binary.NativeEndian.Uint64(h.Bytes()) != l.id {
		return errors.New("resolver cgroup changed")
	}
	return nil
}

func snapshot(ctx context.Context, b *dbus.Conn) (owner string, pid uint32, group string, err error) {
	getOwner := func(name string) (string, uint32, error) {
		var o string
		var p uint32
		e := b.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, name).Store(&o)
		if e == nil {
			e = b.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, o).Store(&p)
		}
		return o, p, e
	}
	m, mp, e := getOwner(managerName)
	if e != nil || mp != 1 {
		return "", 0, "", errors.New("system manager is not PID 1")
	}
	owner, pid, e = getOwner(resolverName)
	if e != nil || pid < 2 {
		return "", 0, "", errors.New("resolved is not running")
	}
	var path dbus.ObjectPath
	e = b.Object(m, "/org/freedesktop/systemd1").CallWithContext(ctx, managerName+".Manager.GetUnit", 0, "systemd-resolved.service").Store(&path)
	if e != nil {
		return "", 0, "", e
	}
	props := map[string]dbus.Variant{}
	for _, name := range []string{"MainPID", "Delegate", "ControlGroup"} {
		var v dbus.Variant
		if e = b.Object(m, path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, managerName+".Service", name).Store(&v); e != nil {
			return "", 0, "", e
		}
		props[name] = v
	}
	main, ok := props["MainPID"].Value().(uint32)
	if !ok || main != pid {
		return "", 0, "", errors.New("resolver bus owner differs from service MainPID")
	}
	delegate, ok := props["Delegate"].Value().(bool)
	if !ok || delegate {
		return "", 0, "", errors.New("resolver cgroup is delegated")
	}
	group, ok = props["ControlGroup"].Value().(string)
	if !ok || !strings.HasPrefix(group, "/") || filepath.Clean(group) != group || filepath.Base(group) != "systemd-resolved.service" || group == "/systemd-resolved.service" {
		return "", 0, "", errors.New("invalid resolver service cgroup")
	}
	return owner, pid, group, nil
}

func protectedDir(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("absolute protected path required")
	}
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
		var s unix.Stat_t
		if e = unix.Fstat(fd, &s); e != nil || s.Uid != 0 || s.Mode&0022 != 0 {
			unix.Close(fd)
			return nil, errors.New("resolver path is not root protected")
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}
func validateProcess(pid uint32, group string) error {
	base := "/proc/" + strconv.FormatUint(uint64(pid), 10)
	b, e := os.ReadFile(base + "/cgroup")
	if e != nil || strings.TrimSpace(string(b)) != "0::"+group {
		return errors.New("resolver kernel cgroup mismatch")
	}
	// Root controls the service executable; it may run as a dedicated UID.
	f, e := os.Open(base + "/exe")
	if e != nil {
		return e
	}
	defer f.Close()
	var s unix.Stat_t
	if e = unix.Fstat(int(f.Fd()), &s); e != nil || s.Uid != 0 || s.Mode&0022 != 0 || s.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("untrusted resolver executable")
	}
	// Pin the stub to sockets actually owned by the verified process. An
	// unrelated process bound to the same address is not an OS resolver lease.
	entries, e := os.ReadDir(base + "/fd")
	if e != nil {
		return e
	}
	owned := map[string]bool{}
	for _, entry := range entries {
		v, e := os.Readlink(base + "/fd/" + entry.Name())
		if e == nil && strings.HasPrefix(v, "socket:[") {
			owned[strings.TrimSuffix(strings.TrimPrefix(v, "socket:["), "]")] = true
		}
	}
	for _, proto := range []string{"udp", "tcp"} {
		b, e = os.ReadFile(base + "/net/" + proto)
		if e != nil {
			return e
		}
		found := false
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) > 9 && f[1] == "3500007F:0035" && owned[f[9]] && (proto == "udp" || f[3] == "0A") {
				found = true
			}
		}
		if !found {
			return errors.New("verified resolver does not own the DNS stub")
		}
	}
	return nil
}
