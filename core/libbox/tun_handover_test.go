//go:build with_gvisor && !windows

package libbox

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/sing-box/option"
	singtun "github.com/sagernet/sing-tun"
)

// handoverTUN records whether the TUN inbound that opened it has closed it.
type handoverTUN struct {
	*fakeGVisorTun
	closed atomic.Bool
}

func (t *handoverTUN) Close() error {
	t.closed.Store(true)
	return t.fakeGVisorTun.Close()
}

// open reports whether a generation still reads this device.
func (t *handoverTUN) open() bool {
	return t.ep.IsAttached() && !t.closed.Load()
}

// handoverTUNs hands out one fake gVisor TUN per OpenInterface call, the way
// dup(packetFlow fd) gives every TUN inbound its own device on iOS.
type handoverTUNs struct {
	mu     sync.Mutex
	opened []*handoverTUN
	fail   map[int]bool
}

func (h *handoverTUNs) open(*singtun.Options, option.TunPlatformOptions) (singtun.Tun, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := len(h.opened) + 1
	if h.fail[call] {
		h.opened = append(h.opened, nil)
		return nil, fmt.Errorf("test TUN open %d refused", call)
	}
	device := &handoverTUN{fakeGVisorTun: newFakeGVisorTun(9000)}
	h.opened = append(h.opened, device)
	return device, nil
}

func (h *handoverTUNs) device(t *testing.T, call int) *handoverTUN {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if call > len(h.opened) || h.opened[call-1] == nil {
		t.Fatalf("TUN open %d did not produce a device (opened %d)", call, len(h.opened))
	}
	return h.opened[call-1]
}

func (h *handoverTUNs) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.opened)
}

type handoverTestCallback struct {
	mu     sync.Mutex
	errors []string
}

func (c *handoverTestCallback) OnStatusChanged(string) {}

func (c *handoverTestCallback) OnError(_ int, message string) {
	c.mu.Lock()
	c.errors = append(c.errors, message)
	c.mu.Unlock()
}

func (c *handoverTestCallback) reported() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.errors...)
}

func handoverConfig(outboundTag string) string {
	return fmt.Sprintf(`{
		"log":{"disabled":true},
		"inbounds":[{"type":"tun","tag":"tun-in","address":["198.18.0.1/15"],"mtu":9000,"stack":"gvisor"}],
		"outbounds":[{"type":"direct","tag":%q}],
		"route":{"final":%q}
	}`, outboundTag, outboundTag)
}

func startHandoverLibbox(t *testing.T, tuns *handoverTUNs) (*Libbox, *handoverTestCallback) {
	t.Helper()
	callback := &handoverTestCallback{}
	instance := New(callback)
	instance.SetTunOpener(tuns.open)
	instance.SetSwitchTUNHandover(true)
	instance.SetDebugRoutingProbeEnabled(true)
	if err := instance.SetNetworkInterfaces(switchNetworkInterfaces); err != nil {
		t.Fatal(err)
	}
	instance.SetDefaultInterface("test0", 1)
	if err := instance.StartStandalone(handoverConfig("source-direct")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Stop() })
	return instance, callback
}

// routeThroughTUN injects a SYN to the routing-probe Direct target and waits
// until the generation reading this TUN has routed it.
func routeThroughTUN(t *testing.T, instance *Libbox, device *handoverTUN, sourcePort uint16, wantTag string) {
	t.Helper()
	device.injectInbound(buildTCPSyn(
		tcpip.AddrFromSlice([]byte{198, 18, 0, 3}),
		tcpip.AddrFromSlice(directProbeTarget.AsSlice()),
		sourcePort, 443,
	))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(instance.RoutingProbeSnapshot(), fmt.Sprintf("%q:1", wantTag)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no connection routed to %s: %s", wantTag, instance.RoutingProbeSnapshot())
}

func prepareHandoverCandidate(t *testing.T, instance *Libbox, tuns *handoverTUNs) {
	t.Helper()
	if err := prepareSwitchForTest(instance, handoverConfig("candidate-direct"), ""); err != nil {
		t.Fatal(err)
	}
	if got := tuns.count(); got != 1 {
		t.Fatalf("candidate opened the TUN during preparation: %d opens", got)
	}
	if !tuns.device(t, 1).open() {
		t.Fatal("preparation detached the active TUN")
	}
}

func TestTUNHandoverMovesPacketFlowToCandidateAtCommit(t *testing.T) {
	tuns := &handoverTUNs{}
	instance, callback := startHandoverLibbox(t, tuns)
	routeThroughTUN(t, instance, tuns.device(t, 1), 40001, "source-direct")

	prepareHandoverCandidate(t, instance, tuns)
	if err := instance.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if tuns.device(t, 1).open() {
		t.Fatal("source generation still reads the TUN after commit")
	}
	if got := tuns.count(); got != 2 {
		t.Fatalf("commit opened %d TUN devices, want 2", got)
	}
	routeThroughTUN(t, instance, tuns.device(t, 2), 40002, "candidate-direct")

	if err := instance.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	if !tuns.device(t, 2).open() {
		t.Fatal("retiring the source closed the committed TUN")
	}
	if reported := callback.reported(); len(reported) != 0 {
		t.Fatalf("unexpected engine errors: %v", reported)
	}

	// The committed generation can itself be switched again.
	if err := prepareSwitchForTest(instance, handoverConfig("third-direct"), ""); err != nil {
		t.Fatal(err)
	}
	if err := instance.CommitPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	routeThroughTUN(t, instance, tuns.device(t, 3), 40003, "third-direct")
}

func TestTUNHandoverReopensSourceWhenCandidateCannotOpen(t *testing.T) {
	tuns := &handoverTUNs{fail: map[int]bool{2: true}}
	instance, callback := startHandoverLibbox(t, tuns)
	prepareHandoverCandidate(t, instance, tuns)

	err := instance.CommitPreparedSwitch()
	if err == nil || errors.Is(err, errTUNHandoverLost) {
		t.Fatalf("commit error = %v, want a recoverable handover failure", err)
	}
	routeThroughTUN(t, instance, tuns.device(t, 3), 40011, "source-direct")
	if err := instance.AbortPreparedSwitch(); err != nil {
		t.Fatal(err)
	}
	if !tuns.device(t, 3).open() {
		t.Fatal("aborting the candidate closed the reopened source TUN")
	}
	if reported := callback.reported(); len(reported) != 0 {
		t.Fatalf("recoverable handover failure was reported as fatal: %v", reported)
	}
}

func TestTUNHandoverReportsLostTunnel(t *testing.T) {
	tuns := &handoverTUNs{fail: map[int]bool{2: true, 3: true}}
	instance, callback := startHandoverLibbox(t, tuns)
	prepareHandoverCandidate(t, instance, tuns)

	if err := instance.CommitPreparedSwitch(); !errors.Is(err, errTUNHandoverLost) {
		t.Fatalf("commit error = %v, want errTUNHandoverLost", err)
	}
	reported := callback.reported()
	if len(reported) != 1 || !strings.Contains(reported[0], errTUNHandoverLost.Error()) {
		t.Fatalf("lost tunnel was not reported to the Provider: %v", reported)
	}
}

func TestTUNHandoverRequiresTUNInbound(t *testing.T) {
	tuns := &handoverTUNs{}
	instance, _ := startHandoverLibbox(t, tuns)
	err := prepareSwitchForTest(instance, `{
		"log":{"disabled":true},
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct"}
	}`, "")
	if err == nil || !strings.Contains(err.Error(), "TUN inbound") {
		t.Fatalf("prepare error = %v, want missing TUN inbound", err)
	}
	routeThroughTUN(t, instance, tuns.device(t, 1), 40021, "source-direct")
}

const handoverAnyConnectConfig = `{
	"log":{"disabled":true},
	"inbounds":[{"type":"tun","tag":"tun-in","address":["198.18.0.1/15"],"mtu":9000,"stack":"gvisor"}],
	"outbounds":[
		{"type":"vpn","tag":"vpn"},
		{"type":"direct","tag":"direct"}
	],
	"route":{"final":"vpn"}
}`

// Two Scenarios over the same AnyConnect Line: the candidate borrows the
// connected session, so the switch neither redials nor drops it, and the TUN
// moves to the candidate only at Commit.
func TestTUNHandoverReusesAnyConnectSessionWithoutRedial(t *testing.T) {
	const identity = "line-runtime-v1:office"
	tuns := &handoverTUNs{}
	client := &switchTestVPNClient{session: newStartTestSession()}
	platform := &xdPlatformInterface{opener: tuns.open}
	platform.setDefaultInterface("test0", 1)
	instance := &Libbox{vpn: client, platform: platform, tunHandover: true}
	if err := instance.StartResolvedWithInsecureAndRuntimeIdentity(
		"vpn.example.com:443", "192.0.2.10", "employee", "secret", false,
		identity, handoverAnyConnectConfig,
	); err != nil {
		t.Fatalf("start source generation: %v", err)
	}
	defer instance.Stop()
	if got := tuns.count(); got != 1 {
		t.Fatalf("source generation opened %d TUN devices", got)
	}

	if err := prepareSwitchWithLineIDForTest(instance, handoverAnyConnectConfig, "company", identity); err != nil {
		t.Fatalf("prepare switch: %v", err)
	}
	if got := tuns.count(); got != 1 {
		t.Fatalf("candidate opened the TUN during preparation: %d opens", got)
	}
	reused, err := instance.PreparedSwitchReusedLineIDs()
	if err != nil || reused != `["company"]` {
		t.Fatalf("reused Lines = %q, %v; want the AnyConnect Line", reused, err)
	}
	if err := instance.CommitPreparedSwitch(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := instance.RetireCommittedSwitch(); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if tuns.device(t, 1).open() || !tuns.device(t, 2).open() {
		t.Fatal("TUN did not move to the committed generation")
	}
	if connects, disconnects := client.counts(); connects != 1 || disconnects != 0 {
		t.Fatalf("AnyConnect connects=%d disconnects=%d, want the original session kept", connects, disconnects)
	}
}
