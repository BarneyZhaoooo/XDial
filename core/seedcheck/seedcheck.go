// Package seedcheck compares a company preset ("seed") by meaning rather than
// by bytes. The private packaging repository keeps one authoritative seed; each
// platform package carries either that file or a document generated from it.
// Check proves a built package routes exactly like the authority: the same
// lines, rules in the same priority, the same default exits and bindings, the
// same region-to-Scenario map and the same offline rule files. Only
// differences a platform declares as capability limits are tolerated.
package seedcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/kafeifei/xdial/core/config"
)

// Namespace is fixed so imported identities are stable between files.
const Namespace = "seed"

// Seed is a preset loaded into the shared model.
type Seed struct {
	Profile  *config.Profile
	Regions  map[string]string // region code or "*" -> imported Scenario ID
	Revision int
	Mobile   bool
}

// Load accepts the mobile seed envelope ("xdial-mobile-seed") or a plain
// portable profile document, as both app importers do.
func Load(data []byte) (*Seed, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("seed is not JSON")
	}
	var format string
	_ = json.Unmarshal(root["format"], &format)
	if format != "xdial-mobile-seed" {
		profile, err := config.ImportProfileDocument(data, Namespace)
		if err != nil {
			return nil, fmt.Errorf("import profile document: %v", err)
		}
		return &Seed{Profile: profile}, nil
	}
	var envelope struct {
		SchemaVersion int               `json:"schema_version"`
		Revision      int               `json:"revision"`
		Regions       map[string]string `json:"region_scenarios"`
		Profile       json.RawMessage   `json:"profile"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.SchemaVersion != 1 || envelope.Revision <= 0 {
		return nil, fmt.Errorf("invalid mobile seed envelope")
	}
	var source struct {
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(envelope.Profile, &source); err != nil {
		return nil, fmt.Errorf("invalid seed profile")
	}
	profile, err := config.ImportProfileDocument(envelope.Profile, Namespace)
	if err != nil {
		return nil, fmt.Errorf("import seed profile: %v", err)
	}
	if len(source.Scenarios) != len(profile.Scenarios) {
		return nil, fmt.Errorf("seed Scenarios changed during import")
	}
	ids := map[string]string{}
	for i, scenario := range source.Scenarios {
		ids[scenario.ID] = profile.Scenarios[i].ID
	}
	regions := map[string]string{}
	for key, scenario := range envelope.Regions {
		target, ok := ids[scenario]
		if !ok {
			return nil, fmt.Errorf("region %s names unknown Scenario %s", key, scenario)
		}
		regions[strings.ToUpper(strings.TrimSpace(key))] = target
	}
	return &Seed{Profile: profile, Regions: regions, Revision: envelope.Revision, Mobile: true}, nil
}

// LegacyProfileDocument is what a desktop app without mobile-seed support
// receives: the authority's profile object, unchanged, so it imports to the
// same identities and routing.
func LegacyProfileDocument(authority []byte) ([]byte, error) {
	var envelope struct {
		Format  string          `json:"format"`
		Profile json.RawMessage `json:"profile"`
	}
	if err := json.Unmarshal(authority, &envelope); err != nil || envelope.Format != "xdial-mobile-seed" {
		return nil, fmt.Errorf("authority must be a mobile seed")
	}
	var value any
	if err := json.Unmarshal(envelope.Profile, &value); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(value, "", "  ")
	return append(out, '\n'), err
}

func normalizedFormat(format string) string {
	switch strings.ToLower(format) {
	case "srs", "binary":
		return "binary"
	case "json", "source":
		return "source"
	case "", "auto":
		return "auto"
	}
	return format
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

var secretLineFields = map[string]bool{"vpn_username": true, "vpn_password": true, "trojan_password": true,
	"ss_password": true, "vmess_uuid": true, "anytls_password": true, "tailscale_auth_key": true}

// Fingerprint is the routing meaning of a seed. Map keys name the parts a
// platform may declare as unsupported: "lines", "rule_sets", "scenarios",
// "active_scenario", "regions", "offline_rules".
func Fingerprint(seed *Seed, offline map[string]string) map[string]any {
	p := seed.Profile
	var lines []map[string]any
	for _, line := range p.Lines {
		var object map[string]any
		data, _ := json.Marshal(line)
		_ = json.Unmarshal(data, &object)
		for key, value := range object {
			// Secrets are the user's; empty values are formatting, not meaning.
			if secretLineFields[key] || strings.HasPrefix(key, "identity_") || key == "verified" || isEmpty(value) {
				delete(object, key)
			}
		}
		lines = append(lines, object)
	}
	var rules []map[string]any
	for _, rule := range p.RuleSets {
		rules = append(rules, map[string]any{
			"id": rule.ID, "name": rule.Name, "type": string(rule.Type), "enabled": rule.Enabled,
			"url": rule.URL, "format": normalizedFormat(rule.Format), "fetch_line_id": rule.EffectiveFetchLineID(),
			"domains": sorted(rule.Domains), "cidrs": sorted(rule.CIDRs), "invert": rule.Invert,
			"no_resolve": rule.NoResolve,
		})
	}
	var scenarios []map[string]any
	for _, scenario := range p.Scenarios {
		var bindings []map[string]any
		for _, binding := range scenario.Bindings {
			bindings = append(bindings, map[string]any{"rule_set_id": binding.RuleSetID, "line_id": binding.LineID,
				"conditions": sorted(binding.ConditionIDs)})
		}
		scenarios = append(scenarios, map[string]any{
			"id": scenario.ID, "name": scenario.Name, "default_line_id": scenario.DefaultLineID,
			"match_ssids": sorted(scenario.MatchSSIDs), "bindings": bindings,
			"match_order": append([]string{}, scenario.MatchOrder...),
		})
	}
	regions := map[string]any{}
	for key, value := range seed.Regions {
		regions[key] = value
	}
	offlineRules := map[string]any{}
	for key, value := range offline {
		offlineRules[key] = value
	}
	return canonical(map[string]any{"lines": lines, "rule_sets": rules, "scenarios": scenarios,
		"active_scenario": p.ActiveScenarioID, "regions": regions, "offline_rules": offlineRules})
}

func canonical(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// OfflineRules reads a RuleSets directory (manifest.json mapping rule URLs to
// files) into URL -> SHA-256 of the file. A missing directory is empty.
func OfflineRules(directory string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid rule manifest")
	}
	out := map[string]string{}
	for url, name := range manifest {
		if name != filepath.Base(name) {
			return nil, fmt.Errorf("rule manifest names a path: %s", name)
		}
		file, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("offline rule %s is missing", name)
		}
		sum := sha256.Sum256(file)
		out[url] = hex.EncodeToString(sum[:])
	}
	return out, nil
}

// Diff lists every difference between two fingerprints, except for parts the
// platform declared as unsupported. Paths read like "scenarios[1].bindings".
func Diff(authority, packaged map[string]any, allowed []string) []string {
	skip := map[string]bool{}
	for _, part := range allowed {
		skip[part] = true
	}
	var out []string
	keys := make([]string, 0, len(authority))
	for key := range authority {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if skip[key] {
			continue
		}
		out = append(out, diffValue(key, authority[key], packaged[key])...)
	}
	return out
}

func diffValue(path string, a, b any) []string {
	if reflect.DeepEqual(a, b) {
		return nil
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{path + ": " + describe(a) + " -> " + describe(b)}
		}
		keys := map[string]bool{}
		for key := range av {
			keys[key] = true
		}
		for key := range bv {
			keys[key] = true
		}
		names := make([]string, 0, len(keys))
		for key := range keys {
			names = append(names, key)
		}
		sort.Strings(names)
		var out []string
		for _, key := range names {
			out = append(out, diffValue(path+"."+key, av[key], bv[key])...)
		}
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return []string{fmt.Sprintf("%s: %d items -> %s", path, len(av), describe(b))}
		}
		var out []string
		for i := range av {
			out = append(out, diffValue(fmt.Sprintf("%s[%d]", path, i), av[i], bv[i])...)
		}
		return out
	}
	return []string{path + ": " + describe(a) + " -> " + describe(b)}
}

func describe(value any) string {
	if value == nil {
		return "missing"
	}
	if list, ok := value.([]any); ok {
		return fmt.Sprintf("%d items", len(list))
	}
	data, _ := json.Marshal(value)
	if len(data) > 80 {
		return string(data[:77]) + "..."
	}
	return string(data)
}

func isEmpty(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}
