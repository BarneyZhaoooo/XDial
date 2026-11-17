//go:build (windows || linux) && !android

package libbox

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// NativeFlow is trusted OS adapter input, never a client-supplied IPC claim.
// It contains observed facts only; outbound, DNS and routing decisions are
// deliberately absent. Unknown process identity leaves sing-box's finder active.
type NativeFlow struct {
	Source      netip.AddrPort
	Destination netip.AddrPort
	ProcessID   uint32
	ProcessPath string
	UserID      int32
	UserIDKnown bool
}

func (f NativeFlow) metadata(network string) (adapter.InboundContext, error) {
	if !f.Source.IsValid() || !f.Destination.IsValid() || f.Source.Port() == 0 || f.Destination.Port() == 0 || f.Destination.Addr().IsUnspecified() {
		return adapter.InboundContext{}, fmt.Errorf("native flow requires original source and destination endpoints")
	}
	m := adapter.InboundContext{Inbound: "desktop-native-flow", InboundType: "transparent", Network: network,
		Source: M.SocksaddrFromNetIP(f.Source), Destination: M.SocksaddrFromNetIP(f.Destination)}
	if f.ProcessID != 0 || f.ProcessPath != "" {
		uid := int32(-1)
		if f.UserIDKnown {
			if f.UserID < 0 {
				return adapter.InboundContext{}, fmt.Errorf("invalid observed user ID")
			}
			uid = f.UserID
		}
		m.ProcessInfo = &adapter.ConnectionOwner{ProcessID: f.ProcessID, ProcessPath: f.ProcessPath, UserId: uid}
	}
	return m, nil
}

// SetNativeFlowIngress opts an idle desktop runtime into the same generation
// switch and retirement contract used by the stable mobile ingress. The OS
// adapter owns interception and supplies already accepted TCP/UDP flows.
func (l *Libbox) SetNativeFlowIngress(enabled bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running || l.startPreparing || l.cleaning {
		return fmt.Errorf("native flow mode requires an idle runtime")
	}
	if enabled && l.stablePacketIngress {
		return fmt.Errorf("packet ingress is already selected")
	}
	if !enabled && l.nativeFlowConfigValidator != nil {
		return fmt.Errorf("configured native adapter requires native flow ingress")
	}
	l.nativeFlowIngress = enabled
	return nil
}

func (l *Libbox) nativeFlowRouter() (*packetIngressRouter, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.nativeFlowIngress || !l.running || l.cleaning || l.packetIngress == nil {
		return nil, fmt.Errorf("native flow runtime is not running")
	}
	return &packetIngressRouter{ingress: l.packetIngress}, nil
}

// RouteNativeTCP transfers connection ownership on both success and failure.
// Once admitted, a flow retains its generation until it ends or is retired.
// Dispatch on a per-flow worker: sniffing and DNS sessions can block until input
// arrives or the flow closes. Never call it while holding an OS callback lock.
func (l *Libbox) RouteNativeTCP(conn net.Conn, flow NativeFlow, onClose N.CloseHandlerFunc) error {
	metadata, err := flow.metadata(N.NetworkTCP)
	var router *packetIngressRouter
	if err == nil {
		router, err = l.nativeFlowRouter()
	}
	if err != nil {
		conn.Close()
		if onClose != nil {
			onClose(err)
		}
		return err
	}
	router.RouteConnectionEx(context.Background(), conn, metadata, onClose)
	return nil
}

func (l *Libbox) RouteNativeUDP(conn N.PacketConn, flow NativeFlow, onClose N.CloseHandlerFunc) error {
	metadata, err := flow.metadata(N.NetworkUDP)
	var router *packetIngressRouter
	if err == nil {
		router, err = l.nativeFlowRouter()
	}
	if err != nil {
		conn.Close()
		if onClose != nil {
			onClose(err)
		}
		return err
	}
	router.RoutePacketConnectionEx(context.Background(), conn, metadata, onClose)
	return nil
}
