//go:build linux && !android

package linuxinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/controlpipe"
	"github.com/kafeifei/xdial/core/linuxmaintenance"
	"github.com/kafeifei/xdial/core/linuxservice"
)

const goodBundle = "/opt/xdial-test/xdial-service"
const oldBundle = "/opt/xdial-baseline/xdial-service"

func guest(t *testing.T) {
	t.Helper()
	dmi, _ := os.ReadFile("/sys/class/dmi/id/product_name")
	pid1, _ := os.ReadFile("/proc/1/comm")
	if os.Getenv("XDIAL_DISTRO_VM") != "1" || strings.TrimSpace(string(dmi)) != "XDial-isolated-distro" {
		t.Skip("disposable full distro only")
	}
	if os.Geteuid() != 0 || strings.TrimSpace(string(pid1)) != "systemd" {
		t.Fatal("guest root/systemd required")
	}
}
func command(t *testing.T, path string, args ...string) {
	t.Helper()
	out, e := exec.Command(path, args...).CombinedOutput()
	if e != nil {
		t.Fatalf("%s: %v %s", filepath.Base(path), e, out)
	}
}
func user(t *testing.T, op string, st linuxservice.Status) controlpipe.Response {
	t.Helper()
	args := []string{op}
	var input []byte
	if op != "status" {
		args = append(args, "--instance", st.Instance, "--revision", fmt.Sprint(st.Revision))
	}
	if op == "connect" {
		input, _ = json.Marshal(config.Profile{Lines: []config.Line{{ID: "direct", Name: "Direct", Enabled: true, Type: config.LineTypeDirect}}, Scenarios: []config.Scenario{{ID: "one", Name: "One", DefaultLineID: "direct"}}, ActiveScenarioID: "one"})
	}
	c := exec.Command(binaryPath, args...)
	c.Stdin = bytes.NewReader(input)
	c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1001, Gid: 1001, NoSetGroups: true}}
	out, e := c.Output()
	var r controlpipe.Response
	if json.Unmarshal(out, &r) != nil {
		t.Fatalf("user CLI %s: %v %s", op, e, out)
	}
	return r
}
func status(t *testing.T) linuxservice.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c := exec.Command(binaryPath, "status")
		c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1001, Gid: 1001, NoSetGroups: true}}
		out, e := c.Output()
		if e == nil {
			var r controlpipe.Response
			var s linuxservice.Status
			if json.Unmarshal(out, &r) == nil && r.Code == "ok" && json.Unmarshal(r.Payload, &s) == nil {
				return s
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("installed service not ready")
	return linuxservice.Status{}
}
func disconnected(t *testing.T, s linuxservice.Status) {
	t.Helper()
	var c struct{ Desired bool }
	if json.Unmarshal(s.Connection, &c) != nil || c.Desired || s.CleanupRequired {
		t.Fatalf("service restored a connection or requires cleanup: %+v", s)
	}
}
func connect(t *testing.T) {
	t.Helper()
	r := user(t, "connect", status(t))
	if r.Code != "ok" {
		t.Fatalf("connection: %+v", r)
	}
}
func hashes(t *testing.T) [3]string {
	t.Helper()
	var h [3]string
	for i := range names {
		b, e := readTarget(i)
		if e != nil {
			t.Fatal(e)
		}
		h[i] = digest(b)
	}
	return h
}
func installOld(t *testing.T, enabled bool) {
	t.Helper()
	command(t, oldBundle+"/install.sh", "install", "1001", "65536", "131072", "50000", "32000", "127.0.0.1:31234")
	if enabled {
		command(t, "systemctl", "enable", "xdial-service")
	}
	command(t, "systemctl", "start", "xdial-service")
	disconnected(t, status(t))
	connect(t)
}
func removeFixture(t *testing.T) {
	t.Helper()
	command(t, goodBundle+"/install.sh", "remove")
	if _, e := os.Stat(transaction); !os.IsNotExist(e) {
		t.Fatal("upgrade debris remains", e)
	}
	if e := os.RemoveAll(state); e != nil {
		t.Fatal(e)
	}
}
func TestUpgradeCrashChild(t *testing.T) {
	stage := os.Getenv("XDIAL_UPGRADE_CRASH")
	if stage == "" {
		t.Skip("child only")
	}
	guest(t)
	checkpoint = func(s string) error {
		if s == stage {
			fmt.Println("CRASH_STAGE", s)
			syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}
		return nil
	}
	if e := Upgrade(goodBundle); e != nil {
		t.Fatal(e)
	}
	t.Fatal("crash stage not reached")
}

func TestInstalledUpgradeLifecycle(t *testing.T) {
	guest(t)
	group := os.Getenv("XDIAL_UPGRADE_GROUP")
	if group != "" && group != "all" && group != "early" && group != "late" {
		t.Fatal("unknown upgrade test group")
	}
	command(t, "modprobe", "nft_tproxy")
	command(t, "modprobe", "tcp_diag")
	command(t, "modprobe", "udp_diag")
	command(t, "ip", "addr", "add", "192.0.2.53/32", "dev", "lo")
	defer exec.Command("ip", "addr", "del", "192.0.2.53/32", "dev", "lo").Run()
	os.Remove("/etc/resolv.conf")
	if e := os.WriteFile("/etc/resolv.conf", []byte("nameserver 192.0.2.53\n"), 0644); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		out, _ := exec.Command("journalctl", "-u", "xdial-service", "--no-pager", "-n", "100").CombinedOutput()
		t.Logf("journal:\n%s", out)
		exec.Command("systemctl", "stop", "xdial-service").Run()
	})
	if !t.Run("normal-and-startup-rollback", func(t *testing.T) {
		if group == "late" {
			t.Skip("covered by early group with the same binaries")
		}
		installOld(t, true)
		old := hashes(t)
		policy, _ := read(state+"/policy.json", 16384, true)
		if e := os.Chmod(goodBundle, 0777); e != nil {
			t.Fatal(e)
		}
		out, rejected := exec.Command(goodBundle+"/install.sh", "upgrade").CombinedOutput()
		if e := os.Chmod(goodBundle, 0755); e != nil {
			t.Fatal(e)
		}
		if rejected == nil || !bytes.Contains(out, []byte("Bundle ancestry")) || hashes(t) != old {
			t.Fatalf("writable bundle was not rejected before execution: %v %s", rejected, out)
		}
		command(t, goodBundle+"/install.sh", "upgrade")
		newer := hashes(t)
		if newer == old {
			t.Fatal("upgrade did not replace package")
		}
		disconnected(t, status(t))
		connect(t)
		after, _ := read(state+"/policy.json", 16384, true)
		if !bytes.Equal(policy, after) {
			t.Fatal("upgrade changed policy")
		}
		// Live maintenance excludes mutations, but users can still stop traffic.
		lease, e := linuxmaintenance.InstallationLease(state)
		if e != nil {
			t.Fatal(e)
		}
		defer lease.Close()
		if r := user(t, "disconnect", status(t)); r.Code != "ok" {
			t.Fatal("maintenance blocked user disconnect", r.Code)
		}
		if r := user(t, "connect", status(t)); r.Code != "maintenance_required" {
			t.Fatal("live maintenance allowed connection", r.Code)
		}
		lease.Close()
		connect(t)
		// A root-reviewed candidate with a real failing service startup, not a mock
		// of systemctl. The current helper stages it and owns rollback.
		bad := "/opt/xdial-bad-upgrade"
		if e := os.Mkdir(bad, 0700); e != nil {
			t.Fatal(e)
		}
		defer os.RemoveAll(bad)
		var checks strings.Builder
		for _, name := range []string{"xdial-service", "xdial-service.service", "install.sh"} {
			b, e := read(goodBundle+"/"+name, 128<<20, false)
			if e != nil {
				t.Fatal(e)
			}
			mode := os.FileMode(0644)
			if name == "xdial-service" {
				b = []byte("#!/bin/sh\nexit 42\n")
				mode = 0755
			}
			if e = writeNew(bad+"/"+name, b, mode); e != nil {
				t.Fatal(e)
			}
			fmt.Fprintf(&checks, "%s  %s\n", digest(b), name)
		}
		if e := writeNew(bad+"/SHA256SUMS", []byte(checks.String()), 0600); e != nil {
			t.Fatal(e)
		}
		if e := Upgrade(bad); e == nil {
			t.Fatal("broken service committed")
		}
		if hashes(t) != newer {
			t.Fatal("startup failure failed to restore package")
		}
		disconnected(t, status(t))
		connect(t)
		if got, _ := systemctl("is-enabled", "xdial-service"); got != "enabled" {
			t.Fatal("enablement lost", got)
		}
		// An installed but stopped service must not be started by an upgrade.
		command(t, "systemctl", "stop", "xdial-service")
		command(t, goodBundle+"/install.sh", "upgrade")
		if active, _ := systemctl("is-active", "xdial-service"); active != "inactive" {
			t.Fatal("inactive upgrade started service", active)
		}
		command(t, "systemctl", "start", "xdial-service")
		disconnected(t, status(t))
		connect(t)
		removeFixture(t)
	}) {
		return
	}
	for _, point := range []string{"staged", "prepared", "stopped", "binary", "unit", "manifest", "ready", "committed", "cleanup"} {
		early := point == "staged" || point == "prepared" || point == "stopped" || point == "binary"
		if (group == "early" && !early) || (group == "late" && early) {
			continue
		}
		if !t.Run("kill-"+point, func(t *testing.T) {
			installOld(t, false)
			old := hashes(t)
			c := exec.Command("/service-tests", "-test.run=^TestUpgradeCrashChild$", "-test.v")
			c.Env = append(os.Environ(), "XDIAL_UPGRADE_CRASH="+point)
			out, e := c.CombinedOutput()
			var exit *exec.ExitError
			if !strings.Contains(string(out), "CRASH_STAGE "+point) || !errors.As(e, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatalf("real kill missing: %v %s", e, out)
			}
			if point == "ready" || point == "committed" || point == "cleanup" {
				r := user(t, "connect", status(t))
				if r.Code != "maintenance_required" {
					t.Fatalf("unfinished upgrade accepted connection: %+v", r)
				}
			}
			if point == "binary" {
				// A changed installed file is not silently overwritten by recovery.
				if e := os.WriteFile(binaryPath, []byte("foreign replacement"), 0755); e != nil {
					t.Fatal(e)
				}
				if e := Recover(); e == nil {
					t.Fatal("foreign file was overwritten")
				}
				b, _ := os.ReadFile(binaryPath)
				if string(b) != "foreign replacement" {
					t.Fatal("foreign file changed")
				}
				b, e := read(goodBundle+"/xdial-service", 128<<20, false)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(binaryPath, b, 0755); e != nil {
					t.Fatal(e)
				}
			}
			if e := Recover(); e != nil {
				t.Fatal(e)
			}
			command(t, goodBundle+"/install.sh", "recover-upgrade")
			got := hashes(t)
			if point == "committed" || point == "cleanup" {
				if got == old {
					t.Fatal("committed package rolled back")
				}
			} else if got != old {
				t.Fatal("uncommitted package not restored")
			}
			if point != "staged" {
				if active, _ := systemctl("is-active", "xdial-service"); active == "active" {
					t.Fatal("explicit recovery restarted service")
				}
				command(t, "systemctl", "start", "xdial-service")
				disconnected(t, status(t))
				connect(t)
			}
			if enabled, _ := systemctl("is-enabled", "xdial-service"); enabled != "disabled" {
				t.Fatal("enablement changed", enabled)
			}
			removeFixture(t)
		}) {
			return
		}
	}
	t.Log("XDIAL_DISTRO_UPGRADE_OK", group)
}
