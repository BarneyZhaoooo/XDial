package mobile

import (
	"encoding/json"
	"github.com/kafeifei/xdial/core/config"
	"strings"
	"testing"
)

func TestTailscaleSetupRetainsSelectionAndMetadata(t *testing.T) {
	raw, err := EnsureTailscaleSetup(testProfile)
	if err != nil {
		t.Fatal(err)
	}
	var setup struct {
		Profile string
		LineID  string `json:"line_id"`
	}
	json.Unmarshal([]byte(raw), &setup)
	before, _ := config.ParseRuntimeProfile([]byte(testProfile))
	after, _ := config.ParseRuntimeProfile([]byte(setup.Profile))
	if after.ActiveScenarioID != before.ActiveScenarioID || !strings.Contains(setup.Profile, `"extension_metadata":{"keep":true}`) {
		t.Fatal("setup changed selection or metadata")
	}
	repeat, err := EnsureTailscaleSetup(setup.Profile)
	if err != nil || repeat != raw {
		t.Fatal("setup duplicated identity")
	}
	status := `{"backend_state":"Running","exit_nodes":[{"id":"node","name":"Home","ip":"100.64.0.8","online":true}]}`
	selected, err := SelectTailscaleExit(setup.Profile, setup.LineID, status, "node")
	if err != nil {
		t.Fatal(err)
	}
	result, _ := config.ParseRuntimeProfile([]byte(selected))
	line := result.FindLine(result.ActiveScenario().DefaultLineID)
	if line.TailscaleExitNode != "100.64.0.8" || line.ID == setup.LineID || len(result.Scenarios) != len(after.Scenarios)+1 {
		t.Fatal("selection must add independent binding")
	}
	if result.FindLine(setup.LineID).TailscaleExitNode != "" {
		t.Fatal("original line mutated")
	}
	for _, bad := range []string{strings.Replace(status, "Running", "NeedsLogin", 1), strings.Replace(status, "true", "false", 1), strings.Replace(status, "100.64.0.8", "invalid", 1)} {
		if _, err := SelectTailscaleExit(setup.Profile, setup.LineID, bad, "node"); err == nil {
			t.Fatal("accepted stale or unavailable exit")
		}
	}
	if _, err := SelectTailscaleExit(setup.Profile, setup.LineID, status, "absent"); err == nil {
		t.Fatal("accepted absent exit")
	}
}
func TestTailscaleSetupFirstRun(t *testing.T) {
	if _, err := EnsureTailscaleSetup(""); err != nil {
		t.Fatal(err)
	}
}
func TestTailscaleBrowserURL(t *testing.T) {
	for _, raw := range []string{"http://login.example.invalid/", "file:///tmp/x", "https://u:p@login.example.invalid/", "javascript:alert(1)", "https:///login"} {
		if ValidateTailscaleAuthURL(raw) {
			t.Fatal("unsafe URL accepted")
		}
	}
	if !ValidateTailscaleAuthURL("https://login.example.invalid/a/test") {
		t.Fatal("valid URL rejected")
	}
}
