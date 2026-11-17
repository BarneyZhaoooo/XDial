package mobile

import (
	"encoding/json"
	"github.com/kafeifei/xdial/core/config"
	"reflect"
	"strings"
	"testing"
)

const editorFixture = `{"lines":[{"id":"direct","name":"Direct","type":"direct","enabled":true},{"id":"other","type":"direct","enabled":true},{"id":"off","type":"direct","enabled":false}],"rule_sets":[{"id":"group","name":"Group","type":"group","enabled":true,"conditions":[{"id":"a","type":"manual","enabled":true,"domains":["secret.invalid"]},{"id":"b","type":"manual","enabled":true,"domains":["b.invalid"]}]},{"id":"second","type":"manual","enabled":true,"domains":["second.invalid"]}],"scenarios":[{"id":"one","name":"One","default_line_id":"direct","bindings":[]},{"id":"edit","name":"Edit","default_line_id":"direct","extra":{"keep":true},"bindings":[{"rule_set_id":"group","line_id":"direct","condition_ids":["a"],"extension":42},{"rule_set_id":"second","line_id":"direct"}],"match_order":["second","a"]}],"active_scenario_id":"one","extension_metadata":{"keep":true}}`

func editOK(t *testing.T, raw, command string) string {
	t.Helper()
	out, err := EditScenario(raw, command)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func editProfile(t *testing.T, raw string) *config.Profile {
	t.Helper()
	p, err := config.ParseProfile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestScenarioEditorPreservesPartialAndExplicitOrdering(t *testing.T) {
	changed := editOK(t, editorFixture, `{"action":"bind","id":"edit","rule_id":"group","line_id":"other"}`)
	p := editProfile(t, changed)
	b := p.Scenarios[1].Bindings[0]
	if b.LineID != "other" || !reflect.DeepEqual(b.ConditionIDs, []string{"a"}) || !strings.Contains(changed, `"extension":42`) {
		t.Fatal("target edit broadened partial rule or lost metadata")
	}
	if _, err := EditScenario(changed, `{"action":"move","id":"edit","rule_id":"group","offset":1}`); err == nil {
		t.Fatal("silently changed source order")
	}
	list := editOK(t, changed, `{"action":"list_order","id":"edit"}`)
	moved := editOK(t, list, `{"action":"move","id":"edit","rule_id":"group","offset":1}`)
	p = editProfile(t, moved)
	if p.Scenarios[1].Bindings[1].RuleSetID != "group" || len(p.Scenarios[1].Bindings[1].ConditionIDs) != 1 {
		t.Fatal("move changed scope")
	}
	full := editOK(t, moved, `{"action":"full_rule","id":"edit","rule_id":"group"}`)
	if len(editProfile(t, full).Scenarios[1].Bindings[1].ConditionIDs) != 0 {
		t.Fatal("explicit full scope failed")
	}
	removed := editOK(t, changed, `{"action":"unbind","id":"edit","rule_id":"group"}`)
	if !reflect.DeepEqual(editProfile(t, removed).Scenarios[1].MatchOrder, []string{"second"}) {
		t.Fatal("unbind removed unrelated order")
	}
}
func TestScenarioEditorLibraryCASAndResourceOwnership(t *testing.T) {
	changedDefault := editOK(t, editorFixture, `{"action":"default","id":"edit","line_id":"other"}`)
	if editProfile(t, changedDefault).Scenarios[1].DefaultLineID != "other" {
		t.Fatal("default exit was not changed")
	}
	library, err := config.NewProfileLibrary(editorFixture)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := config.ProfileLibraryRuntime(library)
	if err != nil {
		t.Fatal(err)
	}
	created := editOK(t, raw, `{"action":"create","name":"Travel","line_id":"direct"}`)
	p := editProfile(t, created)
	id := p.Scenarios[len(p.Scenarios)-1].ID
	renamed := editOK(t, created, `{"action":"rename","id":"`+id+`","name":"Journey"}`)
	updated, err := config.ApplyProfileLibraryRuntime(library, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.ApplyProfileLibraryRuntime(updated, raw); err == nil {
		t.Fatal("stale editor overwrote library")
	}
	reloaded, err := config.ProfileLibraryRuntime(updated)
	if err != nil {
		t.Fatal(err)
	}
	before, after := editProfile(t, raw), editProfile(t, reloaded)
	if after.ActiveScenarioID != before.ActiveScenarioID || !reflect.DeepEqual(before.Lines, after.Lines) || !reflect.DeepEqual(before.RuleSets, after.RuleSets) || !strings.Contains(reloaded, "extension_metadata") {
		t.Fatal("scene edit changed resources or selection")
	}
	deleted := editOK(t, reloaded, `{"action":"delete","id":"`+id+`"}`)
	if _, err = config.ApplyProfileLibraryRuntime(updated, deleted); err != nil {
		t.Fatal(err)
	}
}
func TestScenarioEditorRejectsInvalidTargetsAndDeletionReferences(t *testing.T) {
	for _, command := range []string{
		`{"action":"delete","id":"one"}`, `{"action":"rename","id":"edit","name":" "}`,
		`{"action":"default","id":"edit","line_id":"off"}`, `{"action":"default","id":"edit","line_id":"missing"}`,
		`{"action":"bind","id":"edit","rule_id":"missing","line_id":"direct"}`,
		`{"action":"rename","id":"edit","name":"Name","unknown":true}`,
		`{"action":"rename","id":"edit","name":"Name"}{}`} {
		if _, err := EditScenario(editorFixture, command); err == nil {
			t.Fatalf("accepted %s", command)
		}
	}
	automatic, err := UpdateScenarioConditions(editorFixture, "edit", "Test WiFi", "NZ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = EditScenario(automatic, `{"action":"delete","id":"edit"}`); err == nil {
		t.Fatal("deleted auto referenced scene")
	}
	cleared, err := UpdateScenarioConditions(automatic, "edit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	editOK(t, cleared, `{"action":"delete","id":"edit"}`)
	disabled := strings.Replace(editorFixture, `"name":"Direct","type":"direct","enabled":true`, `"name":"Direct","type":"direct","enabled":false`, 1)
	if _, err = EditScenario(disabled, `{"action":"default","id":"edit","line_id":"direct"}`); err == nil {
		t.Fatal("explicit disabled direct accepted")
	}
}
func TestScenarioEditorPresentationHasNoResourceContents(t *testing.T) {
	view, err := ScenarioEditor(editorFixture, "edit")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(view, "invalid") || strings.Contains(view, "extension_metadata") {
		t.Fatal("presentation exposed resource contents")
	}
	var data map[string]any
	if json.Unmarshal([]byte(view), &data) != nil {
		t.Fatal(view)
	}
	raw := strings.Replace(editorFixture, `{"id":"direct","name":"Direct","type":"direct","enabled":true},`, "", 1)
	view, err = ScenarioEditor(raw, "")
	if err != nil || !strings.Contains(view, `"id":"direct"`) {
		t.Fatal("missing builtin Direct option", err)
	}
}

func TestScenarioEditorEmptyLibraryAndUnusedGroup(t *testing.T) {
	library, err := config.NewProfileLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := config.ProfileLibraryRuntime(library)
	if err != nil {
		t.Fatal(err)
	}
	first := editOK(t, raw, `{"action":"create","name":"First","line_id":"direct"}`)
	p := editProfile(t, first)
	if p.ActiveScenarioID != p.Scenarios[0].ID {
		t.Fatal("creation changed default library selection")
	}
	second := editOK(t, first, `{"action":"create","name":"Second","line_id":"direct"}`)
	p = editProfile(t, second)
	if p.Scenarios[len(p.Scenarios)-2].ID == p.Scenarios[len(p.Scenarios)-1].ID {
		t.Fatal("reused scene identity")
	}
	if _, err = config.ApplyProfileLibraryRuntime(library, second); err != nil {
		t.Fatal(err)
	}
	unused := strings.Replace(editorFixture, `"lines":[`, `"lines":[{"id":"empty","type":"selector","enabled":true,"group_members":[]},`, 1)
	editOK(t, unused, `{"action":"rename","id":"edit","name":"Changed"}`)
}
