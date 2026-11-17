//go:build with_gvisor && !windows

package libbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func packetTestConfig(address string, withTun bool) string {
	entry := ""
	if withTun {
		entry = `"inbounds":[{"type":"tun","tag":"tun-in","stack":"gvisor","address":["198.18.0.1/30"],"dns_mode":"hijack","mtu":1500}],`
	}
	return fmt.Sprintf(`{%s"log":{"disabled":true},"dns":{"servers":[{"type":"hosts","tag":"dns","predefined":{"generation.test":[%q]}}],"final":"dns"},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`, entry, address)
}

func startPacketTestRuntime(t *testing.T) (*Libbox, *fakeGVisorTun, *atomic.Int32) {
	t.Helper()
	l := New(nil)
	device := newFakeGVisorTun(1500)
	opened := &atomic.Int32{}
	l.SetTunOpener(func(*tun.Options, option.TunPlatformOptions) (tun.Tun, error) { opened.Add(1); return device, nil })
	l.SetNetworkInterfaces(switchNetworkInterfaces)
	l.SetDefaultInterface("test0", 1)
	if err := l.SetStablePacketIngress(true); err != nil {
		t.Fatal(err)
	}
	if err := l.StartStandalone(packetTestConfig("192.0.2.1", true)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Stop() })
	return l, device, opened
}

func packetDNS(t *testing.T, device *fakeGVisorTun, id uint16) string {
	t.Helper()
	q := new(mDNS.Msg)
	q.SetQuestion("generation.test.", mDNS.TypeA)
	q.Id = id
	payload, _ := q.Pack()
	device.injectInbound(buildUDPPacket(tcpip.AddrFrom4([4]byte{198, 18, 0, 10}), tcpip.AddrFrom4([4]byte{198, 18, 0, 2}), 40000+id, 53, payload))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		var data []byte
		select {
		case data = <-device.written:
		case <-ctx.Done():
			t.Fatal("no packet DNS response")
		}
		if len(data) < header.IPv4MinimumSize+header.UDPMinimumSize {
			continue
		}
		ip := header.IPv4(data)
		if ip.TransportProtocol() != header.UDPProtocolNumber {
			continue
		}
		var answer mDNS.Msg
		if answer.Unpack(data[int(ip.HeaderLength())+header.UDPMinimumSize:]) != nil || answer.Id != id {
			continue
		}
		if len(answer.Answer) != 1 {
			t.Fatalf("DNS answer %+v", answer)
		}
		return answer.Answer[0].(*mDNS.A).A.String()
	}
}

func TestPacketIngressSingleTUNAndAtomicDNSGeneration(t *testing.T) {
	l, device, opened := startPacketTestRuntime(t)
	if got := packetDNS(t, device, 1); got != "192.0.2.1" {
		t.Fatal(got)
	}
	source := l.box
	ingress := l.packetIngress
	if err := prepareSwitchForTest(l, packetTestConfig("192.0.2.2", false), ""); err != nil {
		t.Fatal(err)
	}
	if got := packetDNS(t, device, 2); got != "192.0.2.1" {
		t.Fatal("candidate intercepted source DNS", got)
	}
	if err := l.AbortPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if got := packetDNS(t, device, 3); got != "192.0.2.1" {
		t.Fatal(got)
	}
	if err := prepareSwitchForTest(l, packetTestConfig("192.0.2.2", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if got := packetDNS(t, device, 4); got != "192.0.2.2" {
		t.Fatal("new DNS did not adopt", got)
	}
	if l.retiredSwitch.box != source || l.packetIngress != ingress || opened.Load() != 1 || !device.ep.IsAttached() {
		t.Fatal("switch replaced or duplicated ingress")
	}
	if err := l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	if got := packetDNS(t, device, 5); got != "192.0.2.2" {
		t.Fatal("retiring source closed stable ingress", got)
	}
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	if ingress.generation() != nil || l.packetIngress != nil {
		t.Fatal("Stop retained ingress")
	}
}

func TestPacketIngressRetainsOldTCPUntilRetire(t *testing.T) {
	l, _, _ := startPacketTestRuntime(t)
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		c, e := server.Accept()
		if e == nil {
			defer c.Close()
			io.Copy(c, c)
		}
	}()
	app, conn := net.Pipe()
	defer app.Close()
	router := &packetIngressRouter{ingress: l.packetIngress}
	done := make(chan struct{})
	router.RouteConnectionEx(context.Background(), conn, adapter.InboundContext{Inbound: "tun-in", Network: N.NetworkTCP, Source: M.ParseSocksaddr("198.18.0.10:40000"), Destination: M.ParseSocksaddr(server.Addr().String())}, func(error) { close(done) })
	check := func() {
		t.Helper()
		app.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := app.Write([]byte("echo")); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 4)
		if _, err := io.ReadFull(app, reply); err != nil || string(reply) != "echo" {
			t.Fatalf("%q %v", reply, err)
		}
	}
	check()
	if err := prepareSwitchForTest(l, packetTestConfig("192.0.2.2", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	check()
	if err := l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("old TCP survived retirement")
	}
	if _, err := app.Write([]byte("late")); err == nil {
		t.Fatal("retired TCP remained writable")
	}
}

type packetTestFlowHandle struct{ closed atomic.Int32 }

func (h *packetTestFlowHandle) CloseFlow() { h.closed.Add(1) }

type packetTestFlowRouter struct {
	adapter.Router
	created atomic.Int32
}

func (r *packetTestFlowRouter) PreMatch(adapter.InboundContext, []byte) adapter.PreMatchResult {
	return adapter.PreMatchResult{Action: adapter.PreMatchFlow, NewTracker: func() tun.FlowTracker { r.created.Add(1); return nil }}
}

func TestPacketIngressRetiresNativeAndLateFlowHandles(t *testing.T) {
	router := &packetTestFlowRouter{}
	g := newPacketGeneration(context.Background(), router)
	r := &packetIngressRouter{ingress: &packetIngress{current: g}}
	result := r.PreMatch(adapter.InboundContext{Network: N.NetworkICMP}, nil)
	tracker := result.NewTracker()
	h := &packetTestFlowHandle{}
	tracker.AttachFlow(h)
	g.close()
	if h.closed.Load() != 1 {
		t.Fatal("native flow not retired")
	}
	late := result.NewTracker()
	lateHandle := &packetTestFlowHandle{}
	late.AttachFlow(lateHandle)
	if lateHandle.closed.Load() != 1 || router.created.Load() != 1 {
		t.Fatal("late flow borrowed retired router")
	}
	if r.PreMatch(adapter.InboundContext{Network: N.NetworkICMP}, nil).Action != adapter.PreMatchDrop {
		t.Fatal("retired router accepted ICMP")
	}
}

func TestPacketIngressUDPAssociationDrainsAndRetires(t *testing.T) {
	l, _, _ := startPacketTestRuntime(t)
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		data := make([]byte, 2048)
		for {
			n, remote, e := server.ReadFrom(data)
			if e != nil {
				return
			}
			server.WriteTo(data[:n], remote)
		}
	}()
	destination := M.ParseSocksaddr(server.LocalAddr().String())
	flow := newMemoryFlowPacketConn()
	r := &packetIngressRouter{ingress: l.packetIngress}
	done := make(chan struct{})
	r.RoutePacketConnectionEx(context.Background(), flow, adapter.InboundContext{Inbound: "tun-in", Network: N.NetworkUDP, Source: M.ParseSocksaddr("198.18.0.10:40000"), Destination: destination}, func(error) { close(done) })
	check := func() {
		t.Helper()
		flow.incoming <- memoryFlowDatagram{payload: []byte("echo"), destination: destination}
		select {
		case reply := <-flow.outgoing:
			if string(reply.payload) != "echo" {
				t.Fatal("bad UDP reply")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("UDP timeout")
		}
	}
	check()
	if err = prepareSwitchForTest(l, packetTestConfig("192.0.2.2", false), ""); err != nil {
		t.Fatal(err)
	}
	if err = l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	check()
	if err = l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("old UDP not retired")
	}
	select {
	case <-flow.closed:
	default:
		t.Fatal("UDP reader leaked")
	}
}

func TestPacketIngressLateDNSWriteCannotEscapeRetirement(t *testing.T) {
	g := newPacketGeneration(context.Background(), nil)
	flow := newMemoryFlowPacketConn()
	writer := &packetGenerationWriter{generation: g, writer: flow}
	g.close()
	if err := writer.WritePacket(buf.As([]byte("late")), M.ParseSocksaddr("198.18.0.10:40000")); err != net.ErrClosed {
		t.Fatalf("late write %v", err)
	}
	select {
	case <-flow.outgoing:
		t.Fatal("retired response emitted")
	default:
	}
}

func TestPacketIngressNativeAttachRacesRetirement(t *testing.T) {
	for i := 0; i < 200; i++ {
		g := newPacketGeneration(context.Background(), &packetTestFlowRouter{})
		tracker := newPacketFlowTracker(g, nil)
		handle := &packetTestFlowHandle{}
		done := make(chan struct{})
		go func() {
			tracker.AttachFlow(handle)
			tracker.CountForward(1)
			tracker.CountReverse(1)
			tracker.FlowEstablished()
			close(done)
		}()
		g.close()
		<-done
		if handle.closed.Load() != 1 {
			t.Fatal("native handle not closed exactly once", handle.closed.Load())
		}
	}
}
