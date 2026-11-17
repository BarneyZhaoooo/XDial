package libbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

const typePooledTailscaleDNS = "xdial-tailscale-runtime-dns"
const pooledTailscaleDNSTag = "xdial-owned-tailnet-dns"

// The native endpoint supports one DNS reconfiguration hook. Its capability
// Box owns that hook and resolver; consumer generations borrow the transport,
// rather than installing competing hooks or copying private DNS snapshots.
func moveTailscaleRuntimeDNS(document map[string]json.RawMessage, capability map[string]any, endpointTag string) error {
	var consumer map[string]json.RawMessage
	if raw := document["dns"]; len(raw) == 0 {
		return nil
	} else if err := json.Unmarshal(raw, &consumer); err != nil {
		return err
	}
	var servers []json.RawMessage
	if err := json.Unmarshal(consumer["servers"], &servers); err != nil {
		return err
	}
	encoded, _ := json.Marshal(capability["servers"])
	var owned []json.RawMessage
	if err := json.Unmarshal(encoded, &owned); err != nil {
		return err
	}
	for _, raw := range owned {
		var identity struct{ Tag string }
		if err := json.Unmarshal(raw, &identity); err != nil {
			return err
		}
		if identity.Tag == pooledTailscaleDNSTag {
			return fmt.Errorf("reserved Tailscale DNS identity")
		}
	}
	found := false
	for i, raw := range servers {
		var server struct{ Type, Tag, Endpoint string }
		if err := json.Unmarshal(raw, &server); err != nil {
			return err
		}
		if server.Type != "tailscale" {
			continue
		}
		if found || server.Endpoint != endpointTag || server.Tag == "" {
			return fmt.Errorf("ambiguous Tailscale DNS capability")
		}
		found = true
		var native map[string]json.RawMessage
		json.Unmarshal(raw, &native)
		native["tag"], _ = json.Marshal(pooledTailscaleDNSTag)
		ownedServer, _ := json.Marshal(native)
		owned = append(owned, ownedServer)
		servers[i], _ = json.Marshal(map[string]string{"type": typePooledTailscaleDNS, "tag": server.Tag})
	}
	if !found {
		return nil
	}
	consumer["servers"], _ = json.Marshal(servers)
	document["dns"], _ = json.Marshal(consumer)
	capability["servers"] = owned
	return nil
}

type pooledTailscaleDNSTransport struct {
	dns.TransportAdapter
	owner adapter.DNSTransport
}

func registerPooledTailscaleDNS(registry *dns.TransportRegistry) {
	dns.RegisterTransport[option.StubOptions](registry, typePooledTailscaleDNS, func(ctx context.Context, _ log.ContextLogger, tag string, _ option.StubOptions) (adapter.DNSTransport, error) {
		line := service.FromContext[*tailscaleLineRuntime](ctx)
		if line == nil || line.nativeDNS == nil {
			return nil, fmt.Errorf("owned Tailscale DNS unavailable")
		}
		return &pooledTailscaleDNSTransport{dns.NewTransportAdapter(typePooledTailscaleDNS, tag, nil), line.nativeDNS}, nil
	})
}
func (t *pooledTailscaleDNSTransport) Start(adapter.StartStage) error { return nil }
func (t *pooledTailscaleDNSTransport) Close() error                   { return nil }
func (t *pooledTailscaleDNSTransport) Reset()                         {}
func (t *pooledTailscaleDNSTransport) Exchange(ctx context.Context, message *mDNS.Msg) (*mDNS.Msg, error) {
	return t.owner.Exchange(ctx, message)
}
func (t *pooledTailscaleDNSTransport) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(*mDNS.Msg, error)) {
	t.owner.ExchangeAsync(ctx, message, callback)
}

func (l *tailscaleLineRuntime) forEndpointTag(tag string) *tailscaleLineRuntime {
	if l == nil {
		return nil
	}
	copy := *l
	copy.consumerEndpointTag = tag
	return &copy
}

// Only Get is adapted. Lifecycle and creation remain with the consumer's own
// manager; the borrowed endpoint is never inserted into its ownership list.
type mobileBorrowedEndpointManager struct {
	adapter.EndpointManager
	tag      string
	endpoint adapter.Endpoint
}

func (m *mobileBorrowedEndpointManager) Get(tag string) (adapter.Endpoint, bool) {
	if tag == m.tag {
		return m.endpoint, true
	}
	return m.EndpointManager.Get(tag)
}
func mobileEndpointManager(ctx context.Context) adapter.EndpointManager {
	manager := service.FromContext[adapter.EndpointManager](ctx)
	line := service.FromContext[*tailscaleLineRuntime](ctx)
	if line != nil && line.consumerEndpointTag != "" {
		if endpoint, ok := line.outbound.(adapter.Endpoint); ok {
			return &mobileBorrowedEndpointManager{manager, line.consumerEndpointTag, endpoint}
		}
	}
	return manager
}

// Android must never enter the desktop compatibility fallback to local DNS.
// Inspect the isolated capability before opening any protocol socket.
func validateAndroidTailscaleTransport(configJSON, lineID, identity string) error {
	spec, err := prepareTailscaleRuntimeSpec(configJSON, lineID, identity)
	if err != nil {
		return err
	}
	var doc struct {
		DNS struct {
			Servers []struct{ Type, Server, Detour string }
		}
		Route struct {
			Auto bool `json:"auto_detect_interface"`
		}
	}
	if err = json.Unmarshal([]byte(spec.capabilityConfigJSON), &doc); err != nil {
		return err
	}
	if !doc.Route.Auto {
		return fmt.Errorf("Android Tailscale requires protected transport")
	}
	for _, server := range doc.DNS.Servers {
		if server.Type == "tailscale" {
			continue
		}
		if server.Type != "udp" || server.Detour != "" {
			return fmt.Errorf("Android Tailscale requires captured system DNS")
		}
		if _, err = netip.ParseAddr(server.Server); err != nil {
			return fmt.Errorf("Android Tailscale requires numerical system DNS")
		}
	}
	return nil
}
