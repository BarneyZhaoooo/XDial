//go:build linux && !android

package linuxservice

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/linuxresolver"
	mDNS "github.com/miekg/dns"
)

func TestResolvedClientChild(t *testing.T) {
	if os.Getenv("XDIAL_RESOLVER_CLIENT") != "1" {
		t.Skip("child only")
	}
	isolated(t)
	q := new(mDNS.Msg)
	q.SetQuestion("uid.corp.test.", mDNS.TypeA)
	r, _, e := (&mDNS.Client{Timeout: 3 * time.Second}).Exchange(q, "192.0.2.54:53")
	if e != nil || r == nil || r.Rcode != 0 || len(r.Answer) != 1 {
		t.Fatal("ordinary client DNS", e, r)
	}
	a, ok := r.Answer[0].(*mDNS.A)
	if !ok || a.A.String() != "192.0.2.10" {
		t.Fatal("ordinary client answer", r)
	}
}

func TestResolvedDNSChild(t *testing.T) {
	addr := os.Getenv("XDIAL_RESOLVER_TEST_ADDR")
	if addr == "" {
		t.Skip("child only")
	}
	isolated(t)
	pc, e := net.ListenPacket("udp4", addr+":53")
	if e != nil {
		t.Fatal(e)
	}
	defer pc.Close()
	fmt.Println("DNS_CHILD_READY")
	buf := make([]byte, 65535)
	for {
		n, peer, e := pc.ReadFrom(buf)
		if e != nil {
			return
		}
		var q mDNS.Msg
		if q.Unpack(buf[:n]) != nil {
			continue
		}
		r := new(mDNS.Msg)
		r.SetReply(&q)
		r.Authoritative = true
		if len(q.Question) > 0 && q.Question[0].Qtype == mDNS.TypeA {
			r.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: q.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60}, A: net.ParseIP(os.Getenv("XDIAL_RESOLVER_TEST_ANSWER"))}}
		}
		b, _ := r.Pack()
		pc.WriteTo(b, peer)
	}
}

func TestInstalledResolvedLifecycle(t *testing.T) {
	isolated(t)
	dmi, _ := os.ReadFile("/sys/class/dmi/id/product_name")
	pid1, _ := os.ReadFile("/proc/1/comm")
	if os.Getenv("XDIAL_DISTRO_VM") != "1" || strings.TrimSpace(string(dmi)) != "XDial-isolated-distro" {
		t.Skip("full guest only")
	}
	if os.Geteuid() != 0 || strings.TrimSpace(string(pid1)) != "systemd" {
		t.Fatal("guest root/systemd required")
	}
	const installer = "/opt/xdial-test/xdial-service/install.sh"
	const socket = "/run/xdial-service/control.sock"
	const state = "/var/lib/xdial-service"
	for _, mod := range []string{"nft_tproxy", "nft_socket", "tcp_diag", "udp_diag", "veth"} {
		cmdOK(t, "modprobe", mod)
	}
	cmdOK(t, "systemctl", "start", "systemd-resolved")
	// Configure only the disposable guest's own links/namespaces. Two upstream
	// servers are actually reached over different veth links, not injected hosts.
	for i, name := range []string{"rdna", "rdnb"} {
		ns := "xdial-" + name
		peer := name + "-peer"
		network := fmt.Sprintf("10.88.%d.", i+1)
		cmdOK(t, "ip", "netns", "add", ns)
		t.Cleanup(func() { exec.Command("ip", "netns", "del", ns).Run(); exec.Command("ip", "link", "del", name).Run() })
		cmdOK(t, "ip", "link", "add", name, "type", "veth", "peer", "name", peer)
		cmdOK(t, "ip", "link", "set", peer, "netns", ns)
		cmdOK(t, "ip", "addr", "add", network+"1/24", "dev", name)
		cmdOK(t, "ip", "link", "set", name, "up")
		cmdOK(t, "ip", "netns", "exec", ns, "ip", "addr", "add", network+"2/24", "dev", peer)
		cmdOK(t, "ip", "netns", "exec", ns, "ip", "link", "set", peer, "up")
		cmdOK(t, "ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up")
		c := exec.Command("ip", "netns", "exec", ns, "/service-tests", "-test.run=^TestResolvedDNSChild$", "-test.v")
		c.Env = append(os.Environ(), "XDIAL_RESOLVER_TEST_ADDR="+network+"2", fmt.Sprintf("XDIAL_RESOLVER_TEST_ANSWER=192.0.2.%d", (i+1)*10))
		pipe, e := c.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		c.Stderr = os.Stderr
		if e = c.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Process.Kill(); c.Wait() })
		ready := make(chan bool, 1)
		go func() {
			s := bufio.NewScanner(pipe)
			for s.Scan() {
				if s.Text() == "DNS_CHILD_READY" {
					ready <- true
					return
				}
			}
			ready <- false
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("DNS child failed")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("DNS child not ready")
		}
	}
	configure := func() {
		cmdOK(t, "resolvectl", "dns", "rdna", "10.88.1.2")
		cmdOK(t, "resolvectl", "dns", "rdnb", "10.88.2.2")
		cmdOK(t, "resolvectl", "domain", "rdna", "~corp.test")
		cmdOK(t, "resolvectl", "domain", "rdnb", "~.", "lab.test")
		for _, link := range []string{"rdna", "rdnb"} {
			cmdOK(t, "resolvectl", "default-route", link, "no")
			cmdOK(t, "resolvectl", "llmnr", link, "no")
			cmdOK(t, "resolvectl", "mdns", link, "no")
			cmdOK(t, "resolvectl", "dnssec", link, "no")
		}
		cmdOK(t, "resolvectl", "flush-caches")
	}
	configure()
	if e := os.Remove("/etc/resolv.conf"); e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if e := os.Symlink("/run/systemd/resolve/stub-resolv.conf", "/etc/resolv.conf"); e != nil {
		t.Fatal(e)
	}
	query := func(proto, target, name, want string) {
		t.Helper()
		q := new(mDNS.Msg)
		q.SetQuestion(name, mDNS.TypeA)
		r, _, e := (&mDNS.Client{Net: proto, Timeout: 3 * time.Second}).Exchange(q, target)
		if e != nil {
			t.Fatalf("DNS %s via %s: %v", name, target, e)
		}
		if r.Id != q.Id || r.Rcode != mDNS.RcodeSuccess || len(r.Answer) != 1 {
			t.Fatalf("DNS result %s: %v", name, r)
		}
		a, ok := r.Answer[0].(*mDNS.A)
		if !ok || a.A.String() != want {
			t.Fatalf("DNS answer %s: %v expected %s", name, r.Answer, want)
		}
	}
	query("udp", "127.0.0.53:53", "split.corp.test.", "192.0.2.10")
	query("udp", "127.0.0.53:53", "public.example.", "192.0.2.20")
	l, e := linuxresolver.Open(context.Background())
	if e != nil {
		t.Fatal("real resolver lease:", e)
	}
	if e = l.Validate(context.Background()); e != nil {
		l.Close()
		t.Fatal(e)
	}
	l.Close()
	cmdOK(t, "ip", "route", "add", "192.0.2.54/32", "dev", "lo")
	t.Cleanup(func() { exec.Command("ip", "route", "del", "192.0.2.54/32", "dev", "lo").Run() })
	cmdOK(t, installer, "install", "1001", "65536", "131072", "50000", "32000", "127.0.0.1:31234")
	t.Cleanup(func() {
		out, _ := exec.Command("journalctl", "-u", "xdial-service", "--no-pager", "-n", "100").CombinedOutput()
		t.Logf("service journal:\n%s", out)
		exec.Command("systemctl", "stop", "xdial-service").Run()
	})
	cmdOK(t, "systemctl", "start", "xdial-service")
	require := func(op string, revision uint64, profile []byte) {
		t.Helper()
		r := callAsUser(t, socket, op, revision, profile)
		if r.Code != "ok" {
			t.Fatalf("%s: %s", op, r.Code)
		}
	}
	require("status", 0, nil)
	// A shared native NSS path cannot be mislabeled as application-aware DNS.
	// Assert rejection, then explicitly select wire DNS in this disposable guest.
	nss, e := os.ReadFile("/etc/nsswitch.conf")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.WriteFile("/etc/nsswitch.conf", nss, 0644) })
	setHosts := func(value string) {
		t.Helper()
		lines := strings.Split(string(nss), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "hosts:") {
				lines[i] = "hosts: " + value
			}
		}
		if e := os.WriteFile("/etc/nsswitch.conf", []byte(strings.Join(lines, "\n")), 0644); e != nil {
			t.Fatal(e)
		}
	}
	setHosts("files resolve [!UNAVAIL=return] dns")
	if r := callAsUser(t, socket, "connect", 0, serviceProfile(9, false)); r.Code != "unsupported_native_resolver_ingress" {
		t.Fatal("native NSS bypass was admitted", r.Code)
	}
	setHosts("files dns")
	require("connect", 0, serviceProfile(9, false))
	// These ordinary root sockets have no mark, proving root is not exempted.
	// The original target has no listener: success requires real TProxy + the
	// protected core->stub hop + resolved's exempted cgroup upstream sockets.
	query("udp", "192.0.2.54:53", "split.corp.test.", "192.0.2.10")
	query("tcp", "192.0.2.54:53", "public.example.", "192.0.2.20")
	mainPID, e := exec.Command("systemctl", "show", "--property=MainPID", "--value", "systemd-resolved").Output()
	if e != nil {
		t.Fatal(e)
	}
	proc, e := os.Stat("/proc/" + strings.TrimSpace(string(mainPID)))
	if e != nil {
		t.Fatal(e)
	}
	resolverUID := proc.Sys().(*syscall.Stat_t).Uid
	for _, uid := range []uint32{1001, resolverUID} {
		c := exec.Command("/service-tests", "-test.run=^TestResolvedClientChild$", "-test.v")
		c.Env = append(os.Environ(), "XDIAL_RESOLVER_CLIENT=1")
		c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, NoSetGroups: true}}
		if out, e := c.CombinedOutput(); e != nil {
			t.Fatalf("client uid %d outside resolver cgroup: %v %s", uid, e, out)
		}
	}
	// Change OS domain routing while connected, without rewriting any XDial
	// rule. The shared core must not retain the old per-link answer in its cache.
	cmdOK(t, "resolvectl", "domain", "rdna", "~.")
	cmdOK(t, "resolvectl", "domain", "rdnb", "~corp.test", "lab.test")
	query("udp", "192.0.2.54:53", "split.corp.test.", "192.0.2.20")
	query("udp", "192.0.2.54:53", "public.example.", "192.0.2.10")
	cmdOK(t, "systemctl", "restart", "systemd-resolved")
	deadline := time.Now().Add(8 * time.Second)
	stopped := false
	for time.Now().Before(deadline) {
		r := callAsUser(t, socket, "status", 0, nil)
		if strings.Contains(string(r.Payload), `"desired":false`) {
			stopped = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("resolver retirement did not withdraw connection")
	}
	configure()
	// Observe the new revision/instance, then explicitly reconnect after the
	// resolver's restart; no automatic connection or reused identity lease.
	r := callAsUser(t, socket, "status", 0, nil)
	var st Status
	if e := strictJSON(r.Payload, &st); e != nil {
		t.Fatal(e)
	}
	require("connect", st.Revision, serviceProfile(9, false))
	query("udp", "192.0.2.54:53", "fresh.corp.test.", "192.0.2.10")
	waitDisconnected := func() Status {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			r := callAsUser(t, socket, "status", 0, nil)
			if strings.Contains(string(r.Payload), `"desired":false`) {
				var state Status
				if e := strictJSON(r.Payload, &state); e != nil {
					t.Fatal(e)
				}
				return state
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("unsupported native resolver ingress did not withdraw capture")
		return Status{}
	}
	setHosts("files resolve [!UNAVAIL=return] dns")
	st = waitDisconnected()
	if r := callAsUser(t, socket, "connect", st.Revision, serviceProfile(9, false)); r.Code != "unsupported_native_resolver_ingress" {
		t.Fatal("changed NSS bypass was admitted", r.Code)
	}
	setHosts("files dns")
	require("connect", st.Revision, serviceProfile(9, false))
	if e := os.Mkdir("/run/nscd", 0755); e != nil {
		t.Fatal(e)
	}
	cache, e := net.Listen("unix", "/run/nscd/socket")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cache.Close(); os.Remove("/run/nscd") })
	st = waitDisconnected()
	if r := callAsUser(t, socket, "connect", st.Revision, serviceProfile(9, false)); r.Code != "unsupported_native_resolver_ingress" {
		t.Fatal("shared NSS cache was admitted", r.Code)
	}
	cache.Close()
	os.Remove("/run/nscd")
	require("connect", st.Revision, serviceProfile(9, false))
	query("udp", "192.0.2.54:53", "final.corp.test.", "192.0.2.10")
	cmdOK(t, installer, "remove")
	if _, e := os.Stat(filepath.Join(state, "capture", "session.json")); !os.IsNotExist(e) {
		t.Fatal("capture journal remains", e)
	}
	if e := os.RemoveAll(state); e != nil {
		t.Fatal(e)
	} // disposable guest only; next test installs fresh
	t.Log("XDIAL_DISTRO_RESOLVED_OK")
}
