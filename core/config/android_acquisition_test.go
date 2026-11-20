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

func TestAppleResourceAcquisitionKeepsOSUnderlayRouting(t *testing.T) {
	profile := &Profile{Lines: []Line{{ID: "source", Type: LineTypeShadowsocks, Enabled: true, SSServer: "198.51.100.7", SSPort: 8388, SSMethod: "aes-128-gcm", SSPass: "fixture"}}}
	apple, err := GenerateAppleResourceAcquisition(profile, "source", t.TempDir(), []string{"192.0.2.53"})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Route    map[string]any `json:"route"`
		Inbounds []any          `json:"inbounds"`
	}
	if err := json.Unmarshal(apple, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Route["auto_detect_interface"]; ok {
		t.Fatal("Apple resource session would skip a gateway-less utun Underlay")
	}
	if _, ok := document.Route["default_interface"]; ok {
		t.Fatal("Apple resource session pinned one interface instead of OS routing")
	}
	if len(document.Inbounds) != 0 {
		t.Fatal("resource session gained an ingress")
	}
	android, err := GenerateAndroidAcquisition(profile, "source", t.TempDir(), []string{"192.0.2.53"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(android, &document); err != nil {
		t.Fatal(err)
	}
	if document.Route["auto_detect_interface"] != true {
		t.Fatal("Android resource session lost its callback-selected network")
	}
}

func TestAppleColdRuleAcquisitionIsolatesProtocolAndDNS(t *testing.T) {
	profile := &Profile{Lines: []Line{
		{ID: "vpn", Type: LineTypeVPN, Enabled: true, VPNServer: "vpn.invalid"},
		{ID: "tail", Type: LineTypeTailscale, Enabled: true, TailscaleExitNode: "100.64.0.42", TailscaleAuthKey: "unused-registration-key"},
	}}
	for _, lineID := range []string{"vpn", "tail"} {
		t.Run(lineID, func(t *testing.T) {
			dir := t.TempDir()
			servers := []string{"192.0.2.53"}
			if _, err := GenerateAppleResourceAcquisition(profile, lineID, dir, servers); !errors.Is(err, ErrAndroidAcquisitionProtocolNeedsLease) {
				t.Fatalf("parallel source acquisition accepted protocol: %v", err)
			}
			data, err := GenerateAppleColdRuleAcquisition(profile, lineID, dir, servers)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Inbounds, Experimental json.RawMessage
				Outbounds, Endpoints   []map[string]any
				DNS                    struct {
					Final   string
					Servers []map[string]any
				}
				Route struct{ Final string }
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Inbounds != nil || doc.Experimental != nil || doc.Route.Final == "direct" {
				t.Fatal("cold fetch leaked into user traffic")
			}
			var resolver map[string]any
			for _, server := range doc.DNS.Servers {
				if server["tag"] == doc.DNS.Final {
					resolver = server
				}
			}
			if lineID == "vpn" {
				if len(doc.Endpoints) != 0 || doc.Route.Final != "vpn" || resolver["type"] != "https" || resolver["detour"] != "vpn" {
					t.Fatal("AnyConnect cold fetch did not own DNS and HTTPS")
				}
			} else {
				if len(doc.Endpoints) != 1 || doc.Endpoints[0]["tag"] != doc.Route.Final || doc.Endpoints[0]["exit_node"] != "100.64.0.42" ||
					doc.Endpoints[0]["auth_key"] != nil || resolver["detour"] != doc.Route.Final {
					t.Fatal("Tailscale cold fetch did not own DNS, exit and identity")
				}
				for _, outbound := range doc.Outbounds {
					if outbound["type"] == "vpn" {
						t.Fatal("unrelated AnyConnect line activated")
					}
				}
			}
		})
	}
	profile.Lines[1].TailscaleExitNode = ""
	if _, err := GenerateAppleColdRuleAcquisition(profile, "tail", t.TempDir(), []string{"192.0.2.53"}); err == nil {
		t.Fatal("cold Tailscale fetch accepted a missing exit node")
	}
}
