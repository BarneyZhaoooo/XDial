//go:build !windows

package libbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAndroidSetupHasProtectedCompilerDNSAndNoIngress(t *testing.T) {
	profile := `{"lines":[{"id":"tail","type":"tailscale","enabled":true}],"scenarios":[{"id":"s","default_line_id":"tail"}],"active_scenario_id":"s"}`
	raw, err := GenerateAndroidTailscaleSetupConfig(profile, "tail", `["192.0.2.53"]`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal([]byte(raw), &doc)
	if len(doc["inbounds"].([]any)) != 0 {
		t.Fatal("setup captures traffic")
	}
	dns, _ := json.Marshal(doc["dns"])
	if strings.Contains(string(dns), `"local"`) || !strings.Contains(string(dns), "192.0.2.53") {
		t.Fatal("setup bypasses captured DNS")
	}
	route := doc["route"].(map[string]any)
	if route["auto_detect_interface"] != true || route["final"] != "direct" || route["default_domain_resolver"] != "xdial-public-dns" {
		t.Fatal("setup lost protected compiler route", route)
	}
	endpoint := doc["endpoints"].([]any)[0].(map[string]any)
	if endpoint["exit_node"] != nil || endpoint["accept_routes"] != false || endpoint["system_interface"] != false {
		t.Fatal("setup enables forwarding")
	}
	if _, err := GenerateAndroidTailscaleSetupConfig(profile, "tail", `[]`, t.TempDir()); err == nil {
		t.Fatal("missing DNS accepted")
	}
	apple, err := GenerateTailscaleSetupConfig(profile, "tail", t.TempDir())
	if err != nil || !strings.Contains(apple, `"type":"local"`) {
		t.Fatal("Apple setup changed")
	}
}
