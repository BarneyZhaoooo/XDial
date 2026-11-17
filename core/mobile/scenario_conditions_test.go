package mobile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kafeifei/xdial/core/config"
)

func TestConditionsPreserveLibraryAndGlobalOwnership(t *testing.T) {
	library, err := config.NewProfileLibrary(testProfile)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.ProfileLibraryRuntime(library)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := UpdateScenarioConditions(runtime, "home", " Home \nGuest\nHome", "nz，JP, *")
	if err != nil {
		t.Fatal(err)
	}
	library, err = config.ApplyProfileLibraryRuntime(library, updated)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _ := config.ProfileLibraryRuntime(library)
	var root map[string]json.RawMessage
	json.Unmarshal([]byte(reloaded), &root)
	if string(root["active_scenario_id"]) != `"office"` || root["extension_metadata"] == nil {
		t.Fatal("editor changed selection or metadata")
	}
	conditions, _ := ScenarioConditions(reloaded, "home")
	if !strings.Contains(conditions, `"countries":["*","JP","NZ"]`) || !strings.Contains(conditions, `"ssids":["Home","Guest"]`) {
		t.Fatal(conditions)
	}
	// Other scenes and bindings survive, and same-revision stale edits cannot
	// overwrite a second editor's committed conditions.
	if _, err = config.ApplyProfileLibraryRuntime(library, runtime); err == nil {
		t.Fatal("stale editor overwrote conditions")
	}
	cleared, err := UpdateScenarioConditions(reloaded, "home", "", "")
	if err != nil {
		t.Fatal(err)
	}
	conditions, _ = ScenarioConditions(cleared, "office")
	if !strings.Contains(conditions, "Office-Guest") {
		t.Fatal("other scene changed")
	}
	conditions, _ = ScenarioConditions(cleared, "home")
	if conditions != `{"countries":[],"ssids":[]}` {
		t.Fatal(conditions)
	}
}

func TestConditionsRefuseAmbiguousOrInvalidChanges(t *testing.T) {
	first, err := UpdateScenarioConditions(testProfile, "home", "Home", "JP")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, ssid, country string }{
		{"", "", ""},
		{"abroad", "Home", ""}, {"abroad", "", "JP"}, {"missing", "", ""},
		{"home", strings.Repeat("a", 33), ""}, {"home", "", "JPN"}, {"home", "", "XX"}, {"home", "", "T1"},
	} {
		if _, err := UpdateScenarioConditions(first, tc.id, tc.ssid, tc.country); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestConditionsNullSSIDIsAnEmptyPresentationList(t *testing.T) {
	raw := strings.Replace(testProfile, `"match_ssids":["Office"," Office-Guest "]`, `"match_ssids":null`, 1)
	value, err := ScenarioConditions(raw, "office")
	if err != nil || value != `{"countries":[],"ssids":[]}` {
		t.Fatalf("%s %v", value, err)
	}
}

func TestSurveyDoesNotQueryForSSIDOrManualOverride(t *testing.T) {
	c := controller(t)
	if c.NeedsCountrySurvey("Office") || !c.NeedsCountrySurvey("unknown") {
		t.Fatal("wrong observation policy")
	}
	ticket := c.BeginSurvey()
	c.SelectScenario("home")
	if c.NeedsCountrySurvey("") {
		t.Fatal("manual override allowed IO")
	}
	c.CompleteSurvey(ticket, "", "NZ", 100)
	if c.state.Pending.ScenarioID != "home" {
		t.Fatal("late observation replaced manual intent")
	}
	c.Abort(c.state.Pending.Token)
	c.SetAutomatic(true)
	c.regions = map[string]string{}
	if c.NeedsCountrySurvey("") {
		t.Fatal("empty mapping allowed IO")
	}
}

func TestReaffirmingSelectedScenarioCancelsAutomaticSurvey(t *testing.T) {
	c := controller(t)
	ticket := c.BeginSurvey()
	c.SelectScenario("office")
	c.CompleteSurvey(ticket, "", "JP", 100)
	if c.state.Automatic || c.state.Pending != nil || c.state.Selected != "office" {
		t.Fatal("same-scene manual intent lost")
	}
}
