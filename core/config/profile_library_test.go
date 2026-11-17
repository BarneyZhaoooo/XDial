package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const libraryLegacy = `{"profile_id":"legacy","lines":[{"id":"direct","type":"direct","enabled":true},{"id":"vpn","type":"vpn","enabled":true,"vpn_server":"vpn.invalid","vpn_username":"fixture","vpn_password":"test-only","custom_line":{"preserve":true}}],"rule_sets":[],"scenarios":[{"id":"work","name":"Work","default_line_id":"vpn","custom_scene":7}],"active_scenario_id":"work","mobile_automatic":false,"custom_root":"preserved"}`
const libraryIncoming = `{"lines":[{"id":"direct","type":"direct","enabled":true},{"id":"remote","type":"anytls","enabled":true,"anytls_server":"proxy.invalid","anytls_port":443,"anytls_password":"fixture"}],"rule_sets":[{"id":"rule","name":"Example","type":"manual","enabled":true,"domains":["old.invalid"]}],"scenarios":[{"id":"scene","name":"Imported","default_line_id":"remote","bindings":[{"rule_set_id":"rule","line_id":"remote"}]}],"active_scenario_id":"scene"}`

func testLibraryImport(t *testing.T, raw, source string, updating bool) string {
	t.Helper()
	p, err := ImportProfileDocument([]byte(source), "imported")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(p)
	preview, err := PreviewProfileLibraryImport(raw, string(data), "Imported", "imported", updating)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}
func TestProfileLibraryMigrationPreservesUnknownFieldsAndIdentity(t *testing.T) {
	raw, err := NewProfileLibrary(libraryLegacy)
	if err != nil {
		t.Fatal(err)
	}
	l, err := decodeLibrary(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Profiles) != 1 || l.Profiles[0].ID != "legacy" || len(libraryObjects(l.Profiles[0].Profile, "scenarios")) != 0 || len(l.Scenarios) != 1 {
		t.Fatal("ownership not promoted")
	}
	snapshot, err := ProfileLibraryRuntime(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"custom_line":{"preserve":true}`, `"custom_scene":7`, `"custom_root":"preserved"`, `"mobile_automatic":false`, `"identity_profile_id":"legacy"`} {
		if !strings.Contains(snapshot, fragment) {
			t.Fatalf("lost %s", fragment)
		}
	}
	draft, _ := libraryProfile(snapshot)
	draft["active_scenario_id"] = libraryJSON("work")
	draft["mobile_automatic"] = libraryJSON(true)
	updated, err := ApplyProfileLibraryRuntime(raw, string(libraryJSON(draft)))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := ProfileLibraryRuntime(updated)
	if !strings.Contains(after, `"custom_line":{"preserve":true}`) || !strings.Contains(after, `"mobile_automatic":true`) {
		t.Fatal("editor lost metadata")
	}
}
func TestProfileLibraryPreviewIsAtomicAndDoesNotSelectImportedScene(t *testing.T) {
	raw, err := NewProfileLibrary(libraryLegacy)
	if err != nil {
		t.Fatal(err)
	}
	preview := testLibraryImport(t, raw, libraryIncoming, false)
	committed, err := CommitProfileLibraryPreview(raw, preview)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := decodeLibrary(committed)
	if l.ActiveScenarioID != "work" || len(l.Profiles) != 2 || len(l.Scenarios) != 2 || l.EditingProfileID != "imported" {
		t.Fatal("import changed active choice or lost a source")
	}
	if len(libraryObjects(l.Profiles[1].Profile, "scenarios")) != 0 || l.Profiles[1].Baseline == nil {
		t.Fatal("source owns global scenes or lost baseline")
	}
	if _, err = CommitProfileLibraryPreview(committed, preview); err == nil {
		t.Fatal("stale preview overwrote newer library")
	}
	summary, err := ProfileLibrarySummary(committed)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"test-only", "vpn.invalid", "proxy.invalid", "fixture"} {
		if strings.Contains(summary, secret) {
			t.Fatal("summary exposed credentials or endpoint")
		}
	}
	before, _ := decodeLibrary(raw)
	if len(before.Profiles) != 1 {
		t.Fatal("preview mutated its input")
	}
}
func TestProfileLibraryRefreshKeepsGlobalChoicesAndRejectsLostDependencies(t *testing.T) {
	raw, _ := NewProfileLibrary(libraryLegacy)
	raw, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, libraryIncoming, false))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := ProfileLibraryRuntime(raw)
	draft, _ := libraryProfile(snapshot)
	scenes := libraryObjects(draft, "scenarios")
	scene := scenes[1]
	scene["name"] = libraryJSON("My name")
	scene["default_line_id"] = libraryJSON("direct")
	scene["match_ssids"] = libraryJSON([]string{"isolated-fixture"})
	draft["scenarios"] = libraryJSON(scenes)
	raw, err = ApplyProfileLibraryRuntime(raw, string(libraryJSON(draft)))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, strings.Replace(libraryIncoming, "old.invalid", "new.invalid", 1), true))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := decodeLibrary(updated)
	if string(libraryJSON(l.Scenarios[1])) != string(libraryJSON(scene)) {
		t.Fatal("source refresh rewrote global scenario")
	}
	runtime, _ := ProfileLibraryRuntime(updated)
	if !strings.Contains(runtime, "new.invalid") {
		t.Fatal("source content did not update")
	}
	missing := strings.Replace(libraryIncoming, `{"id":"remote","type":"anytls","enabled":true,"anytls_server":"proxy.invalid","anytls_port":443,"anytls_password":"fixture"}`, `{"id":"replacement","type":"anytls","enabled":true}`, 1)
	missing = strings.ReplaceAll(missing, `"default_line_id":"remote"`, `"default_line_id":"replacement"`)
	missing = strings.ReplaceAll(missing, `"line_id":"remote"`, `"line_id":"replacement"`)
	p, err := ImportProfileDocument([]byte(missing), "imported")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PreviewProfileLibraryImport(updated, string(libraryJSON(p)), "Imported", "imported", true); err == nil {
		t.Fatal("removed global dependency accepted")
	}
}
func TestProfileLibraryEmptyGroupIsStoredButNotRunnable(t *testing.T) {
	raw, _ := NewProfileLibrary("")
	l, _ := decodeLibrary(raw)
	l.Groups = []libraryObject{{"id": libraryJSON("empty"), "type": libraryJSON("selector"), "enabled": libraryJSON(true), "group_members": libraryJSON([]string{})}}
	l.Scenarios[0]["default_line_id"] = libraryJSON("empty")
	raw, err := encodeLibrary(l)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := ProfileLibraryRuntime(raw)
	p, _ := ParseRuntimeProfile([]byte(runtime))
	if _, err = BuildConnectionPlan(p); err == nil {
		t.Fatal("empty draft group became runnable")
	}
}
func TestProfileLibraryRejectsFutureSchemasAndSSIDConflicts(t *testing.T) {
	if _, err := NewProfileLibrary(`{"lines":[{"id":"direct","type":"vpn","enabled":true}],"scenarios":[{"id":"s","default_line_id":"direct"}],"active_scenario_id":"s"}`); err == nil {
		t.Fatal("reserved identity silently changed VPN into Direct")
	}
	raw, _ := NewProfileLibrary(libraryLegacy)
	if _, err := ProfileLibraryRuntime(strings.Replace(raw, `"schemaVersion":3`, `"schemaVersion":4`, 1)); err == nil {
		t.Fatal("future schema accepted")
	}
	l, _ := decodeLibrary(raw)
	l.Scenarios[0]["match_ssids"] = libraryJSON([]string{"same"})
	second := copyLibraryObject(l.Scenarios[0])
	second["id"] = libraryJSON("second")
	l.Scenarios = append(l.Scenarios, second)
	if _, err := encodeLibrary(l); err == nil {
		t.Fatal("SSID conflict accepted")
	}
}

func TestProfileLibraryMigrationKeepsExactLegacyIdentityNamespace(t *testing.T) {
	raw, err := NewProfileLibrary(strings.Replace(libraryLegacy, `"profile_id":"legacy",`, "", 1))
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	old := TailscaleStateDirectory(base)
	next := TailscaleStateDirectory(filepath.Join(base, "profiles", "default"))
	got, err := ResolveProfileLibraryIdentityNamespace(raw, base, next)
	if err != nil || got != old {
		t.Fatal("legacy identity was orphaned")
	}
	other := TailscaleStateDirectory(filepath.Join(base, "profiles", "other"))
	got, err = ResolveProfileLibraryIdentityNamespace(raw, base, other)
	if err != nil || got != other {
		t.Fatal("unrelated source inherited legacy identity")
	}
	fresh, _ := NewProfileLibrary("")
	got, err = ResolveProfileLibraryIdentityNamespace(fresh, base, next)
	if err != nil || got != next {
		t.Fatal("fresh library adopted old state")
	}
	named, _ := NewProfileLibrary(libraryLegacy)
	got, err = ResolveProfileLibraryIdentityNamespace(named, base, next)
	if err != nil || got != next {
		t.Fatal("named identity changed namespace")
	}
}

func TestProfileLibraryRemovalPreservesGlobalScenesAndRejectsReferences(t *testing.T) {
	raw, _ := NewProfileLibrary(libraryLegacy)
	if _, err := RemoveProfileLibrarySource(raw, "legacy"); err == nil {
		t.Fatal("removed only source")
	}
	raw, _ = CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, libraryIncoming, false))
	if _, err := RemoveProfileLibrarySource(raw, "imported"); err == nil {
		t.Fatal("removed referenced source")
	}
	direct := `{"lines":[{"id":"direct","type":"direct","enabled":true}],"scenarios":[{"id":"s","default_line_id":"direct"}],"active_scenario_id":"s"}`
	base, _ := NewProfileLibrary("")
	base, _ = CommitProfileLibraryPreview(base, testLibraryImport(t, base, direct, false))
	removed, err := RemoveProfileLibrarySource(base, "imported")
	if err != nil {
		t.Fatal(err)
	}
	l, _ := decodeLibrary(removed)
	if len(l.Profiles) != 1 || len(l.Scenarios) != 2 {
		t.Fatal("removal cascaded into global scene")
	}
}

func TestProfileLibraryRefreshFollowingGroupPinsImplicitChoice(t *testing.T) {
	raw, _ := NewProfileLibrary("")
	p, _ := libraryProfile(libraryIncoming)
	lines := libraryObjects(p, "lines")
	lines = append(lines, libraryObject{"id": libraryJSON("group"), "type": libraryJSON("selector"), "enabled": libraryJSON(true), "group_members": libraryJSON([]string{"remote", "direct"})})
	p["lines"] = libraryJSON(lines)
	raw, _ = CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, string(libraryJSON(p)), false))
	l, err := decodeLibrary(raw)
	if err != nil {
		t.Fatal(err)
	}
	id := libraryString(l.Groups[0], "id")
	source := l.GroupSources[id]
	source.FollowsMembers = true
	l.GroupSources[id] = source
	raw, err = encodeLibrary(l)
	if err != nil {
		t.Fatal(err)
	}
	lines[len(lines)-1]["group_members"] = libraryJSON([]string{"direct", "remote"})
	p["lines"] = libraryJSON(lines)
	updated, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, string(libraryJSON(p)), true))
	if err != nil {
		t.Fatal(err)
	}
	l, _ = decodeLibrary(updated)
	if libraryString(l.Groups[0], "group_default") != documentID("imported", "line", "remote") {
		t.Fatal("source reorder silently changed exit")
	}
	lines[len(lines)-1]["group_members"] = libraryJSON([]string{"direct"})
	p["lines"] = libraryJSON(lines)
	next, err := ImportProfileDocument(libraryJSON(p), "imported")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PreviewProfileLibraryImport(updated, string(libraryJSON(next)), "Imported", "imported", true); err == nil {
		t.Fatal("lost fixed group member accepted")
	}
}

func TestProfileLibraryRejectsRefreshRemovingPartialRuleScope(t *testing.T) {
	source := `{"lines":[{"id":"direct","type":"direct","enabled":true}],"rule_sets":[{"id":"group","type":"group","enabled":true,"conditions":[{"id":"a","type":"manual","enabled":true,"domains":["a.invalid"]},{"id":"b","type":"manual","enabled":true,"domains":["b.invalid"]}]}],"scenarios":[{"id":"s","default_line_id":"direct","bindings":[{"rule_set_id":"group","line_id":"direct","condition_ids":["a"]}],"match_order":["a"]}],"active_scenario_id":"s"}`
	raw, _ := NewProfileLibrary("")
	raw, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, source, false))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := libraryProfile(source)
	rules := libraryObjects(p, "rule_sets")
	rules[0]["conditions"] = libraryJSON(libraryObjects(rules[0], "conditions")[1:])
	p["rule_sets"] = libraryJSON(rules)
	// Incoming templates may change, but the global partial scope must survive.
	p["scenarios"] = libraryJSON([]libraryObject{})
	p["active_scenario_id"] = libraryJSON("")
	next, err := ImportProfileDocument(libraryJSON(p), "imported")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PreviewProfileLibraryImport(raw, string(libraryJSON(next)), "Imported", "imported", true); err == nil {
		t.Fatal("lost partial matching scope accepted")
	}
}

func TestProfileLibraryFileRefreshKeepsLocalCredentialsAtSameServer(t *testing.T) {
	raw, _ := NewProfileLibrary("")
	raw, _ = CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, libraryIncoming, false))
	runtime, _ := ProfileLibraryRuntime(raw)
	draft, _ := libraryProfile(runtime)
	lines := libraryObjects(draft, "lines")
	for _, line := range lines {
		if libraryString(line, "type") == "anytls" {
			line["anytls_password"] = libraryJSON("local-fixture")
		}
	}
	draft["lines"] = libraryJSON(lines)
	raw, err := ApplyProfileLibraryRuntime(raw, string(libraryJSON(draft)))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, libraryIncoming, true))
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ = ProfileLibraryRuntime(updated)
	if !strings.Contains(runtime, "local-fixture") {
		t.Fatal("local credentials overwritten")
	}
	p, err := ImportProfileDocument([]byte(strings.Replace(libraryIncoming, "proxy.invalid", "changed.invalid", 1)), "imported")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PreviewProfileLibraryImport(updated, string(libraryJSON(p)), "Imported", "imported", true); err == nil {
		t.Fatal("local credentials moved to another endpoint")
	}
}

func TestProfileLibraryRejectsStaleControllerWriteAfterImport(t *testing.T) {
	raw, _ := NewProfileLibrary(libraryLegacy)
	old, _ := ProfileLibraryRuntime(raw)
	updated, err := CommitProfileLibraryPreview(raw, testLibraryImport(t, raw, libraryIncoming, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyProfileLibraryRuntime(updated, old); err == nil {
		t.Fatal("stale controller deleted newly imported resources")
	}
	if _, err = ApplyProfileLibrarySelection(updated, old); err == nil {
		t.Fatal("old controller overwrote imported resources")
	}
	draft, _ := libraryProfile(old)
	delete(draft, "library_revision")
	if _, err = ApplyProfileLibraryRuntime(raw, string(libraryJSON(draft))); err == nil {
		t.Fatal("unversioned editor accepted")
	}
}

func TestProfileLibrarySelectionAllowsRepeatedControllerCommits(t *testing.T) {
	raw, _ := NewProfileLibrary(libraryLegacy)
	runtime, _ := ProfileLibraryRuntime(raw)
	draft, _ := libraryProfile(runtime)
	draft["mobile_automatic"] = libraryJSON(true)
	updated, err := ApplyProfileLibrarySelection(raw, string(libraryJSON(draft)))
	if err != nil {
		t.Fatal(err)
	}
	// Controller still holds its initial revision, but only changes its own preference.
	draft["mobile_automatic"] = libraryJSON(false)
	updated, err = ApplyProfileLibrarySelection(updated, string(libraryJSON(draft)))
	if err != nil {
		t.Fatal(err)
	}
	result, _ := ProfileLibraryRuntime(updated)
	if !strings.Contains(result, `"mobile_automatic":false`) {
		t.Fatal("second controller commit lost")
	}
}
