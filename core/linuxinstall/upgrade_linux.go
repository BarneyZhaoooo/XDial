//go:build linux && !android

// Package linuxinstall owns the privileged, fixed-path package transaction. It
// is reachable only from a root maintenance CLI, never the user control pipe.
package linuxinstall

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kafeifei/xdial/core/linuxmaintenance"
	"github.com/kafeifei/xdial/core/linuxservice"
	"golang.org/x/sys/unix"
)

const state = "/var/lib/xdial-service"
const transaction = state + "/upgrade"
const binaryPath = "/usr/local/libexec/xdial-service"
const unitPath = "/etc/systemd/system/xdial-service.service"
const manifestPath = state + "/package.sha256"

var targets = []string{binaryPath, unitPath, manifestPath}
var names = []string{"binary", "unit", "manifest"}
var modes = []os.FileMode{0755, 0644, 0600}

// Test-only fault injection is assigned by in-package guest tests; never an
// environment switch or a privileged client operation in the production binary.
var checkpoint func(string) error

func step(s string) error {
	if checkpoint != nil {
		return checkpoint(s)
	}
	return nil
}

type journal struct {
	Version    int       `json:"version"`
	Token      string    `json:"token"`
	Committed  bool      `json:"committed"`
	RolledBack bool      `json:"rolledBack"`
	Active     bool      `json:"active"`
	Enabled    string    `json:"enabled"`
	Old        [3]string `json:"old"`
	New        [3]string `json:"new"`
}

func digest(b []byte) string  { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hashValid(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }

// All directories and all files used after admission have root-only writable
// ancestors. openat/no-follow closes symlink traversal at every path component.
func openDir(path string, private bool) (*os.File, error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("root-owned absolute path required")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(fd)
			return nil, errors.New("unsafe installation directory")
		}
	}
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || (private && st.Mode&0077 != 0) {
		unix.Close(fd)
		return nil, errors.New("installation state must be private")
	}
	return os.NewFile(uintptr(fd), path), nil
}
func read(path string, limit int64, private bool) ([]byte, error) {
	d, e := openDir(filepath.Dir(path), false)
	if e != nil {
		return nil, e
	}
	defer d.Close()
	fd, e := unix.Openat(int(d.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || st.Uid != 0 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || (private && st.Mode&0077 != 0) {
		return nil, errors.New("unsafe installation file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("installation file exceeds bound")
	}
	return b, nil
}
func syncDir(path string) error {
	d, e := openDir(path, false)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func writeNew(path string, b []byte, mode os.FileMode) error {
	d, e := openDir(filepath.Dir(path), false)
	if e != nil {
		return e
	}
	defer d.Close()
	fd, e := unix.Openat(int(d.Fd()), filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), path)
	_, e = f.Write(b)
	if e == nil {
		e = f.Chmod(mode)
	}
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close(), d.Sync())
}
func atomicWrite(path string, b []byte, mode os.FileMode, token string) error {
	temp := filepath.Join(filepath.Dir(path), ".xdial-upgrade-"+token+"-"+filepath.Base(path))
	// A journal-owned temporary may contain only a partial write after a crash.
	// Refuse symlinks/hardlinks/foreign ownership; never follow or truncate it.
	if _, e := read(temp, 128<<20, false); e == nil {
		if e = os.Remove(temp); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := writeNew(temp, b, mode); e != nil {
		return e
	}
	if e := os.Rename(temp, path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func save(j *journal) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	return atomicWrite(transaction+"/journal.json", b, 0600, j.Token)
}
func load() (*journal, error) {
	dir, e := openDir(transaction, true)
	if e != nil {
		return nil, e
	}
	dir.Close()
	b, e := read(transaction+"/journal.json", 16384, true)
	if e != nil {
		return nil, e
	}
	var j journal
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&j); e != nil {
		return nil, e
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return nil, errors.New("trailing upgrade journal")
	}
	if (j.RolledBack && !j.Committed) || j.Version != 1 || len(j.Token) != 32 || strings.Trim(j.Token, "0123456789abcdef") != "" || (j.Enabled != "enabled" && j.Enabled != "disabled") {
		return nil, errors.New("invalid upgrade journal")
	}
	for i := range names {
		if !hashValid(j.Old[i]) || !hashValid(j.New[i]) {
			return nil, errors.New("invalid upgrade digest")
		}
	}
	return &j, nil
}
func systemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", args...)
	out, e := cmd.CombinedOutput()
	if e != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("systemctl %s failed: %w", args[0], e)
	}
	return strings.TrimSpace(string(out)), nil
}
func unitState() (active bool, enabled string, e error) {
	fragment, err := systemctl("show", "--property=FragmentPath", "--value", "xdial-service")
	if err != nil || fragment != unitPath {
		return false, "", errors.New("installed unit identity differs")
	}
	drop, err := systemctl("show", "--property=DropInPaths", "--value", "xdial-service")
	if err != nil || drop != "" {
		return false, "", errors.New("unit overrides require explicit review before upgrade")
	}
	enabled, err = systemctl("is-enabled", "xdial-service")
	if enabled != "enabled" && enabled != "disabled" {
		return false, "", errors.New("unsupported unit enablement state")
	}
	status, err := systemctl("is-active", "xdial-service")
	if status != "active" && status != "inactive" && status != "failed" {
		return false, "", errors.New("unit transition in progress")
	}
	return status == "active", enabled, nil
}
func parseHashes(b []byte, allowed []string) (map[string]string, error) {
	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !hashValid(fields[0]) {
			return nil, errors.New("invalid package checksum manifest")
		}
		match := false
		for _, name := range allowed {
			if fields[1] == name {
				match = true
			}
		}
		if !match || result[fields[1]] != "" {
			return nil, errors.New("unexpected package manifest entry")
		}
		result[fields[1]] = fields[0]
	}
	if len(result) != len(allowed) {
		return nil, errors.New("incomplete package manifest")
	}
	return result, nil
}
func readTarget(i int) ([]byte, error) {
	b, e := read(targets[i], 128<<20, i == 2)
	if e != nil {
		return nil, e
	}
	st, e := os.Lstat(targets[i])
	if e != nil || st.Mode().Perm() != modes[i] {
		return nil, errors.New("installed file mode changed")
	}
	return b, nil
}
func current() ([3][]byte, error) {
	var files [3][]byte
	for i := range targets {
		b, e := readTarget(i)
		if e != nil {
			return files, e
		}
		files[i] = b
	}
	h, e := parseHashes(files[2], targets[:2])
	if e != nil {
		return files, e
	}
	for i := 0; i < 2; i++ {
		if digest(files[i]) != h[targets[i]] {
			return files, errors.New("installed package identity changed")
		}
	}
	return files, nil
}
func candidate(bundle string) ([3][]byte, error) {
	var files [3][]byte
	d, e := openDir(bundle, false)
	if e != nil {
		return files, e
	}
	d.Close()
	b, e := read(filepath.Join(bundle, "SHA256SUMS"), 16384, false)
	if e != nil {
		return files, e
	}
	h, e := parseHashes(b, []string{"xdial-service", "xdial-service.service", "install.sh"})
	if e != nil {
		return files, e
	}
	for i, name := range []string{"xdial-service", "xdial-service.service", "install.sh"} {
		b, e = read(filepath.Join(bundle, name), 128<<20, false)
		if e != nil {
			return files, e
		}
		if digest(b) != h[name] {
			return files, errors.New("candidate checksum mismatch")
		}
		if i < 2 {
			files[i] = b
		}
	}
	if !bytes.Contains(files[1], []byte("\nType=notify\n")) || !bytes.Contains(files[1], []byte("\nExecStart="+binaryPath+" serve --state "+state+"\n")) {
		return files, errors.New("candidate lacks installed service readiness contract")
	}
	files[2] = []byte(digest(files[0]) + "  " + binaryPath + "\n" + digest(files[1]) + "  " + unitPath + "\n")
	return files, nil
}
func validateStage(j *journal) ([3][]byte, [3][]byte, error) {
	var old, new [3][]byte
	for i, n := range names {
		for _, v := range []struct {
			prefix string
			dest   *[3][]byte
			hash   string
		}{{"old-", &old, j.Old[i]}, {"new-", &new, j.New[i]}} {
			b, e := read(transaction+"/"+v.prefix+n, 128<<20, true)
			if e != nil || digest(b) != v.hash {
				return old, new, errors.New("upgrade backup identity changed")
			}
			v.dest[i] = b
		}
		b, e := readTarget(i)
		if e != nil {
			return old, new, e
		}
		h := digest(b)
		if h != j.Old[i] && h != j.New[i] {
			return old, new, errors.New("installed file outside upgrade scope")
		}
	}
	return old, new, nil
}
func clean(j *journal) error {
	// Journal is removed last. Interruption during backup cleanup is recovered
	// as a cleanup-only operation using the committed/rolled-back file identity.
	for i, n := range names {
		for _, prefix := range []string{"old-", "new-"} {
			path := transaction + "/" + prefix + n
			b, e := read(path, 128<<20, true)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			expected := j.Old[i]
			if prefix == "new-" {
				expected = j.New[i]
			}
			if digest(b) != expected {
				return errors.New("upgrade cleanup identity changed")
			}
			if e = os.Remove(path); e != nil {
				return e
			}
			if e = step("cleanup"); e != nil {
				return e
			}
		}
	}
	if e := os.Remove(transaction + "/journal.json"); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e := syncDir(transaction); e != nil {
		return e
	}
	if e := os.Remove(transaction); e != nil {
		return e
	}
	return syncDir(state)
}

// Upgrade is an explicit root installation transaction. It never restores a
// VPN connection intent: a restarted service is ready but remains disconnected.
func Upgrade(bundle string) error {
	lock, e := linuxmaintenance.InstallationLease(state)
	if e != nil {
		return e
	}
	defer lock.Close()
	root, e := openDir(state, true)
	if e != nil {
		return e
	}
	root.Close()
	if _, e = os.Lstat(transaction); !os.IsNotExist(e) {
		return errors.New("run recover-upgrade before another installation")
	}
	old, e := current()
	if e != nil {
		return e
	}
	next, e := candidate(bundle)
	if e != nil {
		return e
	}
	active, enabled, e := unitState()
	if e != nil {
		return e
	}
	d, e := openDir(state, true)
	if e != nil {
		return e
	}
	d.Close()
	if e = os.Mkdir(transaction, 0700); e != nil {
		return e
	}
	if e = syncDir(state); e != nil {
		return e
	}
	j := &journal{Version: 1, Active: active, Enabled: enabled}
	token := make([]byte, 16)
	if _, e = rand.Read(token); e != nil {
		return e
	}
	j.Token = hex.EncodeToString(token)
	for i, n := range names {
		j.Old[i] = digest(old[i])
		j.New[i] = digest(next[i])
		if e = writeNew(transaction+"/old-"+n, old[i], 0600); e != nil {
			return e
		}
		if e = writeNew(transaction+"/new-"+n, next[i], 0600); e != nil {
			return e
		}
	}
	if e = step("staged"); e != nil {
		return e
	}
	if e = save(j); e != nil {
		return e
	}
	if e = step("prepared"); e != nil {
		return e
	}
	if e = apply(j, next); e != nil {
		// Preserve the original failure and any rollback failure. A failed rollback
		// retains the durable journal; no subsequent connection may bypass it.
		return errors.Join(e, rollback(j, true))
	}
	j.Committed = true
	if e = save(j); e != nil {
		return e
	}
	if e = step("committed"); e != nil {
		return e
	}
	return clean(j)
}
func apply(j *journal, next [3][]byte) error {
	if _, e := systemctl("stop", "xdial-service"); e != nil {
		return e
	}
	if e := linuxservice.RecoverState(state); e != nil {
		return e
	}
	if e := step("stopped"); e != nil {
		return e
	}
	for i, path := range targets {
		if e := atomicWrite(path, next[i], modes[i], j.Token); e != nil {
			return e
		}
		if e := step(names[i]); e != nil {
			return e
		}
	}
	if _, e := systemctl("daemon-reload"); e != nil {
		return e
	}
	enabled, _ := systemctl("is-enabled", "xdial-service")
	if enabled != j.Enabled {
		return errors.New("enablement changed during upgrade")
	}
	if j.Active {
		if _, e := systemctl("start", "xdial-service"); e != nil {
			return e
		}
	}
	return step("ready")
}
func rollback(j *journal, restart bool) error {
	old, _, e := validateStage(j)
	if e != nil {
		return e
	}
	if _, e = systemctl("stop", "xdial-service"); e != nil {
		return e
	}
	if e = linuxservice.RecoverState(state); e != nil {
		return e
	}
	for i, path := range targets {
		if e = atomicWrite(path, old[i], modes[i], j.Token); e != nil {
			return e
		}
	}
	if _, e = systemctl("daemon-reload"); e != nil {
		return e
	}
	enabled, _ := systemctl("is-enabled", "xdial-service")
	if enabled != j.Enabled {
		return errors.New("enablement ownership changed; leave stopped")
	}
	if restart && j.Active {
		if _, e = systemctl("start", "xdial-service"); e != nil {
			return e
		}
	}
	// Publish the chosen final identity before deleting backups. Recovery after
	// this point only verifies those files and removes transaction debris.
	j.RolledBack = true
	j.Committed = true
	if e = save(j); e != nil {
		return e
	}
	return clean(j)
}

// Recover rolls back uncommitted work, or finalizes a committed package. An
// explicit crash recovery leaves the service stopped; enablement is untouched.
func Recover() error {
	lock, e := linuxmaintenance.InstallationLease(state)
	if e != nil {
		return e
	}
	defer lock.Close()
	root, e := openDir(state, true)
	if e != nil {
		return e
	}
	root.Close()
	j, e := load()
	if os.IsNotExist(e) {
		return recoverOrphan()
	}
	if e != nil {
		return e
	}
	if !j.Committed {
		return rollback(j, false)
	}
	for i := range targets {
		b, e := readTarget(i)
		expected := j.New[i]
		if j.RolledBack {
			expected = j.Old[i]
		}
		if e != nil || digest(b) != expected {
			return errors.New("committed package identity changed")
		}
	}
	enabled, _ := systemctl("is-enabled", "xdial-service")
	if enabled != j.Enabled {
		return errors.New("enablement ownership changed")
	}
	if _, e = systemctl("stop", "xdial-service"); e != nil {
		return e
	}
	if e = linuxservice.RecoverState(state); e != nil {
		return e
	}
	if _, e = systemctl("daemon-reload"); e != nil {
		return e
	}
	return clean(j)
}

// No installed file or service state is changed before the first durable
// journal. A pre-journal crash can only leave these private staged files.
func recoverOrphan() error {
	d, e := openDir(transaction, true)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer d.Close()
	entries, e := d.ReadDir(-1)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		name := entry.Name()
		known := false
		for _, n := range names {
			if name == "old-"+n || name == "new-"+n {
				known = true
			}
		}
		if strings.HasPrefix(name, ".xdial-upgrade-") && strings.HasSuffix(name, "-journal.json") {
			token := strings.TrimSuffix(strings.TrimPrefix(name, ".xdial-upgrade-"), "-journal.json")
			known = len(token) == 32 && strings.Trim(token, "0123456789abcdef") == ""
		}
		if !known {
			return errors.New("unknown orphan upgrade file")
		}
		if _, e = read(filepath.Join(transaction, name), 128<<20, true); e != nil {
			return e
		}
	}
	for _, entry := range entries {
		if e = os.Remove(filepath.Join(transaction, entry.Name())); e != nil {
			return e
		}
	}
	if e = d.Sync(); e != nil {
		return e
	}
	if e = os.Remove(transaction); e != nil {
		return e
	}
	return syncDir(state)
}
