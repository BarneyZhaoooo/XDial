//go:build linux && !android

package linuxcapture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/nftables"
	rtnl "github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func guest(t *testing.T) {
	t.Helper()
	if os.Getenv("XDIAL_ISOLATED_VM") != "1" {
		t.Skip("requires isolated guest")
	}
	b, e := os.ReadFile("/proc/cmdline")
	if e != nil || !strings.Contains(string(b), "xdial_isolated_vm=transparent") || os.Geteuid() != 0 {
		t.Fatal("refuse mutation outside isolated guest")
	}
	is, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range is {
		if i.Flags&net.FlagLoopback == 0 {
			t.Fatal("guest must have no external interface")
		}
	}
}
func testPlan() Plan {
	return Plan{CaptureMark: 1 << 16, BypassMark: 1 << 17, Table: 50000, Priority: 32000, Listener: "127.0.0.1:15080"}
}
func state(t *testing.T) string {
	t.Helper()
	p, e := os.MkdirTemp("/run", "xdial-capture-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(p) })
	return p
}
func empty(t *testing.T, p string) {
	t.Helper()
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	defer k.close()
	if e = k.preflight(&journal{Plan: testPlan(), Token: strings.Repeat("0", 32)}); e != nil {
		t.Fatal("kernel residue", e)
	}
	if _, e = os.Stat(filepath.Join(p, "session.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("journal residue", e)
	}
}

func TestCaptureInstallRollback(t *testing.T) {
	guest(t)
	for step := 1; step <= 3; step++ {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			p := state(t)
			sentinel := errors.New("injected after kernel acknowledgement")
			_, e := install(context.Background(), p, testPlan(), func(s int) error {
				if s == step {
					return sentinel
				}
				return nil
			})
			if !errors.Is(e, sentinel) {
				t.Fatal(e)
			}
			t.Log(e)
			empty(t, p)
		})
	}
	p := state(t)
	s, e := Install(context.Background(), p, testPlan())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Install(context.Background(), p, testPlan()); e == nil {
		t.Fatal("accepted second owner")
	}
	if e = Recover(p); e == nil {
		t.Fatal("recovered live owner")
	}
	// Another netlink socket is not allowed to delete the owned table.
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	k.nft.DelTable(table(s.j))
	k.deadline()
	e = k.nft.Flush()
	k.close()
	if e == nil {
		t.Fatal("foreign socket deleted owner table")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	empty(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = Install(ctx, p, testPlan()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	empty(t, p)
}

func TestCaptureConflictsPreserved(t *testing.T) {
	guest(t)
	p := state(t)
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	defer k.close()
	foreign := rtnl.NewRule()
	foreign.Family = unix.AF_INET
	foreign.Table = 50001
	foreign.Priority = testPlan().Priority
	defer func() { k.rt.RuleDel(foreign) }()
	if e = k.rt.RuleAdd(foreign); e != nil {
		t.Fatal(e)
	}
	if _, e = Install(context.Background(), p, testPlan()); e == nil || !strings.Contains(e.Error(), "reservation conflicts") {
		t.Fatal("priority conflict not detected", e)
	}
	if e = k.rt.RuleDel(foreign); e != nil {
		t.Fatal("foreign rule removed", e)
	}
	foreign.Priority = testPlan().Priority - 1
	if e = k.rt.RuleAdd(foreign); e != nil {
		t.Fatal(e)
	}
	if _, e = Install(context.Background(), p, testPlan()); e == nil || !strings.Contains(e.Error(), "shadowed") {
		t.Fatal("accepted shadowed capture priority", e)
	}
	if e = k.rt.RuleDel(foreign); e != nil {
		t.Fatal(e)
	}
	r := &rtnl.Route{Family: unix.AF_INET, Table: testPlan().Table, Type: unix.RTN_BLACKHOLE, Dst: &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}}
	if e = k.rt.RouteAdd(r); e != nil {
		t.Fatal(e)
	}
	if _, e = Install(context.Background(), p, testPlan()); e == nil || !strings.Contains(e.Error(), "table is occupied") {
		t.Fatal("table conflict not detected", e)
	}
	if e = k.rt.RouteDel(r); e != nil {
		t.Fatal("foreign route removed", e)
	}
	empty(t, p)
}

func TestCaptureCancellationAndNamespace(t *testing.T) {
	guest(t)
	p := state(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := install(ctx, p, testPlan(), func(step int) error {
		if step == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	empty(t, p)
	j := crash(t, p, 1)
	s, err := openState(p)
	if err != nil {
		t.Fatal(err)
	}
	ns := j.NetNS
	j.NetNS++
	if err = s.write(j); err != nil {
		t.Fatal(err)
	}
	s.close()
	if err = Recover(p); err == nil || !strings.Contains(err.Error(), "another network namespace") {
		t.Fatal("accepted stale namespace", err)
	}
	j.NetNS = ns
	s, err = openState(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.write(j); err != nil {
		t.Fatal(err)
	}
	s.close()
	if err = Recover(p); err != nil {
		t.Fatal(err)
	}
	empty(t, p)
}

func TestCaptureChild(t *testing.T) {
	if os.Getenv("XDIAL_CAPTURE_CHILD") != "1" {
		t.Skip("child only")
	}
	guest(t)
	step, _ := strconv.Atoi(os.Getenv("XDIAL_CRASH_STEP"))
	_, e := install(context.Background(), os.Getenv("XDIAL_CAPTURE_STATE"), testPlan(), func(s int) error {
		if s == step {
			fmt.Println("CAPTURE_CHILD_READY")
			time.Sleep(time.Minute)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Fatal("child was not killed")
}

func crash(t *testing.T, p string, step int) *journal {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	c := exec.CommandContext(ctx, exe, "-test.run=^TestCaptureChild$", "-test.v")
	c.Env = append(os.Environ(), "XDIAL_CAPTURE_CHILD=1", "XDIAL_CAPTURE_STATE="+p, fmt.Sprintf("XDIAL_CRASH_STEP=%d", step))
	out, e := c.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	c.Stderr = os.Stderr
	if e = c.Start(); e != nil {
		t.Fatal(e)
	}
	scan := bufio.NewScanner(out)
	ready := false
	for scan.Scan() {
		if scan.Text() == "CAPTURE_CHILD_READY" {
			ready = true
			break
		}
	}
	if !ready {
		c.Wait()
		t.Fatal("child failed before ready")
	}
	if e = c.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = c.Wait(); e == nil {
		t.Fatal("child not killed")
	}
	s, e := openState(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.close()
	j, e := s.read()
	if e != nil || j == nil {
		t.Fatal("missing durable intent", e)
	}
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	defer k.close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		present, e := k.tablePresent(j)
		if e != nil {
			t.Fatal(e)
		}
		if !present {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kernel retained dead owner's capture")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return j
}

func TestCaptureCrashRecovery(t *testing.T) {
	guest(t)
	for step := 1; step <= 3; step++ {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			p := state(t)
			crash(t, p, step)
			if e := Recover(p); e != nil {
				t.Fatal(e)
			}
			if e := Recover(p); e != nil {
				t.Fatal(e)
			}
			empty(t, p)
		})
	}
}

func TestCaptureRecoveryRefusesChangedOwnership(t *testing.T) {
	guest(t)
	p := state(t)
	j := crash(t, p, 3)
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	defer k.close()
	// Replace our rule with a different action under the same priority/table.
	if e = k.rt.RuleDel(rule(j)); e != nil {
		t.Fatal(e)
	}
	r := rule(j)
	r.Type = unix.RTN_BLACKHOLE
	if e = k.rt.RuleAdd(r); e != nil {
		t.Fatal(e)
	}
	if e = Recover(p); e == nil {
		t.Fatal("deleted changed action")
	}
	if e = k.rt.RuleDel(r); e != nil {
		t.Fatal("changed rule was removed", e)
	}
	if e = k.rt.RuleAdd(rule(j)); e != nil {
		t.Fatal(e)
	}
	// Replace route with a different private ownership cookie.
	if e = k.rt.RouteDel(route(j)); e != nil {
		t.Fatal(e)
	}
	routeOther := route(j)
	routeOther.Realm ^= 2
	if e = k.rt.RouteAdd(routeOther); e != nil {
		t.Fatal(e)
	}
	if e = Recover(p); e == nil {
		t.Fatal("deleted changed realm")
	}
	if e = k.rt.RouteDel(routeOther); e != nil {
		t.Fatal("changed route was removed", e)
	}
	if e = k.rt.RouteAdd(route(j)); e != nil {
		t.Fatal(e)
	}
	if e = Recover(p); e != nil {
		t.Fatal(e)
	}
	empty(t, p)
}

func TestCaptureStateSafety(t *testing.T) {
	guest(t)
	p := state(t)
	if e := os.Symlink(p, p+"-link"); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(p + "-link")
	if _, e := Install(context.Background(), p+"-link", testPlan()); e == nil {
		t.Fatal("followed state symlink")
	}
	if e := os.WriteFile(filepath.Join(p, "session.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Recover(p); e == nil {
		t.Fatal("accepted corrupt journal")
	}
	if _, e := Install(context.Background(), p, testPlan()); e == nil {
		t.Fatal("overwrote corrupt journal")
	}
	if e := os.Remove(filepath.Join(p, "session.json")); e != nil {
		t.Fatal(e)
	}
	empty(t, p)
	// A foreign unowned nft table is untouched by a complete transaction.
	k, e := openKernel()
	if e != nil {
		t.Fatal(e)
	}
	defer k.close()
	foreign := k.nft.CreateTable(&nftables.Table{Name: "foreign_fixture", Family: nftables.TableFamilyIPv4})
	k.deadline()
	if e = k.nft.Flush(); e != nil {
		t.Fatal(e)
	}
	s, e := Install(context.Background(), p, testPlan())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	k.deadline()
	if _, e = k.nft.ListTable("foreign_fixture"); e != nil {
		t.Fatal("removed foreign table", e)
	}
	k.nft.DelTable(foreign)
	k.deadline()
	if e = k.nft.Flush(); e != nil {
		t.Fatal(e)
	}
	empty(t, p)
}
