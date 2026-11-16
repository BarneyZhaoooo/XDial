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
