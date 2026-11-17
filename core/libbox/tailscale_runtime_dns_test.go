//go:build !windows

package libbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
)

func TestPooledTailscaleDNSOwnsHookAndProtectedResolver(t *testing.T) {
	raw := strings.Replace(tailscaleRuntimeTestConfig, `"route":{"final"`, `"route":{"auto_detect_interface":true,"final"`, 1)
	raw = strings.Replace(raw, `"dns":{"servers":[{`, `"dns":{"servers":[{"type":"tailscale","tag":"tail-dns","endpoint":"tailscale-tail","accept_default_resolvers":false},{`, 1)
	spec, err := prepareTailscaleRuntimeSpec(raw, "tail", "identity")
	if err != nil {
		t.Fatal(err)
	}
	var consumer, owner map[string]any
	json.Unmarshal([]byte(spec.consumerConfigJSON), &consumer)
	json.Unmarshal([]byte(spec.capabilityConfigJSON), &owner)
	servers := consumer["dns"].(map[string]any)["servers"].([]any)
	if servers[0].(map[string]any)["type"] != typePooledTailscaleDNS || servers[0].(map[string]any)["tag"] != "tail-dns" {
		t.Fatal("consumer owns hook or lost rule tag")
	}
	owned := owner["dns"].(map[string]any)["servers"].([]any)
	if len(owned) != 2 || owned[1].(map[string]any)["tag"] != pooledTailscaleDNSTag || owned[1].(map[string]any)["accept_default_resolvers"] != false {
		t.Fatal("owned resolver changed native semantics")
	}
	if err = validateAndroidTailscaleTransport(raw, "tail", "identity"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"auto_detect_interface":true,`, "", 1), strings.ReplaceAll(raw, `"type":"udp"`, `"type":"local"`), strings.ReplaceAll(raw, "192.0.2.53", "dns.example.invalid"), strings.ReplaceAll(raw, "xdial-system-dns", pooledTailscaleDNSTag)} {
		if err = validateAndroidTailscaleTransport(bad, "tail", "identity"); err == nil {
			t.Fatal("unsafe isolated resolver accepted")
		}
	}
}

type borrowedDNSTestOwner struct {
	adapter.DNSTransport
	closes, resets, exchanges int
}

func (t *borrowedDNSTestOwner) Close() error { t.closes++; return nil }
func (t *borrowedDNSTestOwner) Reset()       { t.resets++ }
func (t *borrowedDNSTestOwner) Exchange(_ context.Context, m *mDNS.Msg) (*mDNS.Msg, error) {
	t.exchanges++
	return m, nil
}
func (t *borrowedDNSTestOwner) ExchangeAsync(ctx context.Context, m *mDNS.Msg, cb func(*mDNS.Msg, error)) {
	r, e := t.Exchange(ctx, m)
	cb(r, e)
}
func TestPooledTailscaleDNSConsumerNeverClosesSharedOwner(t *testing.T) {
	owner := &borrowedDNSTestOwner{}
	line := &tailscaleLineRuntime{nativeDNS: owner}
	a, b := line.forEndpointTag("a"), line.forEndpointTag("b")
	if a == b || a.nativeDNS != b.nativeDNS || a.consumerEndpointTag != "a" || b.consumerEndpointTag != "b" || line.consumerEndpointTag != "" {
		t.Fatal("alias mutated shared handle")
	}
	first := &pooledTailscaleDNSTransport{owner: a.nativeDNS}
	second := &pooledTailscaleDNSTransport{owner: b.nativeDNS}
	first.Close()
	first.Reset()
	second.Exchange(context.Background(), new(mDNS.Msg))
	second.ExchangeAsync(context.Background(), new(mDNS.Msg), func(*mDNS.Msg, error) {})
	if owner.closes != 0 || owner.resets != 0 || owner.exchanges != 2 {
		t.Fatal("consumer took ownership")
	}
}

func TestPooledTailscaleDNSAcceptsGeneratedAndroidConfig(t *testing.T) {
	profile := `{"lines":[{"id":"tail","type":"tailscale","enabled":true,"tailscale_exit_node":"100.64.0.1","tailscale_magic_dns":true}],"scenarios":[{"id":"s","default_line_id":"tail"}],"active_scenario_id":"s"}`
	raw, err := GenerateAndroidConfig(profile, `["192.0.2.53"]`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAndroidTailscaleTransport(raw, "tail", "identity"); err != nil {
		t.Fatal(err)
	}
}
