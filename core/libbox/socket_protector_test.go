//go:build !windows

package libbox

import (
	"errors"
	"testing"
)

type testSocketProtector struct {
	fd  int
	err error
}

func (p *testSocketProtector) ProtectSocket(fd int) error { p.fd = fd; return p.err }

func TestSocketProtectionSurvivesPlatformOwnership(t *testing.T) {
	sentinel := errors.New("protection denied")
	protector := &testSocketProtector{err: sentinel}
	library := New(nil)
	if err := library.SetSocketProtector(protector); err != nil {
		t.Fatal(err)
	}
	candidate, err := library.platform.switchCandidate(`[{"name":"test0","index":7,"type":"other"}]`, "test0", 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []*xdPlatformInterface{library.platform, candidate, library.platform.lineRuntimeCapability()} {
		if !platform.UsePlatformAutoDetectInterfaceControl() {
			t.Fatal("protection disabled")
		}
		if err := platform.AutoDetectInterfaceControl(42); !errors.Is(err, sentinel) {
			t.Fatalf("failure swallowed: %v", err)
		}
		if protector.fd != 42 {
			t.Fatal("borrowed socket not forwarded")
		}
	}
	protector.err = nil
	if err := candidate.AutoDetectInterfaceControl(43); err != nil {
		t.Fatal(err)
	}
	library.running = true
	if err := library.SetSocketProtector(nil); err == nil {
		t.Fatal("active protection replaced")
	}
}

func TestHostInterfaceFactsSurviveRestrictedGoInterfaceLookup(t *testing.T) {
	library := New(nil)
	const snapshot = `[{"name":"android-underlay","index":4242,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`
	if err := library.SetNetworkInterfaces(snapshot); err != nil {
		t.Fatal(err)
	}
	library.SetDefaultInterface("android-underlay", 4242)
	for _, platform := range []*xdPlatformInterface{library.platform, library.platform.lineRuntimeCapability()} {
		interfaces, err := platform.NetworkInterfaces()
		if err != nil || len(interfaces) != 1 {
			t.Fatalf("snapshot lost: %v %v", interfaces, err)
		}
		got := interfaces[0]
		if got.MTU != 1500 || got.Flags&1 == 0 || len(got.Addresses) != 1 || got.Addresses[0].String() != "192.0.2.10/24" {
			t.Fatalf("host facts lost: %+v", got)
		}
		if platform.defaultInterface.MTU != 1500 {
			t.Fatal("default interface discarded host facts")
		}
	}
	candidate, err := library.platform.switchCandidate(snapshot, "android-underlay", 4242)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.defaultInterface.MTU != 1500 {
		t.Fatal("candidate discarded host facts")
	}
	if err := library.SetNetworkInterfaces(`[{"name":"bad","index":4,"type":"wifi","facts":{"mtu":0}}]`); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	interfaces, _ := library.platform.NetworkInterfaces()
	if interfaces[0].Name != "android-underlay" {
		t.Fatal("rejected snapshot mutated live facts")
	}
}
