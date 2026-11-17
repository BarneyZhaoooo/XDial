package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAndroidAcquisitionOwnsExactDNSAndNoIngress(t *testing.T) {
	profile := &Profile{Lines: []Line{
		{ID: "direct", Type: LineTypeDirect, Enabled: true},
		{ID: "source", Type: LineTypeShadowsocks, Enabled: true, SSServer: "127.0.0.1", SSPort: 12345, SSMethod: "none", SSPass: "fixture"},
	}, RuleSets: []RuleSet{{ID: "private-rule", Type: RuleSetTypeURL, URL: "https://rules.invalid/private", Enabled: true}}, Scenarios: []Scenario{{ID: "traffic", DefaultLineID: "direct"}}, ActiveScenarioID: "traffic"}
	for _, lineID := range []string{"direct", "source"} {
		data, err := GenerateAndroidAcquisition(profile, lineID, t.TempDir(), []string{"192.0.2.53"})
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err = json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"inbounds", "experimental"} {
			if doc[key] != nil {
				t.Fatalf("resource retained %s", key)
			}
		}
		if strings.Contains(string(data), "private-rule") || strings.Contains(string(data), "resource-placeholder") || strings.Contains(string(data), `"type":"local"`) {
			t.Fatal("resource retained traffic or host resolver")
		}
		route := doc["route"].(map[string]any)
		dns := doc["dns"].(map[string]any)
		if route["auto_detect_interface"] != true || route["default_interface"] != nil {
			t.Fatal("Android socket protection bypassed")
		}
		found := false
		for _, raw := range dns["servers"].([]any) {
			server := raw.(map[string]any)
			if server["tag"] != dns["final"] {
				continue
			}
			found = true
			if lineID == "direct" {
				if server["server"] != "192.0.2.53" || route["final"] != "direct" {
					t.Fatal("Direct changed OS facts")
				}
			} else if server["detour"] != route["final"] || server["type"] != "https" {
				t.Fatal("DNS and HTTPS split across Lines")
			}
		}
		if !found {
			t.Fatal("exact resolver missing")
		}
	}
	if profile.ActiveScenarioID != "traffic" || len(profile.RuleSets) != 1 {
		t.Fatal("resource mutated user profile")
	}
}

func TestAndroidAcquisitionProtocolRequiresLease(t *testing.T) {
	for _, kind := range []LineType{LineTypeVPN, LineTypeTailscale} {
		profile := &Profile{Lines: []Line{{ID: "protocol", Type: kind, Enabled: true, VPNServer: "vpn.invalid", TailscaleExitNode: "100.64.0.1"}}}
		if _, err := GenerateAndroidAcquisition(profile, "protocol", t.TempDir(), []string{"192.0.2.53"}); !errors.Is(err, ErrAndroidAcquisitionProtocolNeedsLease) {
			t.Fatalf("protocol capability not guarded: %v", err)
		}
	}
}
