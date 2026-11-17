package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func linuxFlowProfile() *Profile {
	return &Profile{Lines: []Line{{ID: "direct", Type: LineTypeDirect, Enabled: true}}, RuleSets: []RuleSet{{ID: "apps", Type: RuleSetTypeApplication, Enabled: true, Processes: []string{"/opt/Client/a", "/opt/Client/", "browser"}}}, Scenarios: []Scenario{{ID: "one", DefaultLineID: "direct", Bindings: []RuleBinding{{RuleSetID: "apps", LineID: "direct"}}}}, ActiveScenarioID: "one"}
}
func TestLinuxFlowCompiler(t *testing.T) {
	p := linuxFlowProfile()
	b, e := GenerateLinuxFlow(p, t.TempDir(), "192.0.2.53", 1<<17)
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, bad := range []string{`"auth_user"`, `"inbounds"`, `"type":"local"`, `"auto_detect_interface"`, `"default_interface"`} {
		if strings.Contains(s, bad) {
			t.Fatalf("retained %s", bad)
		}
	}
	for _, want := range []string{`"process_path"`, `"process_name"`, `"process_path_regex"`, `"default_mark":131072`, `"server":"192.0.2.53"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s: %s", want, s)
		}
	}
	var doc map[string]any
	json.Unmarshal(b, &doc)
	r := doc["route"].(map[string]any)["rules"].([]any)
	found := false
	for _, raw := range r {
		v := raw.(map[string]any)
		if v["type"] == "logical" && v["outbound"] == "direct" {
			found = true
			if v["mode"] != "or" || len(v["rules"].([]any)) != 3 {
				t.Fatal("process union changed")
			}
		}
	}
	if !found {
		t.Fatal("missing process predicate")
	}
}
func TestLinuxFlowRejectsUnsupportedAdmission(t *testing.T) {
	for _, dns := range []string{"127.0.0.53", "::1", "", "255.255.255.255"} {
		if _, e := GenerateLinuxFlow(linuxFlowProfile(), t.TempDir(), dns, 1<<17); e == nil {
			t.Fatalf("accepted %s", dns)
		}
	}
	p := linuxFlowProfile()
	p.RuleSets[0].Processes = []string{"app*"}
	if _, e := GenerateLinuxFlow(p, t.TempDir(), "192.0.2.53", 1<<17); e == nil {
		t.Fatal("wildcard silently reduced")
	}
	p = linuxFlowProfile()
	p.Lines[0].NativeOptions = json.RawMessage(`{"tls":{"certificate_path":"/root/key"}}`)
	if _, e := GenerateLinuxFlow(p, t.TempDir(), "192.0.2.53", 1<<17); e == nil {
		t.Fatal("privileged file option accepted")
	}
}
