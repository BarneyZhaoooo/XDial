//go:build linux && !android

package libbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	"golang.org/x/sys/unix"
)

const linuxTestBypass = 0x20000

func linuxTrafficConfig(answer string, blocked bool) string {
	var doc map[string]any
	_ = json.Unmarshal([]byte(nativeTestConfig(answer, false)), &doc)
	doc["log"] = map[string]any{"level": "debug"}
	route := doc["route"].(map[string]any)
	route["default_mark"] = linuxTestBypass
	route["find_process"] = true
	if blocked {
		route["rules"] = append(route["rules"].([]any), map[string]any{"process_path": []string{"/app-block"}, "action": "reject"})
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func TestLinuxTProxyProtectionContract(t *testing.T) {
	valid := linuxTrafficConfig("192.0.2.10", false)
	if err := validateLinuxTProxyConfig(valid, linuxTestBypass); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(map[string]any){
		func(d map[string]any) { delete(d["route"].(map[string]any), "default_mark") },
		func(d map[string]any) { d["route"].(map[string]any)["default_mark"] = linuxTestBypass | 0x10000 },
		func(d map[string]any) { d["outbounds"] = []any{map[string]any{"type": "vpn"}} },
		func(d map[string]any) { d["endpoints"] = []any{map[string]any{"type": "tailscale"}} },
		func(d map[string]any) { d["dns"].(map[string]any)["servers"] = []any{map[string]any{"type": "local"}} },
		func(d map[string]any) { d["outbounds"] = []any{map[string]any{"type": "direct", "routing_mark": 1}} },
		func(d map[string]any) { d["outbounds"] = []any{map[string]any{"type": "direct", "netns": "other"}} },
		func(d map[string]any) {
			d["route"].(map[string]any)["rule_set"] = []any{map[string]any{"type": "remote"}}
		},
	} {
		var doc map[string]any
		_ = json.Unmarshal([]byte(valid), &doc)
		change(doc)
		b, _ := json.Marshal(doc)
		if err := validateLinuxTProxyConfig(string(b), linuxTestBypass); err == nil {
			t.Fatal("accepted unprotected configuration", string(b))
		}
	}
	l := New(nil)
	if err := l.ConfigureLinuxTProxy(3); err == nil {
		t.Fatal("accepted multi-bit mark")
	}
	if err := l.ConfigureLinuxTProxy(linuxTestBypass); err != nil {
		t.Fatal(err)
	}
	if err := l.SetNativeFlowIngress(false); err == nil {
		t.Fatal("removed protected ingress")
	}
	if err := l.StartStandalone(nativeTestConfig("192.0.2.10", false)); err == nil {
		l.Stop()
		t.Fatal("started without protection")
	}
	l.vpn = &startTestVPNClient{connectErr: errors.New("unexpected protocol dial")}
	for _, start := range []func() error{
		func() error { return l.Start("unused.invalid", "unused", "unused", valid) },
		func() error { return l.StartStandaloneWithTailscaleLineID("line", "identity", valid) },
	} {
		if err := start(); err == nil || !strings.Contains(err.Error(), "native adapter protocol socket capability") {
			t.Fatal("protocol setup escaped guard", err)
		}
	}
}

func requireTransparentGuest(t *testing.T) {
	t.Helper()
	if os.Getenv("XDIAL_ISOLATED_VM") != "1" {
		t.Skip("requires isolated no-NIC Linux guest")
	}
	b, err := os.ReadFile("/proc/cmdline")
	if err != nil || !strings.Contains(string(b), "xdial_isolated_vm=transparent") || os.Geteuid() != 0 {
		t.Fatal("refuse kernel mutation outside isolated guest")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			t.Fatal("guest must have no external NIC")
		}
	}
}

func guestCommand(t *testing.T, input, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, b)
	}
	return string(b)
}

func markedSocket(_ string, _ string, raw syscall.RawConn) error {
	var err error
	if e := raw.Control(func(fd uintptr) {
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, linuxTestBypass)
		// The synthetic server shares this guest's address space with the proxy.
		// Real remote servers do not occupy the proxy's transparent reply port.
		if err == nil {
			err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		}
	}); e != nil {
		return e
	}
	return err
}

func guestDNS(t *testing.T, expected string) {
	t.Helper()
	c := &mDNS.Client{Net: "udp", Timeout: 5 * time.Second}
	q := new(mDNS.Msg)
	q.SetQuestion("flow.test.", mDNS.TypeA)
	r, _, err := c.Exchange(q, "192.0.2.53:53")
	if err != nil {
		t.Fatal("kernel DNS", err)
	}
	if r.Id != q.Id || len(r.Answer) != 1 || r.Answer[0].(*mDNS.A).A.String() != expected {
		t.Fatal("unexpected kernel DNS answer", r)
	}
}

// This is an ordinary application process: it does not call RouteNative*, set
// marks, supply a PID/path, or know the proxy's listener address.
func TestLinuxTransparentApplication(t *testing.T) {
	if os.Getenv("XDIAL_GUEST_APP") != "1" {
		t.Skip("subprocess only")
	}
	requireTransparentGuest(t)
	c, err := net.DialTimeout("tcp4", os.Getenv("XDIAL_ECHO_ADDR"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = c.Write([]byte("native"))
	b := make([]byte, 6)
	if err == nil {
		_, err = io.ReadFull(c, b)
	}
	if os.Getenv("XDIAL_EXPECT_REJECT") == "1" {
		var timeout net.Error
		if err == nil || errors.As(err, &timeout) && timeout.Timeout() {
			t.Fatal("expected explicit connection close, not timeout", err)
		}
		return
	}
	if err != nil || string(b) != "native" {
		t.Fatal("application echo", err)
	}
}

func TestLinuxTransparentKernelTraffic(t *testing.T) {
	requireTransparentGuest(t)
	// Every command below runs only after the guest guard. No host installer is
	// invoked. The production installation/permission transaction remains separate.
	lc := net.ListenConfig{Control: markedSocket}
	tcp, err := lc.Listen(context.Background(), "tcp4", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	observedUDP := make(chan net.Addr, 1)
	go func() {
		b := make([]byte, 2048)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			select {
			case observedUDP <- a:
			default:
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	l := New(nil)
	snapshot, name, index := nativeTestUnderlay(t)
	l.SetDefaultInterface(name, index)
	if err = l.SetNetworkInterfaces(snapshot); err != nil {
		t.Fatal(err)
	}
	if err = l.ConfigureLinuxTProxy(linuxTestBypass); err != nil {
		t.Fatal(err)
	}
	if err = l.StartStandalone(linuxTrafficConfig("192.0.2.10", false)); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if err = l.StartLinuxTProxy("127.0.0.1:15080", linuxTestBypass); err != nil {
		t.Fatal(err)
	}
	if err = l.StartLinuxTProxy("127.0.0.1:15081", linuxTestBypass); err == nil {
		t.Fatal("duplicate listener")
	}
	guestCommand(t, "", "ip", "route", "add", "192.0.2.0/24", "dev", "lo")
	defer guestCommand(t, "", "ip", "route", "del", "192.0.2.0/24", "dev", "lo")
	guestCommand(t, "", "ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "50000")
	defer guestCommand(t, "", "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "50000")
	guestCommand(t, "", "ip", "rule", "add", "priority", "32000", "fwmark", "0x10000/0x10000", "lookup", "50000")
	defer guestCommand(t, "", "ip", "rule", "del", "priority", "32000", "fwmark", "0x10000/0x10000", "lookup", "50000")
	rules := `add table ip xdial_guest
add chain ip xdial_guest output { type route hook output priority mangle; policy accept; }
add rule ip xdial_guest output meta mark & 0x20000 != 0 counter return
add rule ip xdial_guest output meta l4proto { tcp, udp } meta mark set meta mark | 0x10000 counter
add chain ip xdial_guest input { type filter hook prerouting priority mangle; policy accept; }
add rule ip xdial_guest input meta mark & 0x20000 != 0 counter return
add rule ip xdial_guest input meta mark & 0x10000 != 0 meta l4proto { tcp, udp } tproxy to 127.0.0.1:15080 counter accept
`
	guestCommand(t, rules, "nft", "-f", "-")
	captureInstalled := true
	defer func() {
		if captureInstalled {
			guestCommand(t, "", "nft", "delete", "table", "ip", "xdial_guest")
		}
	}()
	old, err := net.DialTimeout("tcp4", tcp.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	echoNative(t, old)
	guestDNS(t, "192.0.2.10")
	u, err := net.DialTimeout("udp4", udp.LocalAddr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(5 * time.Second))
	u.Write([]byte("udp-kernel"))
	b := make([]byte, 100)
	n, err := u.Read(b)
	if err != nil || string(b[:n]) != "udp-kernel" {
		t.Fatal("original UDP target/reply", err)
	}
	select {
	case source := <-observedUDP:
		if source.String() == u.LocalAddr().String() {
			t.Fatal("UDP bypassed core forwarding")
		}
	case <-time.After(time.Second):
		t.Fatal("UDP server received no packet")
	}
	guestDNS(t, "192.0.2.10")
	if err = l.PrepareSwitch(nativeTestConfig("192.0.2.30", false), "", snapshot, name, index); err == nil {
		t.Fatal("candidate removed protection")
	}
	if err = l.PrepareSwitch(linuxTrafficConfig("192.0.2.30", false), "unaudited-identity", snapshot, name, index); err == nil || !strings.Contains(err.Error(), "native adapter protocol socket capability") {
		t.Fatal("candidate protocol escaped protection", err)
	}
	guestDNS(t, "192.0.2.10")
	echoNative(t, old)
	app := func(path string, reject bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, path, "-test.run=^TestLinuxTransparentApplication$", "-test.v")
		c.Env = append(os.Environ(), "XDIAL_GUEST_APP=1", "XDIAL_ECHO_ADDR="+tcp.Addr().String(), fmt.Sprintf("XDIAL_EXPECT_REJECT=%d", map[bool]int{false: 0, true: 1}[reject]))
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("application %s: %v %s", path, e, out)
		}
		t.Logf("%s: %s", path, out)
	}
	app("/app-block", false)
	if err = l.PrepareSwitch(linuxTrafficConfig("192.0.2.20", true), "", snapshot, name, index); err != nil {
		t.Fatal(err)
	}
	guestDNS(t, "192.0.2.10")
	echoNative(t, old)
	if err = l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	guestDNS(t, "192.0.2.20")
	echoNative(t, old)
	app("/app-block", true)
	app("/app-allow", false)
	if err = l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	old.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = old.Read(b); err == nil {
		t.Fatal("old TCP did not close")
	} else {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("retirement timed out")
		}
	}
	guestDNS(t, "192.0.2.20")
	app("/app-allow", false)
	t.Log(guestCommand(t, "", "nft", "list", "table", "ip", "xdial_guest"))
	current, err := net.DialTimeout("tcp4", tcp.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	echoNative(t, current)
	guestCommand(t, "", "nft", "delete", "table", "ip", "xdial_guest")
	captureInstalled = false
	if err = l.Stop(); err != nil {
		t.Fatal(err)
	}
	current.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = current.Read(b); err == nil {
		t.Fatal("Stop did not close current TCP")
	} else {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("Stop close timed out")
		}
	}
	rebound, err := net.Listen("tcp4", "127.0.0.1:15080")
	if err != nil {
		t.Fatal("listener retained after Stop", err)
	}
	rebound.Close()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err = bindSocketToInterface(fd, index); err != nil {
		t.Fatal(err)
	}
	bound, err := unix.GetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE)
	if err != nil || bound != name {
		t.Fatal("wrong Underlay binding", bound, err)
	}
}
