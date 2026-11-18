//go:build !windows

package libbox

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/hosts"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	socksclient "github.com/sagernet/sing/protocol/socks"
)

// The incident this guards: on a home network in China the system resolver
// answered chatgpt.com with a forged address because the query never reached
// XDial. The application then connected by that address and presented the
// real name in TLS. The name matched a binding to the company Line, but the
// flow still went to the forged address through that Line and the handshake
// was cut. The Line that owns the name must connect to its own answer.
//
// Everything here is the real sing-box data path: SOCKS ingress, sniffing,
// route resolve, a proxy Line implemented as a SOCKS hop (the shape the
// AnyConnect bridge has under Transparent Proxy), and a TLS handshake that
// only succeeds at the name's real endpoint.
func TestFlowAddressCorrectionConnectsThroughLineAnswer(t *testing.T) {
	server := startFlowCorrectionTLSServer(t, "line.example")
	// TEST-NET-3 stands in for the forged public answer the application got.
	forged := M.ParseSocksaddrHostPort("203.0.113.7", server.port)

	t.Run("without correction the forged address is used", func(t *testing.T) {
		ingress := startFlowCorrectionBox(t, false)
		err := flowCorrectionHandshake(ingress, forged, "line.example", server.roots)
		if err == nil {
			t.Fatal("a forged address must not reach the name's endpoint without correction")
		}
	})

	t.Run("the matched Line's answer replaces the forged address", func(t *testing.T) {
		ingress := startFlowCorrectionBox(t, true)
		if err := flowCorrectionHandshake(ingress, forged, "line.example", server.roots); err != nil {
			t.Fatalf("corrected flow did not reach the name's endpoint: %v", err)
		}
	})
}

func startFlowCorrectionBox(t *testing.T, correct bool) M.Socksaddr {
	t.Helper()
	ingressPort := freeFlowCorrectionPort(t)
	linePort := freeFlowCorrectionPort(t)
	config := fmt.Sprintf(`{
		"log": {"disabled": true},
		"dns": {
			"servers": [{
				"type": "hosts",
				"tag": "line-dns",
				"predefined": {"line.example": ["127.0.0.1"]}
			}],
			"final": "line-dns"
		},
		"inbounds": [
			{"type": "socks", "tag": "transparent-proxy-in", "listen": "127.0.0.1", "listen_port": %d},
			{"type": "socks", "tag": "line-server-in", "listen": "127.0.0.1", "listen_port": %d}
		],
		"outbounds": [
			{"type": "direct", "tag": "direct"},
			{"type": "socks", "tag": "proxy-line", "server": "127.0.0.1", "server_port": %d}
		],
		"route": {
			"rules": [
				{"inbound": ["line-server-in"], "outbound": "direct"},
				{"action": "sniff"},
				{"domain_suffix": ["line.example"], "action": "resolve", "server": "line-dns", "xdial_correct_flow_address": %t},
				{"domain_suffix": ["line.example"], "outbound": "proxy-line"}
			],
			"final": "direct"
		}
	}`, ingressPort, linePort, linePort, correct)

	ctx := flowCorrectionBoxContext()
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(config))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	instance, err := box.New(box.Options{Options: options, Context: ctx})
	if err != nil {
		t.Fatalf("construct sing-box: %v", err)
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		t.Fatalf("start sing-box: %v", err)
	}
	t.Cleanup(func() { instance.Close() })
	return M.ParseSocksaddrHostPort("127.0.0.1", uint16(ingressPort))
}

func flowCorrectionBoxContext() context.Context {
	inbounds := inbound.NewRegistry()
	socks.RegisterInbound(inbounds)
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	socks.RegisterOutbound(outbounds)
	transports := dns.NewTransportRegistry()
	hosts.RegisterTransport(transports)
	return box.Context(
		context.Background(),
		inbounds,
		outbounds,
		endpoint.NewRegistry(),
		transports,
		boxservice.NewRegistry(),
		certificate.NewRegistry(),
	)
}

// flowCorrectionHandshake connects by address, exactly like an application
// that resolved the name itself, and requires a verified TLS session for the
// name presented in the ClientHello.
func flowCorrectionHandshake(
	ingress M.Socksaddr,
	destination M.Socksaddr,
	serverName string,
	roots *x509.CertPool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := socksclient.NewClient(N.SystemDialer, ingress, socksclient.Version5, "", "")
	conn, err := client.DialContext(ctx, N.NetworkTCP, destination)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	session := tls.Client(conn, &tls.Config{ServerName: serverName, RootCAs: roots})
	if err := session.HandshakeContext(ctx); err != nil {
		return err
	}
	reply, err := io.ReadAll(session)
	if err != nil {
		return err
	}
	if string(reply) != "line-endpoint" {
		return fmt.Errorf("unexpected endpoint reply %q", reply)
	}
	return nil
}

type flowCorrectionTLSServer struct {
	port  uint16
	roots *x509.CertPool
}

func startFlowCorrectionTLSServer(t *testing.T, name string) flowCorrectionTLSServer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = conn.Write([]byte("line-endpoint"))
			}()
		}
	}()
	return flowCorrectionTLSServer{
		port:  uint16(listener.Addr().(*net.TCPAddr).Port),
		roots: roots,
	}
}

func freeFlowCorrectionPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
