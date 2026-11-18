//go:build !windows

package libbox

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

func TestCountryCodeParsers(t *testing.T) {
	for body, want := range map[string]string{
		"CN":     "CN",
		"hk\r\n": "HK",
		" NZ \n": "NZ",
	} {
		if got, err := parseAppleCountryCode([]byte(body)); err != nil || got != want {
			t.Errorf("apple %q = %q, %v; want %q", body, got, err, want)
		}
	}
	for _, body := range []string{"", "CHN", "C1", "<html>", "XX"} {
		if got, err := parseAppleCountryCode([]byte(body)); err == nil {
			t.Errorf("apple %q accepted as %q", body, got)
		}
	}
	trace := "fl=1\nip=203.0.113.9\nts=1\nloc=jp\ncolo=NRT\n"
	if got, err := parseCloudflareCountryCode([]byte(trace)); err != nil || got != "JP" {
		t.Errorf("cloudflare = %q, %v", got, err)
	}
	for _, body := range []string{"ip=203.0.113.9\n", "loc=XX\n", "loc=T1\n", "loc=\n"} {
		if got, err := parseCloudflareCountryCode([]byte(body)); err == nil {
			t.Errorf("cloudflare %q accepted as %q", body, got)
		}
	}
}

func TestAppleCountryEndpointUsesTheLineResolver(t *testing.T) {
	apple := outboundCountryProbeEndpoints[0]
	if apple.destination.Fqdn != "gspe1-ssl.ls.apple.com" || !apple.lineResolved {
		t.Fatalf("Apple endpoint must resolve through the probed Line: %+v", apple)
	}
	cloudflare := outboundCountryProbeEndpoints[1]
	if !cloudflare.destination.IsIP() || cloudflare.lineResolved {
		t.Fatalf("Cloudflare endpoint must stay numeric: %+v", cloudflare)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "CN")
	}))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	// The Network Extension config has no native resolver; Direct resolves its
	// own hostnames with the route default resolver there.
	transport := newFakeMobileDNSTransport(config.MobilePublicDNSTag)
	ctx := service.ContextWith[adapter.DNSRouter](context.Background(),
		&addressTestDNSRouter{transport: transport, domain: "gspe1-ssl.ls.apple.com"})
	ctx = service.ContextWith[adapter.DNSTransportManager](ctx, &addressTestDNSManager{transport: transport})
	ctx = service.ContextWith[adapter.CertificateStore](ctx, &outboundTLSProbeTestCertificateStore{pool: pool})
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	outbound := &addressTestOutbound{tag: "direct", target: server.Listener.Addr().String()}
	endpoint := outboundAddressProbeEndpoint{
		url:          server.URL,
		destination:  M.ParseSocksaddrHostPortStr("gspe1-ssl.ls.apple.com", "443"),
		lineResolved: true,
	}
	body, err := fetchOutboundProbe(ctx, outbound, endpoint, 1)
	if err != nil || !outbound.dialed {
		t.Fatalf("fetch=%q %v dialed=%v", body, err, outbound.dialed)
	}
	if code, err := apple.parse(body); err != nil || code != "CN" {
		t.Fatalf("parse=%q %v", code, err)
	}
}

func TestDirectLineResolverPrefersNativeThenNetworkExtension(t *testing.T) {
	native := newFakeMobileDNSTransport(config.TransparentNativeDNSTag)
	mobile := newFakeMobileDNSTransport(config.MobilePublicDNSTag)
	both := &directResolverTestManager{transports: map[string]adapter.DNSTransport{
		native.Tag(): native, mobile.Tag(): mobile,
	}}
	if transport, loaded := directLineResolver(both); !loaded || transport != native {
		t.Fatalf("transparent proxy must use the native resolver, got %v", transport)
	}
	onlyMobile := &directResolverTestManager{transports: map[string]adapter.DNSTransport{mobile.Tag(): mobile}}
	if transport, loaded := directLineResolver(onlyMobile); !loaded || transport != mobile {
		t.Fatalf("Network Extension must use its route default resolver, got %v", transport)
	}
	proxyOnly := &directResolverTestManager{transports: map[string]adapter.DNSTransport{
		"proxy-dns-node-a": newFakeMobileDNSTransport("proxy-dns-node-a"),
	}}
	if _, loaded := directLineResolver(proxyOnly); loaded {
		t.Fatal("Direct must never borrow another Line's resolver")
	}
}

type directResolverTestManager struct {
	adapter.DNSTransportManager
	transports map[string]adapter.DNSTransport
}

func (m *directResolverTestManager) Transport(tag string) (adapter.DNSTransport, bool) {
	transport, loaded := m.transports[tag]
	return transport, loaded
}

func TestProbeOutboundCountryRequiresARunningConnection(t *testing.T) {
	instance := &Libbox{}
	if _, err := instance.ProbeOutboundCountry("direct", 5_000); err == nil {
		t.Fatal("probe ran without a running connection")
	}
}

func TestProbeOutboundCountryEvidenceIsReturnedOnFailure(t *testing.T) {
	instance := &Libbox{}
	raw, err := instance.ProbeOutboundCountryEvidence("direct", 5_000)
	if err == nil {
		t.Fatal("probe ran without a running connection")
	}
	var evidence outboundCountryEvidence
	if jsonErr := json.Unmarshal([]byte(raw), &evidence); jsonErr != nil {
		t.Fatalf("evidence is not JSON: %q", raw)
	}
	if evidence.Code != "" || evidence.Endpoint != "" {
		t.Fatalf("failed probe reported a region: %+v", evidence)
	}
	for _, label := range []string{outboundCountryProbeEndpoints[0].label, outboundCountryProbeEndpoints[1].label} {
		if label == "" {
			t.Fatal("country endpoint has no diagnostic label")
		}
	}
}
