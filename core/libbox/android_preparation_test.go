//go:build !windows

package libbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
)

const androidRemoteProfile = `{"lines":[{"id":"direct","type":"direct","enabled":true}],"rule_sets":[{"id":"rules","type":"url","enabled":true,"url":"https://rules.example.invalid/list.json","format":"source"}],"scenarios":[{"id":"s","default_line_id":"direct","bindings":[{"rule_set_id":"rules","line_id":"direct"}]}],"active_scenario_id":"s"}`

func TestAndroidUnboundCompilerIsCacheOnly(t *testing.T) {
	if _, err := GenerateAndroidConfig(androidRemoteProfile, `["192.0.2.53"]`, t.TempDir()); err == nil {
		t.Fatal("cold cache unexpectedly compiled")
	}
	root := t.TempDir()
	profile, _ := config.ParseRuntimeProfile([]byte(androidRemoteProfile))
	calls := 0
	err := materializeNERuleSets(profile, root, func(string, int64) ([]byte, error) {
		calls++
		return []byte(`{"version":3,"rules":[{"domain_suffix":["example.invalid"]}]}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = GenerateAndroidConfig(androidRemoteProfile, `["192.0.2.53"]`, root); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unexpected fetch count")
	}
}

func TestAndroidPreparationProtectsDNSAndRefusesFallback(t *testing.T) {
	l := New(nil)
	protector := &testSocketProtector{err: errors.New("denied")}
	l.SetSocketProtector(protector)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	_, err := l.PrepareAndroidConfig(androidRemoteProfile, `["192.0.2.53"]`, t.TempDir())
	if err == nil || strings.Contains(err.Error(), "example.invalid") {
		t.Fatal("failed acquisition not sanitized")
	}
	if protector.fd == 0 {
		t.Fatal("acquisition bypassed protector")
	}
	if l.running || l.startPreparing {
		t.Fatal("preparation leaked a live runtime")
	}
	// An explicitly selected source must never be replaced with Direct.
	raw := strings.Replace(androidRemoteProfile, `"format":"source"`, `"format":"source","fetch_line_id":"other"`, 1)
	protector.fd = 0
	if _, err = l.PrepareAndroidConfig(raw, `["192.0.2.53"]`, t.TempDir()); err == nil {
		t.Fatal("unavailable acquisition Line accepted")
	}
	if protector.fd != 0 {
		t.Fatal("wrong Line contacted Direct")
	}
}

func TestAndroidBootstrapCannotOwnTUN(t *testing.T) {
	l := New(nil)
	l.SetSocketProtector(&testSocketProtector{})
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	called := false
	l.SetTunOpener(func(*tun.Options, option.TunPlatformOptions) (tun.Tun, error) {
		called = true
		return nil, errors.New("TUN forbidden")
	})
	bootstrap, err := l.newAndroidBootstrap(context.Background(), `["192.0.2.53"]`)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.instance.Close()
	if called {
		t.Fatal("preparation opened TUN")
	}
	if l.running {
		t.Fatal("bootstrap changed traffic state")
	}
}

func TestAndroidCancelPreparationInterruptsLease(t *testing.T) {
	l := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.startPreparing = true
	l.startPreparationCancel = cancel
	l.CancelPreparation()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("preparation cancellation blocked")
	}
	l.startPreparing = false
	l.startPreparationCancel = nil
	// Empty generated remote cache with nil fetch must fail, not panic.
	raw, _ := json.Marshal(map[string]any{"route": map[string]any{"rule_set": []any{map[string]any{"type": "remote", "tag": "r", "url": "https://rules.example.invalid/r", "format": "source"}}}})
	if _, err := materializeGeneratedNERuleSets(raw, t.TempDir(), nil); err == nil {
		t.Fatal("cold generated rule accepted")
	}
}

type blockingPreparationProtector struct {
	entered, release chan struct{}
	once             sync.Once
}

func (p *blockingPreparationProtector) ProtectSocket(int) error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return errors.New("refused")
}
func TestAndroidStopRetiresPreparationBeforeReuse(t *testing.T) {
	l := New(nil)
	p := &blockingPreparationProtector{entered: make(chan struct{}), release: make(chan struct{})}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	done := make(chan error, 1)
	go func() {
		_, err := l.PrepareAndroidConfig(androidRemoteProfile, `["192.0.2.53"]`, t.TempDir())
		done <- err
	}()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("preparation did not request protected DNS")
	}
	if err := l.SetSocketProtector(&testSocketProtector{}); err == nil {
		t.Fatal("changed preparation capability")
	}
	l.CancelPreparation()
	stopped := make(chan error, 1)
	go func() { stopped <- l.Stop() }()
	close(p.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled preparation succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("preparation did not retire")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not retire")
	}
	if err := l.SetSocketProtector(&testSocketProtector{}); err != nil {
		t.Fatal("retired preparation still owns capability")
	}
}
