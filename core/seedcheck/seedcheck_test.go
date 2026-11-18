package seedcheck

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kafeifei/xdial/core/config"
)

const authority = `{"format":"xdial-mobile-seed","schema_version":1,"revision":2,
 "region_scenarios":{"cn":"domestic","*":"overseas"},
 "profile":{"lines":[{"id":"direct","name":"直连","type":"direct","enabled":true},
  {"id":"vpn","name":"公司 VPN","type":"vpn","enabled":true,"vpn_server":"vpn.example.com:8443"}],
  "rule_sets":[{"id":"intranet","name":"公司内网","type":"manual","enabled":true,"domains":["example.com"]},
   {"id":"cn","name":"国内网站","type":"url","enabled":true,"format":"binary","url":"https://rules.example/cn.srs"}],
  "scenarios":[{"id":"domestic","name":"在国内","default_line_id":"vpn","bindings":[{"rule_set_id":"intranet","line_id":"vpn"},{"rule_set_id":"cn","line_id":"direct"}]},
   {"id":"overseas","name":"在国外","default_line_id":"direct","bindings":[{"rule_set_id":"cn","line_id":"vpn"},{"rule_set_id":"intranet","line_id":"vpn"}]}],
  "active_scenario_id":"domestic"}}`

func mustLoad(t *testing.T, text string) *Seed {
	t.Helper()
	seed, err := Load([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return seed
}

func TestLegacyDocumentRoutesLikeTheAuthorityExceptDeclaredParts(t *testing.T) {
	want := Fingerprint(mustLoad(t, authority), map[string]string{"https://rules.example/cn.srs": "abc"})
	legacy, err := LegacyProfileDocument([]byte(authority))
	if err != nil {
		t.Fatal(err)
	}
	got := Fingerprint(mustLoad(t, string(legacy)), nil)
	if d := Diff(want, got, []string{"regions", "offline_rules"}); len(d) != 0 {
		t.Fatalf("legacy document changes routing: %v", d)
	}
	if d := Diff(want, got, nil); len(d) == 0 {
		t.Fatal("undeclared differences in regions and offline rules must be reported")
	}
}

func TestDiffReportsEveryRoutingChange(t *testing.T) {
	want := Fingerprint(mustLoad(t, authority), nil)
	cases := map[string]string{
		"default exit": strings.Replace(authority, `"name":"在国内","default_line_id":"vpn"`, `"name":"在国内","default_line_id":"direct"`, 1),
		"rule priority": strings.Replace(authority,
			`"bindings":[{"rule_set_id":"intranet","line_id":"vpn"},{"rule_set_id":"cn","line_id":"direct"}]`,
			`"bindings":[{"rule_set_id":"cn","line_id":"direct"},{"rule_set_id":"intranet","line_id":"vpn"}]`, 1),
		"bound line":     strings.Replace(authority, `{"rule_set_id":"cn","line_id":"direct"}`, `{"rule_set_id":"cn","line_id":"vpn"}`, 1),
		"region mapping": strings.Replace(authority, `"cn":"domestic"`, `"cn":"overseas"`, 1),
		"rule content":   strings.Replace(authority, `"domains":["example.com"]`, `"domains":["example.org"]`, 1),
		"scenario name":  strings.Replace(authority, `"name":"在国外"`, `"name":"国外"`, 1),
	}
	for name, changed := range cases {
		got := Fingerprint(mustLoad(t, changed), nil)
		if d := Diff(want, got, nil); len(d) == 0 {
			t.Errorf("%s change not detected", name)
		}
	}
	offline := Fingerprint(mustLoad(t, authority), map[string]string{"https://rules.example/cn.srs": "other"})
	if d := Diff(Fingerprint(mustLoad(t, authority), map[string]string{"https://rules.example/cn.srs": "abc"}), offline, nil); len(d) != 1 {
		t.Errorf("offline rule digest change not reported exactly once: %v", d)
	}
}

func TestSecretsAndFormattingDoNotCount(t *testing.T) {
	want := Fingerprint(mustLoad(t, authority), nil)
	withPassword := strings.Replace(authority, `"vpn_server":"vpn.example.com:8443"`, `"vpn_server":"vpn.example.com:8443","vpn_password":"x"`, 1)
	if d := Diff(want, Fingerprint(mustLoad(t, withPassword), nil), nil); len(d) != 0 {
		t.Fatalf("a filled-in credential is not a preset difference: %v", d)
	}
	every := Fingerprint(&Seed{Profile: &config.Profile{Lines: []config.Line{{ID: "all", Type: "vpn",
		VPNUsername: "user-secret", VPNPassword: "pass-secret", TrojanPassword: "trojan-secret", SSPass: "ss-secret",
		VMessUUID: "uuid-secret", AnyTLSPassword: "anytls-secret", TailscaleAuthKey: "key-secret"}}}}, nil)
	if printed, _ := json.Marshal(every); strings.Contains(string(printed), "secret") {
		t.Fatalf("fingerprint carries credentials: %s", printed)
	}
	srs := strings.Replace(authority, `"format":"binary"`, `"format":"srs"`, 1)
	if d := Diff(want, Fingerprint(mustLoad(t, srs), nil), nil); len(d) != 0 {
		t.Fatalf("format synonyms reported as differences: %v", d)
	}
}

func TestEffectiveValuesDoNotCount(t *testing.T) {
	want := Fingerprint(mustLoad(t, authority), nil)
	// An app fills defaults: Direct fetches rule sets, and a Direct line has no protocol options.
	filled := strings.Replace(authority, `"format":"binary"`, `"format":"binary","fetch_line_id":"direct"`, 1)
	filled = strings.Replace(filled, `"type":"direct","enabled":true}`, `"type":"direct","enabled":true,"anytls_port":443}`, 1)
	filled = strings.Replace(filled, `"name":"在国外","default_line_id":"direct"`, `"name":"在国外","default_line_id":"direct","match_order":[]`, 1)
	if d := Diff(want, Fingerprint(mustLoad(t, filled), nil), nil); len(d) != 0 {
		t.Fatalf("effective defaults reported as differences: %v", d)
	}
	viaVPN := strings.Replace(authority, `"format":"binary"`, `"format":"binary","fetch_line_id":"vpn"`, 1)
	if d := Diff(want, Fingerprint(mustLoad(t, viaVPN), nil), nil); len(d) != 1 {
		t.Fatalf("fetching rules through another line must count: %v", d)
	}
}

func TestDiffReportsMatchingContentOfEveryRuleKind(t *testing.T) {
	base := func() config.RuleSet {
		return config.RuleSet{ID: "r", Name: "r", Type: "group", Enabled: true,
			NativeRule:   json.RawMessage(`{"domain_suffix":["a.example"]}`),
			Applications: []config.ApplicationMatch{{Path: "/Applications/A.app", BundleIdentifier: "com.example.a"}},
			Processes:    []string{"a"},
			Conditions:   []config.RuleSet{{ID: "c", Type: "manual", Enabled: true, Domains: []string{"c.example"}}}}
	}
	fingerprint := func(rule config.RuleSet) map[string]any {
		return Fingerprint(&Seed{Profile: &config.Profile{RuleSets: []config.RuleSet{rule}}}, nil)
	}
	want := fingerprint(base())
	reformatted := base()
	reformatted.NativeRule = json.RawMessage("{ \"domain_suffix\": [ \"a.example\" ] }")
	if d := Diff(want, fingerprint(reformatted), nil); len(d) != 0 {
		t.Fatalf("native rule formatting reported as a difference: %v", d)
	}
	changes := map[string]func(*config.RuleSet){
		"native rule": func(r *config.RuleSet) { r.NativeRule = json.RawMessage(`{"domain_suffix":["b.example"]}`) },
		"application": func(r *config.RuleSet) { r.Applications[0].BundleIdentifier = "com.example.b" },
		"process":     func(r *config.RuleSet) { r.Processes = []string{"b"} },
		"condition":   func(r *config.RuleSet) { r.Conditions[0].Domains = []string{"d.example"} },
	}
	for name, change := range changes {
		rule := base()
		change(&rule)
		if d := Diff(want, fingerprint(rule), nil); len(d) == 0 {
			t.Errorf("%s change not detected", name)
		}
	}
}

func TestLoadRequiresFieldsPlatformsDefaultDifferently(t *testing.T) {
	cases := map[string]string{
		"line enabled": strings.Replace(authority, `"type":"vpn","enabled":true,`, `"type":"vpn",`, 1),
		"rule enabled": strings.Replace(authority, `"type":"manual","enabled":true,`, `"type":"manual",`, 1),
		"trojan port": strings.Replace(authority, `{"id":"vpn","name":"公司 VPN","type":"vpn","enabled":true,"vpn_server":"vpn.example.com:8443"}`,
			`{"id":"vpn","name":"公司 VPN","type":"trojan","enabled":true,"trojan_server":"t.example.com"}`, 1),
	}
	for name, seed := range cases {
		if seed == authority {
			t.Fatalf("%s: fixture did not change", name)
		}
		if _, err := Load([]byte(seed)); err == nil || !strings.Contains(err.Error(), "platforms default it differently") {
			t.Errorf("%s omitted: got %v", name, err)
		}
	}
}
