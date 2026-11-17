//go:build linux && !android

package linuxservice

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/controlpipe"
	mDNS "github.com/miekg/dns"
)

// These tests intentionally use the installed paths, package script, PID 1 and
// kernel of a disposable full distribution. Never run them on a shared host.
func TestInstalledSystemdLifecycle(t *testing.T) {
	isolated(t)
	dmi, _ := os.ReadFile("/sys/class/dmi/id/product_name")
	if os.Getenv("XDIAL_DISTRO_VM") != "1" || strings.TrimSpace(string(dmi)) != "XDial-isolated-distro" {
		t.Skip("full distribution guest only")
	}
	pid1, _ := os.ReadFile("/proc/1/comm")
	if os.Geteuid() != 0 || strings.TrimSpace(string(pid1)) != "systemd" {
		t.Fatal("guest root and actual systemd required")
	}
	const socket = "/run/xdial-service/control.sock"
	const state = "/var/lib/xdial-service"
	const installer = "/opt/xdial-test/xdial-service/install.sh"
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("fresh guest required", err)
	}
	osRelease, _ := os.ReadFile("/etc/os-release")
	t.Log(string(osRelease))
	cmdOK(t, "modprobe", "nft_tproxy")
	cmdOK(t, "modprobe", "tcp_diag")
	cmdOK(t, "modprobe", "udp_diag")
	cmdOK(t, "ip", "addr", "add", "192.0.2.53/32", "dev", "lo")
	defer exec.Command("ip", "addr", "del", "192.0.2.53/32", "dev", "lo").Run()
	cmdOK(t, "ip", "route", "add", "192.0.2.54/32", "dev", "lo")
	defer exec.Command("ip", "route", "del", "192.0.2.54/32", "dev", "lo").Run()
	// A controlled resolver fixture, NOT evidence of native resolved support.
	if e := os.Remove("/etc/resolv.conf"); e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if e := os.WriteFile("/etc/resolv.conf", []byte("nameserver 192.0.2.53\n"), 0644); e != nil {
		t.Fatal(e)
	}
	before, _ := exec.Command("ip", "-4", "rule", "show").Output()
	lc := markedListenConfig(1 << 17)
	dns, e := lc.ListenPacket(context.Background(), "udp4", "192.0.2.53:53")
	if e != nil {
		t.Fatal(e)
	}
	defer dns.Close()
	var queries atomic.Int32
	go func() {
		b := make([]byte, 4096)
		for {
			n, a, e := dns.ReadFrom(b)
			if e != nil {
				return
			}
			var q mDNS.Msg
			if q.Unpack(b[:n]) != nil {
				continue
			}
			queries.Add(1)
			r := new(mDNS.Msg)
			r.SetReply(&q)
			if len(q.Question) > 0 {
				r.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: q.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 1}, A: net.IPv4(192, 0, 2, 10)}}
			}
			p, _ := r.Pack()
			dns.WriteTo(p, a)
		}
	}()
	echo, e := lc.Listen(context.Background(), "tcp4", "127.0.0.2:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	cmdOK(t, installer, "install", "1001", "65536", "131072", "50000", "32000", "127.0.0.1:31234")
	if e := exec.Command("systemctl", "is-active", "--quiet", "xdial-service").Run(); e == nil {
		t.Fatal("installation started service")
	}
	if _, e := os.Stat(socket); !os.IsNotExist(e) {
		t.Fatal("installation created ingress", e)
	}
	t.Cleanup(func() {
		out, _ := exec.Command("journalctl", "-u", "xdial-service", "--no-pager", "-n", "100").CombinedOutput()
		t.Logf("systemd journal:\n%s", out)
		exec.Command("systemctl", "stop", "xdial-service").Run()
	})
	// A second install must not overwrite the owner's current files or policy.
	policyBefore, _ := os.ReadFile(filepath.Join(state, "policy.json"))
	if e := exec.Command(installer, "install", "1002", "65536", "131072", "50000", "32000", "127.0.0.1:31234").Run(); e == nil {
		t.Fatal("duplicate installation accepted")
	}
	policyAfter, _ := os.ReadFile(filepath.Join(state, "policy.json"))
	if string(policyBefore) != string(policyAfter) {
		t.Fatal("duplicate install changed policy")
	}
	waitStatus := func(old string) Status {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			request, _ := json.Marshal(controlpipe.Request{Operation: "status"})
			out, e := childCommand(t, 1001, "request", socket, request)
			if e == nil {
				for _, line := range strings.Split(string(out), "\n") {
					if strings.HasPrefix(line, "RESPONSE ") {
						var r controlpipe.Response
						if json.Unmarshal([]byte(strings.TrimPrefix(line, "RESPONSE ")), &r) == nil && r.Code == "ok" {
							var st Status
							if json.Unmarshal(r.Payload, &st) == nil && st.Instance != "" && st.Instance != old {
								return st
							}
						}
					}
				}
			}

			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("installed service did not become ready")
		return Status{}
	}
	require := func(r controlpipe.Response) {
		t.Helper()
		if r.Code != "ok" {
			t.Fatalf("service rejected: %+v", r)
		}
	}
	// Reproduce the real unit failure: without NET_BIND_SERVICE, the core
	// reaches the upstream DNS fixture but cannot bind the transparent reply
	// socket to the original privileged destination port. TCP still works.
	threshold, _ := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if strings.TrimSpace(string(threshold)) != "1024" {
		t.Fatalf("unexpected guest low-port boundary: %s", threshold)
	}
	const overrideDir = "/run/systemd/system/xdial-service.service.d"
	if e := os.Mkdir(overrideDir, 0755); e != nil {
		t.Fatal(e)
	}
	override := filepath.Join(overrideDir, "90-guest-capability-test.conf")
	if e := os.WriteFile(override, []byte("[Service]\nCapabilityBoundingSet=~CAP_NET_BIND_SERVICE\n"), 0644); e != nil {
		t.Fatal(e)
	}
	cmdOK(t, "systemctl", "daemon-reload")
	cmdOK(t, "systemctl", "start", "xdial-service")
	waitStatus("")
	require(callAsUser(t, socket, "status", 0, nil))
	require(callAsUser(t, socket, "connect", 0, serviceProfile(9, false)))
	if out, e := childCommand(t, 1001, "echo", "", []byte(echo.Addr().String())); e != nil {
		t.Fatal(e, string(out))
	}
	q := new(mDNS.Msg)
	q.SetQuestion("systemd.test.", mDNS.TypeA)
	_, _, denied := (&mDNS.Client{Timeout: time.Second}).Exchange(q, "192.0.2.54:53")
	if timeout, ok := denied.(net.Error); !ok || !timeout.Timeout() || queries.Load() == 0 {
		t.Fatalf("missing low-port negative control: %v queries=%d", denied, queries.Load())
	}
	t.Logf("without NET_BIND_SERVICE: TCP passed, upstream DNS queries=%d, privileged reply denied", queries.Load())
	restrictedInstance := testInstance
	if e := os.Remove(override); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(overrideDir); e != nil {
		t.Fatal(e)
	}
	cmdOK(t, "systemctl", "daemon-reload")
	cmdOK(t, "systemctl", "restart", "xdial-service")
	waitStatus(restrictedInstance)
	require(callAsUser(t, socket, "status", 0, nil))
	require(callAsUser(t, socket, "connect", 0, serviceProfile(9, false)))
	answer, _, e := (&mDNS.Client{Timeout: 2 * time.Second}).Exchange(q, "192.0.2.54:53")
	if e != nil || answer == nil || len(answer.Answer) != 1 || answer.Answer[0].(*mDNS.A).A.String() != "192.0.2.10" {
		t.Fatalf("installed DNS: %v %+v", e, answer)
	}
	if out, e := childCommand(t, 1001, "echo", "", []byte(echo.Addr().String())); e != nil {
		t.Fatal(e, string(out))
	}
	old := testInstance
	cmdOK(t, "systemctl", "restart", "xdial-service")
	st := waitStatus(old)
	var connection struct{ Desired bool }
	json.Unmarshal(st.Connection, &connection)
	if st.Revision != 0 || st.CleanupRequired || connection.Desired {
		t.Fatalf("restart did not reset: %+v", st)
	}
	require(callAsUser(t, socket, "status", 0, nil))
	if _, e := os.Stat(filepath.Join(state, "capture/session.json")); !os.IsNotExist(e) {
		t.Fatal("graceful restart retained journal", e)
	}
	require(callAsUser(t, socket, "connect", 0, serviceProfile(9, false)))
	old = testInstance
	cmdOK(t, "systemctl", "kill", "--signal=KILL", "--kill-whom=main", "xdial-service")
	st = waitStatus(old)
	json.Unmarshal(st.Connection, &connection)
	if st.Revision != 0 || st.CleanupRequired || connection.Desired {
		t.Fatalf("crash restart did not recover: %+v", st)
	}
	if _, e := os.Stat(filepath.Join(state, "capture/session.json")); !os.IsNotExist(e) {
		t.Fatal("crash restart retained journal", e)
	}
	require(callAsUser(t, socket, "status", 0, nil))
	require(callAsUser(t, socket, "connect", 0, serviceProfile(9, false)))
	// Actual removal while connected must first withdraw capture and stop service.
	cmdOK(t, installer, "remove")
	for _, path := range []string{"/usr/local/libexec/xdial-service", "/etc/systemd/system/xdial-service.service", socket, filepath.Join(state, "capture/session.json")} {
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("remove retained active component", path, e)
		}
	}
	policyAfter, _ = os.ReadFile(filepath.Join(state, "policy.json"))
	if string(policyBefore) != string(policyAfter) {
		t.Fatal("removal lost policy")
	}
	after, _ := exec.Command("ip", "-4", "rule", "show").Output()
	if string(before) != string(after) {
		t.Fatalf("policy routes leaked before=%s after=%s", before, after)
	}
	routes, e := exec.Command("ip", "-4", "route", "show", "table", "50000").CombinedOutput()
	if len(strings.TrimSpace(string(routes))) > 0 && e == nil {
		t.Fatalf("capture routes leaked: %s", routes)
	}
	t.Log("XDIAL_DISTRO_SYSTEMD_OK")
}
