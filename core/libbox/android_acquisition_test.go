//go:build !windows

package libbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
)

type acquisitionCounter struct {
	calls  atomic.Int32
	refuse bool
}

func (p *acquisitionCounter) ProtectSocket(int) error {
	p.calls.Add(1)
	if p.refuse {
		return fmt.Errorf("refused")
	}
	return nil
}

// This local Shadowsocks-none fixture only refuses requests. No DNS or HTTPS packets leave
// the test machine; it proves the core asks the selected proxy to resolve DNS.
func TestAndroidAcquisitionDNSUsesProtectedProxy(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	targets := make(chan string, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				request := make([]byte, 1)
				if _, err := io.ReadFull(conn, request); err != nil {
					return
				}
				if request[0] != 1 {
					return
				}
				address := make([]byte, 6)
				if _, err := io.ReadFull(conn, address); err != nil {
					return
				}
				target := fmt.Sprintf("%s:%d", net.IP(address[:4]), binary.BigEndian.Uint16(address[4:]))
				select {
				case targets <- target:
				default:
				}
			}()
		}
	}()
	profile := &config.Profile{Lines: []config.Line{{ID: "source", Type: config.LineTypeShadowsocks, Enabled: true, SSServer: "127.0.0.1", SSPort: listener.Addr().(*net.TCPAddr).Port, SSMethod: "none", SSPass: "fixture"}}}
	l := New(nil)
	protector := &acquisitionCounter{}
	l.SetSocketProtector(protector)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resource, err := l.newAndroidAcquisition(ctx, profile, "source", `["192.0.2.53"]`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer resource.instance.Close()
	if _, err = resource.fetch("https://rules.example.invalid/r.json", 1024); err == nil {
		t.Fatal("refused proxy succeeded")
	}
	select {
	case target := <-targets:
		if target != "1.1.1.1:443" {
			t.Fatalf("resource DNS did not use compiler DoH: %s", target)
		}
	default:
		t.Fatal("resource bypassed selected proxy")
	}
	if protector.calls.Load() == 0 {
		t.Fatal("proxy connection bypassed protection")
	}
	if l.running {
		t.Fatal("resource activated user traffic")
	}
}

func TestAndroidAcquisitionProtectionRefusalSendsNoProxyPacket(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	profile := &config.Profile{Lines: []config.Line{{ID: "source", Type: config.LineTypeShadowsocks, Enabled: true, SSServer: "127.0.0.1", SSPort: listener.Addr().(*net.TCPAddr).Port, SSMethod: "none", SSPass: "fixture"}}}
	l := New(nil)
	protector := &acquisitionCounter{refuse: true}
	l.SetSocketProtector(protector)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resource, err := l.newAndroidAcquisition(ctx, profile, "source", `["192.0.2.53"]`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer resource.instance.Close()
	if _, err = resource.fetch("https://rules.example.invalid/r.json", 1024); err == nil {
		t.Fatal("refused protection succeeded")
	}
	if protector.calls.Load() == 0 {
		t.Fatal("protection was not consulted")
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if conn, err := listener.Accept(); err == nil {
		conn.Close()
		t.Fatal("socket connected before protection")
	}
}
