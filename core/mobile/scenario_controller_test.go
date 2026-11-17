package mobile

import (
	"encoding/json"
	"testing"
)

const testProfile = `{"extension_metadata":{"keep":true},"lines":[{"id":"direct","type":"direct","enabled":true}],"scenarios":[{"id":"office","match_ssids":["Office"," Office-Guest "],"default_line_id":"direct"},{"id":"home","default_line_id":"direct"},{"id":"abroad","default_line_id":"direct"}],"active_scenario_id":"office"}`

func controller(t *testing.T) *ScenarioController {
	t.Helper()
	c, err := NewScenarioController(testProfile, `{"JP":"home","SG":"missing","*":"abroad"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestPickerMatchesAppleBehavior(t *testing.T) {
	c := controller(t)
	for _, tc := range []struct{ ssid, country, want, reason string }{
		{"Office", "JP", "office", "wifi"}, {" Office-Guest ", "NZ", "office", "wifi"},
		{"office", "JP", "home", "country"}, {"Home", "nz", "abroad", "country"},
		{"", "SG", "", ""}, {"", "日本", "", ""}, {"", "", "", ""}, {"Home", "", "", ""},
	} {
		p := pickScenario(c.scenarios, c.regions, tc.ssid, tc.country)
		if tc.want == "" {
			if p != nil {
				t.Fatalf("unexpected pick %+v", p)
			}
			continue
		}
		if p == nil || p.ScenarioID != tc.want || p.Reason != tc.reason {
			t.Fatalf("%+v got %+v", tc, p)
		}
	}
}
func TestManualOverrideDiscardsLateSurveyAndFailedCandidate(t *testing.T) {
	c := controller(t)
	ticket := c.BeginSurvey()
	if err := c.SetConnected(true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SelectScenario("home"); err != nil {
		t.Fatal(err)
	}
	token := c.state.Pending.Token
	if c.state.Selected != "office" || c.state.Active != "office" || c.state.Automatic {
		t.Fatal("proposal changed active state")
	}
	if _, err := c.CompleteSurvey(ticket, "", "NZ", 1000); err != nil {
		t.Fatal(err)
	}
	if c.state.Pending.ScenarioID != "home" {
		t.Fatal("late survey overwrote user choice")
	}
	candidate, err := c.ProfileForProposal(token)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	json.Unmarshal([]byte(candidate), &document)
	if document["active_scenario_id"] != "home" || document["extension_metadata"] == nil {
		t.Fatal("projection lost data")
	}
	if err := c.Abort(token); err != nil {
		t.Fatal(err)
	}
	if c.state.Selected != "office" || c.state.Active != "office" {
		t.Fatal("failed candidate replaced active session")
	}
	if err := c.Commit(token, 1000); err == nil {
		t.Fatal("stale commit accepted")
	}
}
func TestAutomaticBounceGuardOnlyRecordsSuccessfulCommits(t *testing.T) {
	c := controller(t)
	c.SetConnected(true)
	c.CompleteSurvey(c.BeginSurvey(), "", "JP", 1000)
	c.Abort(c.state.Pending.Token)
	c.CompleteSurvey(c.BeginSurvey(), "", "JP", 1001)
	c.Commit(c.state.Pending.Token, 1001)
	c.CompleteSurvey(c.BeginSurvey(), "Office", "", 61001)
	if c.state.Pending != nil {
		t.Fatal("automatic bounce allowed")
	}
	c.CompleteSurvey(c.BeginSurvey(), "", "NZ", 61002)
	if c.state.Pending == nil || c.state.Pending.ScenarioID != "abroad" {
		t.Fatal("third scenario incorrectly suppressed")
	}
	c.Abort(c.state.Pending.Token)
	c.CompleteSurvey(c.BeginSurvey(), "Office", "", 602002)
	if c.state.Pending == nil || c.state.Pending.ScenarioID != "office" {
		t.Fatal("expired bounce guard persisted")
	}
}
func TestSurveyTicketsAndUnknownTargets(t *testing.T) {
	c := controller(t)
	old := c.BeginSurvey()
	fresh := c.BeginSurvey()
	c.CompleteSurvey(old, "", "JP", 0)
	if c.state.Pending != nil {
		t.Fatal("obsolete survey accepted")
	}
	c.CompleteSurvey(fresh, "", "NZ", 0)
	token := c.state.Pending.Token
	if _, err := c.SelectScenario("home"); err == nil {
		t.Fatal("concurrent transaction accepted")
	}
	c.Abort(token)
	if _, err := c.SelectScenario("missing"); err == nil || !c.state.Automatic {
		t.Fatal("invalid manual target mutated preference")
	}
	if _, err := c.CompleteSurvey(fresh, "", "JP", 0); err != nil || c.state.Pending != nil {
		t.Fatal("consumed survey accepted")
	}
}

func TestIncompleteSelectedScenarioDoesNotBlockPicker(t *testing.T) {
	raw := `{"lines":[{"id":"direct","type":"direct","enabled":true},{"id":"empty","type":"selector","enabled":true}],"scenarios":[{"id":"bad","default_line_id":"empty"},{"id":"good","default_line_id":"direct"}],"active_scenario_id":"bad"}`
	c, err := NewScenarioController(raw, "{}", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.SelectScenario("good"); err != nil {
		t.Fatal(err)
	}
}
