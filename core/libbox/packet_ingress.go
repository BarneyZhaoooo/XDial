package libbox

import (
	"context"
	"fmt"
	"net"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	boxTun "github.com/sagernet/sing-box/protocol/tun"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	jsonext "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// packetIngress owns the only packet reader. Traffic Boxes own DNS, route and
// protocol decisions; replacing one never replaces this ingress's gVisor stack.
type packetIngress struct {
	template []byte
	disabled bool
	mu       sync.Mutex
	current  *packetGeneration
	box      *box.Box
	cancel   context.CancelFunc
}

type packetGeneration struct {
	ctx       context.Context
	cancel    context.CancelFunc
	router    adapter.Router
	mu        sync.Mutex
	closed    bool
	closeDone chan struct{}
	next      uint64
	closers   map[uint64]func()
	calls     sync.WaitGroup
}

func newPacketGeneration(ctx context.Context, router adapter.Router) *packetGeneration {
	ctx, cancel := context.WithCancel(ctx)
	return &packetGeneration{ctx: ctx, cancel: cancel, router: router, closers: make(map[uint64]func()), closeDone: make(chan struct{})}
}

func (g *packetGeneration) enter() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.calls.Add(1)
	return true
}

func (g *packetGeneration) track(close func()) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, false
	}
	g.next++
	id := g.next
	g.closers[id] = close
	return func() { g.mu.Lock(); delete(g.closers, id); g.mu.Unlock() }, true
}

func (g *packetGeneration) close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.closed {
		done := g.closeDone
		g.mu.Unlock()
		<-done
		return
	}
	g.closed = true
	closers := g.closers
	g.closers = make(map[uint64]func())
	g.mu.Unlock()
	g.cancel()
	for _, close := range closers {
		close()
	}
	g.calls.Wait()
	close(g.closeDone)
}

func (p *packetIngress) generation() *packetGeneration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return nil
	}
	return p.current
}
func (p *packetIngress) swap(g *packetGeneration) *packetGeneration {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.current
	p.current = g
	return old
}
func (p *packetIngress) close() {
	if p == nil {
		return
	}
	p.swap(nil).close()
	p.cancel()
	if p.box != nil {
		p.box.Close()
	}
}

// Non-traffic methods refer only to the ingress shell's managers. This shell
// never contains user route rules, protocol identities, or a network resolver.
type packetIngressRouter struct {
	adapter.Router
	ingress *packetIngress
}

func (r *packetIngressRouter) PreMatch(metadata adapter.InboundContext, first []byte) adapter.PreMatchResult {
	// Admit TCP/UDP to the complete sing-box flow router. An early decision from
	// a different generation must not survive until NewConnectionEx. ICMP has
	// no net.Conn path and retains sing-box's native Port/FlowTracker handling.
	if metadata.Network != N.NetworkICMP {
		return adapter.PreMatchResult{Action: adapter.PreMatchContinue}
	}
	g := r.ingress.generation()
	if !g.enter() {
		return adapter.PreMatchResult{Action: adapter.PreMatchDrop}
	}
	defer g.calls.Done()
	result := g.router.PreMatch(metadata, first)
	if result.Action == adapter.PreMatchFlow {
		original := result.NewTracker
		result.NewTracker = func() tun.FlowTracker { return newPacketFlowTracker(g, original) }
	}
	return result
}

func (r *packetIngressRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	g := r.ingress.generation()
	if !g.enter() {
		conn.Close()
		if onClose != nil {
			onClose(net.ErrClosed)
		}
		return
	}
	defer g.calls.Done()
	remove, ok := g.track(func() { conn.Close() })
	if !ok {
		conn.Close()
		if onClose != nil {
			onClose(net.ErrClosed)
		}
		return
	}
	g.router.RouteConnectionEx(g.ctx, conn, metadata, func(err error) {
		remove()
		if onClose != nil {
			onClose(err)
		}
	})
}

func (r *packetIngressRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	g := r.ingress.generation()
	if !g.enter() {
		conn.Close()
		if onClose != nil {
			onClose(net.ErrClosed)
		}
		return
	}
	defer g.calls.Done()
	remove, ok := g.track(func() { conn.Close() })
	if !ok {
		conn.Close()
		if onClose != nil {
			onClose(net.ErrClosed)
		}
		return
	}
	g.router.RoutePacketConnectionEx(g.ctx, conn, metadata, func(err error) {
		remove()
		if onClose != nil {
			onClose(err)
		}
	})
}

func (r *packetIngressRouter) RouteConnection(context.Context, net.Conn, adapter.InboundContext) error {
	return fmt.Errorf("packet ingress requires RouteConnectionEx")
}
func (r *packetIngressRouter) RoutePacketConnection(context.Context, N.PacketConn, adapter.InboundContext) error {
	return fmt.Errorf("packet ingress requires RoutePacketConnectionEx")
}

func (r *packetIngressRouter) HijackDNSPacket(_ context.Context, payload []byte, writer N.PacketWriter, metadata adapter.InboundContext) {
	g := r.ingress.generation()
	if !g.enter() {
		return
	}
	defer g.calls.Done()
	g.router.HijackDNSPacket(g.ctx, payload, &packetGenerationWriter{g, writer}, metadata)
}

type packetGenerationWriter struct {
	generation *packetGeneration
	writer     N.PacketWriter
}

func (w *packetGenerationWriter) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	if !w.generation.enter() {
		buffer.Release()
		return net.ErrClosed
	}
	defer w.generation.calls.Done()
	return w.writer.WritePacket(buffer, destination)
}

type packetFlowTracker struct {
	mu       sync.Mutex
	closed   bool
	handle   tun.FlowHandle
	delegate tun.FlowTracker
	remove   func()
}

func newPacketFlowTracker(g *packetGeneration, factory func() tun.FlowTracker) *packetFlowTracker {
	t := &packetFlowTracker{}
	if !g.enter() {
		t.closed = true
		return t
	}
	defer g.calls.Done()
	if factory != nil {
		t.delegate = factory()
	}
	remove, ok := g.track(t.retire)
	t.mu.Lock()
	t.remove = remove
	if !ok {
		t.closed = true
	}
	t.mu.Unlock()
	return t
}
func (t *packetFlowTracker) retire() {
	t.CloseFlow(tun.FlowCloseReset)
	t.mu.Lock()
	h := t.handle
	t.mu.Unlock()
	if h != nil {
		h.CloseFlow()
	}
}
func (t *packetFlowTracker) AttachFlow(h tun.FlowHandle) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		h.CloseFlow()
		return
	}
	t.handle = h
	if t.delegate != nil {
		t.delegate.AttachFlow(h)
	}
	t.mu.Unlock()
}
func (t *packetFlowTracker) CountForward(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed && t.delegate != nil {
		t.delegate.CountForward(n)
	}
}
func (t *packetFlowTracker) CountReverse(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed && t.delegate != nil {
		t.delegate.CountReverse(n)
	}
}
func (t *packetFlowTracker) FlowEstablished() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed && t.delegate != nil {
		t.delegate.FlowEstablished()
	}
}
func (t *packetFlowTracker) CloseFlow(reason tun.FlowCloseReason) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	if t.handle != nil && t.delegate != nil {
		t.delegate.CloseFlow(reason)
	}
	remove := t.remove
	t.mu.Unlock()
	if remove != nil {
		remove()
	}
}

func newPacketIngress(options option.Inbound, platform *xdPlatformInterface, generation *packetGeneration) (*packetIngress, error) {
	ctx, cancel := context.WithCancel(context.Background())
	template, err := jsonext.Marshal(options)
	if err != nil {
		cancel()
		generation.close()
		return nil, err
	}
	p := &packetIngress{current: generation, cancel: cancel, template: template}
	registry := inboundRegistry()
	inbound.Register[option.TunInboundOptions](registry, "tun", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, opts option.TunInboundOptions) (adapter.Inbound, error) {
		if opts.AutoRoute || opts.AutoRedirect || len(opts.RouteAddressSet) != 0 || len(opts.RouteExcludeAddressSet) != 0 || opts.Stack != "gvisor" {
			return nil, fmt.Errorf("stable packet ingress requires platform-owned gVisor TUN")
		}
		return boxTun.NewInbound(ctx, &packetIngressRouter{router, p}, logger, tag, opts)
	})
	ctx = boxContext(ctx)
	ctx = service.ContextWith[option.InboundOptionsRegistry](ctx, registry)
	ctx = service.ContextWith[adapter.InboundRegistry](ctx, registry)
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, platform.lineRuntimeCapability())
	// No network DNS transport and no user-policy fallbacks in the ingress Box.
	config, err := jsonext.UnmarshalExtendedContext[option.Options](ctx, []byte(`{"log":{"disabled":true},"dns":{"servers":[{"type":"hosts","tag":"ingress-unused"}],"final":"ingress-unused"},"outbounds":[{"type":"direct","tag":"ingress-unused"}],"route":{"final":"ingress-unused"}}`))
	if err == nil {
		config.Inbounds = []option.Inbound{options}
		p.box, err = box.New(box.Options{Options: config, Context: ctx})
	}
	if err == nil {
		err = p.box.Start()
	}
	if err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

func (p *packetIngress) disable() { p.mu.Lock(); p.disabled = true; p.mu.Unlock() }

// SetStablePacketIngress opts the Android host into independently owned packet
// ingress. Apple entrypoints retain their existing packet/flow lifecycle.
func (l *Libbox) SetStablePacketIngress(enabled bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running || l.startPreparing || l.cleaning {
		return fmt.Errorf("packet ingress mode requires an idle runtime")
	}
	if enabled && l.nativeFlowIngress {
		return fmt.Errorf("native flow ingress is already selected")
	}
	l.stablePacketIngress = enabled
	return nil
}
