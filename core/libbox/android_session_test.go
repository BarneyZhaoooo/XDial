//go:build !windows

package libbox

import (
	"strings"
	"testing"
)

func TestAndroidServerLiteralDoesNotUseHostResolver(t *testing.T) {
	l := New(nil)
	if ip, err := l.resolveAndroidServer("https://192.0.2.10:443", "invalid"); err != nil || ip != "192.0.2.10" {
		t.Fatalf("%q %v", ip, err)
	}
	if _, err := l.resolveAndroidServer("https://[2001:db8::1]", "[]"); err == nil {
		t.Fatal("sslcon IPv4 constraint bypassed")
	}
}
func TestAndroidBootstrapDNSIsProtectedAndSanitized(t *testing.T) {
	l := New(nil)
	p := &testSocketProtector{err: errMobileDNSFailed}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	_, err := l.resolveAndroidServer("private-vpn.example.invalid", `["192.0.2.53"]`)
	if err == nil || strings.Contains(err.Error(), "private-vpn") || strings.Contains(err.Error(), "192.0.2.53") {
		t.Fatalf("unsanitized error %v", err)
	}
	if p.fd == 0 {
		t.Fatal("bootstrap DNS did not use platform socket control")
	}
}
