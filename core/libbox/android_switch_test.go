//go:build with_gvisor && !windows

package libbox

import (
	"context"
	"errors"
	"github.com/kafeifei/xdial/core/config"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
)

const androidSwitchProfile = `{"lines":[{"id":"direct","type":"direct","enabled":true}],"scenarios":[{"id":"one","default_line_id":"direct"},{"id":"two","default_line_id":"direct"}],"active_scenario_id":"one"}`

func TestAndroidSwitchKeepsIngressAndDetachedCandidate(t *testing.T) {
	l := New(nil)
	l.SetSocketProtector(&testSocketProtector{})
	l.SetNetworkInterfaces(switchNetworkInterfaces)
	l.SetDefaultInterface("test0", 1)
	dev := newFakeGVisorTun(1500)
	opens := 0
	l.SetTunOpener(func(*tun.Options, option.TunPlatformOptions) (tun.Tun, error) { opens++; return dev, nil })
	if err := l.SetStablePacketIngress(true); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	config, err := l.PrepareAndroidConfig(androidSwitchProfile, `["192.0.2.53"]`, base)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.StartAndroidSession(androidSwitchProfile, config, `["192.0.2.53"]`); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	source := l.box
	ingress := l.packetIngress
	c, err := NewScenarioController(androidSwitchProfile, "{}", false)
	if err != nil {
		t.Fatal(err)
	}
	c.SelectScenario("two")
	target, err := c.ProfileForProposal(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.PrepareAndroidSwitch(target, `["192.0.2.53"]`, base, switchNetworkInterfaces, "test0", 1); err != nil {
		t.Fatal(err)
	}
	// The wrapper releases/cancels its preparation lease on return. It must not
	// cancel the independently owned candidate or its future flows.
	if l.preparedSwitch.runtimeCtx.Err() != nil {
		t.Fatal("candidate tied to preparation lifetime")
	}
	if l.box != source || opens != 1 {
		t.Fatal("preparation replaced source or TUN")
	}
	if ready, err := l.AndroidPreparedSwitchReady(target); err != nil || !ready {
		t.Fatalf("%v %v", ready, err)
	}
	if err = l.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if err = l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	if l.packetIngress != ingress || opens != 1 || l.runtimeCtx.Err() != nil || l.packetIngress.generation().ctx.Err() != nil {
		t.Fatal("source retirement killed candidate/ingress")
	}
	if err = l.PrepareAndroidSwitch(target, `["192.0.2.53"]`, base, switchNetworkInterfaces, "test0", 1); err != nil {
		t.Fatal(err)
	}
	l.CancelPreparation()
	if err = l.CommitPreparedSwitch(); err == nil {
		t.Fatal("cancelled candidate committed")
	}
	if err = l.AbortPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if l.runtimeCtx.Err() != nil {
		t.Fatal("cancel killed committed runtime")
	}
}

func TestPacketSwitchParentCancellationOnlyDuringPreparation(t *testing.T) {
	l, _, _ := startPacketTestRuntime(t)
	parent, cancel := context.WithCancel(context.Background())
	if err := l.prepareSwitchContext(parent, packetTestConfig("192.0.2.2", false), "", "", nil, "", "", switchNetworkInterfaces, "test0", 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	if l.preparedSwitch.runtimeCtx.Err() != nil {
		t.Fatal("published candidate retained preparation parent")
	}
	l.AbortPreparedSwitch()
	parent, cancel = context.WithCancel(context.Background())
	cancel()
	if err := l.prepareSwitchContext(parent, packetTestConfig("192.0.2.3", false), "", "", nil, "", "", switchNetworkInterfaces, "test0", 1); err == nil {
		t.Fatal("cancelled parent published candidate")
	}
	if l.preparedSwitch != nil || l.runtimeCtx.Err() != nil {
		t.Fatal("failed preparation damaged source")
	}
}

func TestPacketGenerationCloseWaitsForAllBorrowers(t *testing.T) {
	g := newPacketGeneration(context.Background(), nil)
	if !g.enter() {
		t.Fatal("cannot enter")
	}
	first, second := make(chan struct{}), make(chan struct{})
	go func() { g.close(); close(first) }()
	<-g.ctx.Done()
	go func() { g.close(); close(second) }()
	select {
	case <-second:
		t.Fatal("second close bypassed outstanding borrower")
	case <-time.After(20 * time.Millisecond):
	}
	g.calls.Done()
	for _, done := range []chan struct{}{first, second} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("close stuck")
		}
	}
	if !errors.Is(g.ctx.Err(), context.Canceled) {
		t.Fatal("context not cancelled")
	}
}

func TestAndroidSwitchReconnectOnlyForExclusiveIdentityChanges(t *testing.T) {
	vpn := `{"lines":[{"id":"vpn","type":"vpn","enabled":true,"vpn_server":"https://switch.invalid","vpn_username":"fixture","vpn_password":"fixture"}],"scenarios":[{"id":"s","default_line_id":"vpn"}],"active_scenario_id":"s"}`
	p, err := config.ParseRuntimeProfile([]byte(vpn))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := lineRuntimeIdentity(p, p.ActiveVPNLine())
	if err != nil {
		t.Fatal(err)
	}
	l := New(nil)
	l.running = true
	l.packetIngress = &packetIngress{}
	l.activeAnyConnectCapability = &anyConnectRuntimeCapability{}
	l.anyConnectRuntimeIdentity = identity
	if reconnect, err := l.AndroidSwitchRequiresReconnect(vpn); err != nil || reconnect {
		t.Fatalf("same identity %v %v", reconnect, err)
	}
	changed := strings.Replace(vpn, "switch.invalid", "changed.invalid", 1)
	if reconnect, err := l.AndroidSwitchRequiresReconnect(changed); err != nil || !reconnect {
		t.Fatalf("changed identity %v %v", reconnect, err)
	}
	if reconnect, err := l.AndroidSwitchRequiresReconnect(androidSwitchProfile); err != nil || reconnect {
		t.Fatalf("Direct must drain old VPN %v %v", reconnect, err)
	}
	l.running = false
	if _, err = l.AndroidSwitchRequiresReconnect(vpn); err == nil {
		t.Fatal("stopped runtime classified as usable")
	}
}
