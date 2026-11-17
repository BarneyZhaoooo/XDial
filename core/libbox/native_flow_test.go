//go:build (windows || linux) && !android

package libbox

import (
	"encoding/json"
	"fmt"
	R "github.com/sagernet/sing-box/route/rule"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	M "github.com/sagernet/sing/common/metadata"
)

func nativeTestConfig(answer string, reject bool) string {
	rules := `{"protocol":"dns","action":"hijack-dns"}`
	if reject {
		rules += `,{"process_path":["blocked-fixture"],"action":"reject"}`
	}
	return fmt.Sprintf(`{"log":{"disabled":true},"dns":{"servers":[{"type":"hosts","tag":"controlled","predefined":{"flow.test":[%q]}}],"final":"controlled"},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[{"action":"sniff"},%s],"final":"direct"}}`, answer, rules)
}

func nativeTestUnderlay(t *testing.T) (string, string, int) {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 && iface.Flags&net.FlagUp != 0 {
			raw, _ := json.Marshal([]map[string]any{{"name": iface.Name, "index": iface.Index, "type": "other"}})
			return string(raw), iface.Name, iface.Index
		}
	}
	t.Fatal("no loopback interface")
	return "", "", 0
}
func newNativeTestRuntime(t *testing.T, config string) *Libbox {
	t.Helper()
	l := New(nil)
	snapshot, name, index := nativeTestUnderlay(t)
	l.SetDefaultInterface(name, index)
	if err := l.SetNetworkInterfaces(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := l.SetNativeFlowIngress(true); err != nil {
		t.Fatal(err)
	}
	if err := l.StartStandalone(config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Stop() })
	return l
}

func nativeDNSAnswer(t *testing.T, l *Libbox) string {
	t.Helper()
	query := new(mDNS.Msg)
	query.SetQuestion("flow.test.", mDNS.TypeA)
	raw, _ := query.Pack()
	dest := netip.MustParseAddrPort("192.0.2.53:53")
	flow := newMemoryFlowPacketConn()
	defer flow.Close()
	flow.incoming <- memoryFlowDatagram{raw, M.SocksaddrFromNetIP(dest)}
	done := make(chan error, 1)
	go func() {
		done <- l.RouteNativeUDP(flow, NativeFlow{Source: netip.MustParseAddrPort("127.0.0.1:41001"), Destination: dest, UserID: -1}, nil)
	}()
	defer func() {
		flow.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("DNS worker did not exit")
		}
	}()

	select {
	case reply := <-flow.outgoing:
		var response mDNS.Msg
		if err := response.Unpack(reply.payload); err != nil {
			t.Fatal(err)
		}
		if response.Id != query.Id || len(response.Answer) != 1 {
			t.Fatal("invalid DNS reply")
		}
		answer, ok := response.Answer[0].(*mDNS.A)
		if !ok {
			t.Fatal("not an A answer")
		}
		return answer.A.String()
	case <-time.After(5 * time.Second):
		t.Fatal("DNS flow timed out")
	}
	return ""
}

func nativeTCP(t *testing.T, l *Libbox, destination netip.AddrPort, process string, closed ...chan error) net.Conn {
	t.Helper()
	app, ingress := net.Pipe()
	t.Cleanup(func() { app.Close() })
	done := make(chan error, 1)
	go func() {
		done <- l.RouteNativeTCP(ingress, NativeFlow{Source: netip.MustParseAddrPort("127.0.0.1:41002"), Destination: destination, ProcessPath: process, UserID: -1}, func(err error) {
			if len(closed) > 0 {
				select {
				case closed[0] <- err:
				default:
				}
			}
		})
	}()
	t.Cleanup(func() {
		app.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("native TCP admission stuck")
		}
	})
	return app
}

func echoNative(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("native")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 6)
	if _, err := io.ReadFull(conn, reply); err != nil || string(reply) != "native" {
		t.Fatal("echo failed", err)
	}
}

func TestNativeFlowSwitchPreservesTCPAndCommitsDNS(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	l := newNativeTestRuntime(t, nativeTestConfig("192.0.2.10", false))
	dest := listener.Addr().(*net.TCPAddr).AddrPort()
	old := nativeTCP(t, l, dest, "blocked-fixture")
	echoNative(t, old)
	if got := nativeDNSAnswer(t, l); got != "192.0.2.10" {
		t.Fatal(got)
	}
	snapshot, name, index := nativeTestUnderlay(t)
	if err := l.PrepareSwitch(nativeTestConfig("192.0.2.20", true), "", snapshot, name, index); err != nil {
		t.Fatal(err)
	}
	echoNative(t, old)
	if got := nativeDNSAnswer(t, l); got != "192.0.2.10" {
		t.Fatal("candidate affected active DNS", got)
	}
	if err := l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	echoNative(t, old)
	if got := nativeDNSAnswer(t, l); got != "192.0.2.20" {
		t.Fatal("DNS did not commit", got)
	}
	rejected := make(chan error, 1)
	blocked := nativeTCP(t, l, dest, "blocked-fixture", rejected)
	blocked.SetDeadline(time.Now().Add(time.Second))
	_, _ = blocked.Write([]byte("blocked"))
	select {
	case err := <-rejected:
		if !R.IsRejected(err) {
			t.Fatal("flow did not receive a sing-box rejection", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("rejection callback missing")
	}

	current := nativeTCP(t, l, dest, "allowed-fixture")
	echoNative(t, current)
	if err := l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	old.SetDeadline(time.Now().Add(time.Second))
	if _, err := old.Write([]byte("retired")); err == nil {
		t.Fatal("retired flow survived")
	}
	echoNative(t, current)
	l.Stop()
	current.SetDeadline(time.Now().Add(time.Second))
	if _, err := current.Write([]byte("stopped")); err == nil {
		t.Fatal("Stop retained flow")
	}
}

func TestNativeFlowRejectsInvalidModesAndListeners(t *testing.T) {
	l := New(nil)
	snapshot, name, index := nativeTestUnderlay(t)
	l.SetDefaultInterface(name, index)
	if err := l.SetNetworkInterfaces(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := l.SetStablePacketIngress(true); err != nil {
		t.Fatal(err)
	}
	if err := l.SetNativeFlowIngress(true); err == nil {
		t.Fatal("two ingress modes accepted")
	}
	l.SetStablePacketIngress(false)
	l.SetNativeFlowIngress(true)
	if err := l.SetStablePacketIngress(true); err == nil {
		t.Fatal("two ingress modes accepted")
	}
	withListener := `{"inbounds":[{"type":"direct","listen":"127.0.0.1","listen_port":0}],"outbounds":[{"type":"direct","tag":"direct"}]}`
	if err := l.StartStandalone(withListener); err == nil {
		l.Stop()
		t.Fatal("native ingress opened a listener")
	}
	if err := l.StartStandalone(nativeTestConfig("192.0.2.10", false)); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if err := l.SetNativeFlowIngress(false); err == nil {
		t.Fatal("running mode changed")
	}
	if err := l.PrepareSwitch(withListener, "", "[]", "", 0); err == nil {
		t.Fatal("candidate listener accepted")
	}
	app, ingress := net.Pipe()
	defer app.Close()
	if err := l.RouteNativeTCP(ingress, NativeFlow{}, nil); err == nil {
		t.Fatal("missing original destination accepted")
	}
	app.SetDeadline(time.Now().Add(time.Second))
	if _, err := app.Write([]byte("invalid")); err == nil {
		t.Fatal("rejected connection left open")
	}
}

func TestNativeUDPFlowRoutesOriginalDestination(t *testing.T) {
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		data := make([]byte, 100)
		n, peer, err := server.ReadFrom(data)
		if err == nil {
			server.WriteTo(data[:n], peer)
		}
	}()
	l := newNativeTestRuntime(t, nativeTestConfig("192.0.2.10", false))
	dest := server.LocalAddr().(*net.UDPAddr).AddrPort()
	flow := newMemoryFlowPacketConn()
	defer flow.Close()
	flow.incoming <- memoryFlowDatagram{[]byte("native-udp"), M.SocksaddrFromNetIP(dest)}
	if err := l.RouteNativeUDP(flow, NativeFlow{Source: netip.MustParseAddrPort("127.0.0.1:41003"), Destination: dest, UserID: -1}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-flow.outgoing:
		if string(reply.payload) != "native-udp" || reply.destination != M.SocksaddrFromNetIP(dest) {
			t.Fatal("UDP payload or peer changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UDP timed out")
	}
}

func TestNativeFlowFindsActualSocketOwner(t *testing.T) {
	// The socket table is real; interception is still represented by net.Pipe.
	// No supplied process path is allowed to make this rule match.
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	original, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	peer, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := json.Marshal(executable)
	config := fmt.Sprintf(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[{"process_path":[%s],"action":"reject"}],"final":"direct"}}`, path)
	l := newNativeTestRuntime(t, config)
	app, ingress := net.Pipe()
	defer app.Close()
	rejected := make(chan error, 1)
	if err := l.RouteNativeTCP(ingress, NativeFlow{Source: original.LocalAddr().(*net.TCPAddr).AddrPort(), Destination: original.RemoteAddr().(*net.TCPAddr).AddrPort()}, func(err error) { rejected <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-rejected:
		if !R.IsRejected(err) {
			t.Fatal("actual process rule did not reject", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OS owner lookup did not match the current executable")
	}
}
