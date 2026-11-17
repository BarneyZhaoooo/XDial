package subscription

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// TLS stays verified. Only the injected test dialer maps the validated numeric
// fixture destination back to a local server; no external request is sent.
func TestProfileSourceHTTPBoundsRedirectsAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			io.WriteString(w, `{"outbounds":[{"type":"direct","tag":"direct"}]}`)
		case "/redirect":
			http.Redirect(w, r, "https://example.com/ok", http.StatusFound)
		case "/private":
			http.Redirect(w, r, "https://127.0.0.1/hidden", http.StatusFound)
		case "/credentials":
			http.Redirect(w, r, "https://user:pass@example.com/ok", http.StatusFound)
		case "/large":
			io.WriteString(w, strings.Repeat("x", 4097))
		case "/stall":
			entered <- struct{}{}
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	certs := x509.NewCertPool()
	certs.AddCert(server.Certificate())
	old := http.DefaultTransport
	transport := old.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: certs}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = old; transport.CloseIdleConnections() }()
	calls := 0
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		if host != "example.com" {
			t.Errorf("unexpected lookup %s", host)
		}
		return []netip.Addr{netip.MustParseAddr("203.0.113.4")}, nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		calls++
		if address != "203.0.113.4:443" {
			t.Errorf("unvalidated destination %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	for _, path := range []string{"/ok", "/redirect"} {
		body, err := FetchProfileSourceBytesWithNetworkContext(context.Background(), "https://example.com"+path, 4096, lookup, dial)
		if err != nil || !strings.Contains(string(body), "outbounds") {
			t.Fatal(path, err)
		}
	}
	for _, path := range []string{"/private", "/credentials", "/large", "/failure"} {
		if _, err := FetchProfileSourceBytesWithNetworkContext(context.Background(), "https://example.com"+path, 4096, lookup, dial); err == nil {
			t.Fatal("accepted " + path)
		}
	}
	if calls != 6 {
		t.Fatalf("unexpected dial count %d; rejected redirect may have dialed", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := FetchProfileSourceBytesWithNetworkContext(ctx, "https://example.com/stall", 4096, lookup, dial)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no request")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled body accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not interrupt response")
	}
}
func TestProfileSourceURLValidationBeforeNetwork(t *testing.T) {
	for _, u := range []string{"http://example.com", "https://127.0.0.1", "https://user:password@example.com", "https://example.com/#token", "https://example.com/" + strings.Repeat("a", 4096)} {
		if ValidateProfileSourceURL(u) == nil {
			t.Fatal("accepted invalid source")
		}
	}
	if err := ValidateProfileSourceURL("https://example.com/?token=fixture"); err != nil {
		t.Fatal(err)
	}
}
