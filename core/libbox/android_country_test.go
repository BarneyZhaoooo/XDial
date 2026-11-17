//go:build !windows

package libbox

import (
	"errors"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"strings"
	"testing"
	"time"
)

func TestAndroidCountryRequiresProtectionAndNeverOpensTUN(t *testing.T) {
	l := New(nil)
	if _, err := l.ProbeAndroidUnderlayCountry(`["192.0.2.53"]`, 1000); err == nil {
		t.Fatal("unprotected survey accepted")
	}
	p := &testSocketProtector{err: errors.New("private fixture denied")}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	opened := false
	l.SetTunOpener(func(*tun.Options, option.TunPlatformOptions) (tun.Tun, error) {
		opened = true
		return nil, errors.New("forbidden")
	})
	_, err := l.ProbeAndroidUnderlayCountry(`["192.0.2.53"]`, 1000)
	if err == nil || strings.Contains(err.Error(), "private") || p.fd == 0 {
		t.Fatalf("err=%v protected=%v", err, p.fd != 0)
	}
	if opened || l.running || l.startPreparing {
		t.Fatal("survey acquired traffic or leaked lease")
	}
	for _, duration := range []int{-1, 0, 99, 10001} {
		if _, err := l.ProbeAndroidUnderlayCountry(`[]`, duration); err == nil {
			t.Fatal("invalid deadline")
		}
	}
}

func TestAndroidCountryStopCancelsAndRetiresLease(t *testing.T) {
	l := New(nil)
	p := &blockingPreparationProtector{entered: make(chan struct{}), release: make(chan struct{})}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	done := make(chan error, 1)
	go func() { _, err := l.ProbeAndroidUnderlayCountry(`["192.0.2.53"]`, 5000); done <- err }()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no protected query")
	}
	if _, err := l.ProbeAndroidUnderlayCountry(`[]`, 1000); err == nil {
		t.Fatal("concurrent observation")
	}
	if err := l.SetSocketProtector(&testSocketProtector{}); err == nil {
		t.Fatal("lease allowed network replacement")
	}
	l.CancelPreparation()
	stopped := make(chan error, 1)
	go func() { stopped <- l.Stop() }()
	close(p.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled survey succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("survey stuck")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop stuck")
	}
	if err := l.SetSocketProtector(&testSocketProtector{}); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchRejectsSecondTUNBeforeCandidateConstruction(t *testing.T) {
	l := newLoopbackFlowRuntime(t)
	defer l.Stop()
	old := l.box
	opened := false
	l.SetTunOpener(func(*tun.Options, option.TunPlatformOptions) (tun.Tun, error) {
		opened = true
		return nil, errors.New("unexpected TUN")
	})
	err := l.PrepareSwitch(`{"inbounds":[{"type":"tun","tag":"tun-in"}]}`, "", `[]`, "", 0)
	if err == nil || !strings.Contains(err.Error(), "flow ingress") {
		t.Fatalf("wrong rejection: %v", err)
	}
	if opened || l.box != old || !l.running || l.preparedSwitch != nil {
		t.Fatal("candidate touched active ingress")
	}
}
