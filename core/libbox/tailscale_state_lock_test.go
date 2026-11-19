package libbox

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTailscaleStateLockExcludesConcurrentRuntimeAndReleases(t *testing.T) {
	stateDirectory := filepath.Join(t.TempDir(), "tailscale", "home")
	configJSON := `{"endpoints":[` +
		`{"type":"tailscale","tag":"tailscale-home","state_directory":` +
		strconv.Quote(stateDirectory) +
		`}]}`

	first, err := acquireTailscaleStateLocks(configJSON)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	t.Cleanup(func() {
		releaseTailscaleStateLocks(first)
	})

	second, err := acquireTailscaleStateLocks(configJSON)
	if !errors.Is(err, errTailscaleStateInUse) {
		releaseTailscaleStateLocks(second)
		t.Fatalf("second lock error = %v, want %v", err, errTailscaleStateInUse)
	}

	releaseTailscaleStateLocks(first)
	first = nil
	third, err := acquireTailscaleStateLocks(configJSON)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	releaseTailscaleStateLocks(third)
}

func TestTailscaleStateLockIgnoresConfigsWithoutTailscale(t *testing.T) {
	locks, err := acquireTailscaleStateLocks(
		`{"endpoints":[{"type":"wireguard","tag":"other"}]}`,
	)
	if err != nil {
		t.Fatalf("lock direct config: %v", err)
	}
	if len(locks) != 0 {
		releaseTailscaleStateLocks(locks)
		t.Fatalf("locks = %d, want 0", len(locks))
	}
}

// The iOS app runs the isolated Tailscale setup with the App Group container as
// its base path. The state lock it holds until the runtime stops therefore lives
// in that shared container, which is what RunningBoard checks at suspension
// (0xdead10cc); the app must stop the setup before it is suspended.
func TestTailscaleSetupStateLockLivesInTheSharedContainer(t *testing.T) {
	container := t.TempDir()
	profile := `{"lines":[{"id":"tail","type":"tailscale","enabled":true}],"scenarios":[{"id":"s","default_line_id":"tail"}],"active_scenario_id":"s"}`
	configJSON, err := GenerateTailscaleSetupConfigWithAuthKey(profile, "tail", container, "")
	if err != nil {
		t.Fatal(err)
	}
	locks, err := acquireTailscaleStateLocks(configJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 {
		releaseTailscaleStateLocks(locks)
		t.Fatalf("locks = %d, want 1", len(locks))
	}
	relative, err := filepath.Rel(container, locks[0].Name())
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		releaseTailscaleStateLocks(locks)
		t.Fatalf("lock %s is outside the shared container %s", locks[0].Name(), container)
	}
	// Stopping the setup releases it, so the next runtime (or the extension) can take it.
	releaseTailscaleStateLocks(locks)
	again, err := acquireTailscaleStateLocks(configJSON)
	if err != nil {
		t.Fatalf("lock after the setup stopped: %v", err)
	}
	releaseTailscaleStateLocks(again)
}
