//go:build !windows

package libbox

import "testing"

func TestAndroidReadinessNeverTreatsUnauthenticatedCoreAsConnected(t *testing.T) {
	l := &Libbox{running: true}
	direct := `{"lines":[{"id":"direct","type":"direct","enabled":true}],"scenarios":[{"id":"s","default_line_id":"direct"}],"active_scenario_id":"s"}`
	if ready, err := l.AndroidSessionReady(direct); err != nil || !ready {
		t.Fatal("non-Tailscale session incorrectly gated")
	}
	l.running = false
	if ready, err := l.AndroidSessionReady(direct); err == nil || ready {
		t.Fatal("stopped session became ready")
	}
	l.running = true
	tail := `{"lines":[{"id":"tail","type":"tailscale","enabled":true,"tailscale_exit_node":"100.64.0.8"}],"scenarios":[{"id":"s","default_line_id":"tail"}],"active_scenario_id":"s"}`
	if ready, err := l.AndroidSessionReady(tail); err == nil || ready {
		t.Fatal("absent Tailscale runtime became connected")
	}
}
