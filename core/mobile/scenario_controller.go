package mobile

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/kafeifei/xdial/core/config"
)

type scenarioPick struct {
	ScenarioID string `json:"scenario_id"`
	Reason     string `json:"reason"`
}

// PickScenario is the shared, side-effect-free mobile policy. SSIDs are case
// sensitive, trimmed, and take priority over country. Missing targets do not
// cause fallback to another scenario. No network queries occur here.
func PickScenario(scenariosJSON, regionsJSON, ssid, country string) (string, error) {
	var scenarios []config.Scenario
	var regions map[string]string
	if err := json.Unmarshal([]byte(scenariosJSON), &scenarios); err != nil {
		return "", fmt.Errorf("invalid scenario list")
	}
	if err := json.Unmarshal([]byte(regionsJSON), &regions); err != nil {
		return "", fmt.Errorf("invalid region mapping")
	}
	result := pickScenario(scenarios, regions, ssid, country)
	data, err := json.Marshal(result)
	return string(data), err
}

func pickScenario(scenarios []config.Scenario, regions map[string]string, ssid, country string) *scenarioPick {
	ssid = strings.TrimSpace(ssid)
	if ssid != "" {
		for _, scenario := range scenarios {
			for _, name := range scenario.MatchSSIDs {
				if strings.TrimSpace(name) == ssid {
					return &scenarioPick{scenario.ID, "wifi"}
				}
			}
		}
	}
	country = strings.ToUpper(country)
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return nil
	}
	target, exists := regions[country]
	if !exists {
		target = regions["*"]
	}
	for _, scenario := range scenarios {
		if scenario.ID == target {
			return &scenarioPick{target, "country"}
		}
	}
	return nil
}

type scenarioProposal struct {
	Token      int64  `json:"token"`
	ScenarioID string `json:"scenario_id"`
	Automatic  bool   `json:"automatic"`
}
type scenarioChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type scenarioState struct {
	Selected  string            `json:"selected"`
	Active    string            `json:"active,omitempty"`
	Automatic bool              `json:"automatic"`
	Choices   []scenarioChoice  `json:"choices"`
	Pending   *scenarioProposal `json:"pending,omitempty"`
}
type scenarioDeparture struct {
	from string
	at   int64
}

// ScenarioController owns user intent and successful selection commits. Hosts
// own permission, storage, observations and dataplane transactions. A proposal
// never changes the active selection: commit only after the host transaction
// succeeds, abort on failure. Pass a monotonic millisecond clock for cooldowns.
type ScenarioController struct {
	mu         sync.Mutex
	profile    map[string]json.RawMessage
	scenarios  []config.Scenario
	regions    map[string]string
	state      scenarioState
	next       int64
	survey     int64
	departures []scenarioDeparture
}

func NewScenarioController(profileJSON, regionsJSON string, automatic bool) (*ScenarioController, error) {
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return nil, fmt.Errorf("invalid runtime profile")
	}
	if _, err = config.BuildConnectionPlan(profile); err != nil {
		return nil, err
	}
	c := &ScenarioController{scenarios: profile.Scenarios, state: scenarioState{Selected: profile.ActiveScenarioID, Automatic: automatic}}
	if err = json.Unmarshal([]byte(profileJSON), &c.profile); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(regionsJSON), &c.regions); err != nil {
		return nil, fmt.Errorf("invalid region mapping")
	}
	seen := map[string]bool{}
	for _, s := range c.scenarios {
		if s.ID == "" || seen[s.ID] {
			return nil, fmt.Errorf("empty or duplicate scenario identity")
		}
		seen[s.ID] = true
		c.state.Choices = append(c.state.Choices, scenarioChoice{s.ID, s.Name})
	}
	return c, nil
}
func (c *ScenarioController) Snapshot() string { c.mu.Lock(); defer c.mu.Unlock(); return c.snapshot() }
func (c *ScenarioController) snapshot() string { data, _ := json.Marshal(c.state); return string(data) }
func (c *ScenarioController) SetAutomatic(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Pending != nil {
		return fmt.Errorf("scenario transaction pending")
	}
	c.state.Automatic = enabled
	c.survey++
	return nil
}
func (c *ScenarioController) SetConnected(connected bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Pending != nil {
		return fmt.Errorf("scenario transaction pending")
	}
	if connected {
		c.state.Active = c.state.Selected
	} else {
		c.state.Active = ""
	}
	return nil
}
func (c *ScenarioController) BeginSurvey() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.survey++
	return c.survey
}
func (c *ScenarioController) CompleteSurvey(ticket int64, ssid, country string, nowMS int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ticket != c.survey || !c.state.Automatic || c.state.Pending != nil {
		return c.snapshot(), nil
	}
	// Consume the ticket even when no decision was available.
	c.survey++
	pick := pickScenario(c.scenarios, c.regions, ssid, country)
	if pick == nil || pick.ScenarioID == c.state.Selected {
		return c.snapshot(), nil
	}
	live := c.departures[:0]
	for _, entry := range c.departures {
		if nowMS-entry.at <= 600000 {
			live = append(live, entry)
		}
	}
	c.departures = live
	if c.state.Active != "" {
		for _, entry := range live {
			if entry.from == pick.ScenarioID {
				return c.snapshot(), nil
			}
		}
	}
	return c.propose(pick.ScenarioID, true)
}
func (c *ScenarioController) SelectScenario(target string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if target == c.state.Selected {
		return c.snapshot(), nil
	}
	return c.propose(target, false)
}
func (c *ScenarioController) propose(target string, automatic bool) (string, error) {
	if c.state.Pending != nil {
		return "", fmt.Errorf("scenario transaction pending")
	}
	found := false
	for _, s := range c.scenarios {
		if s.ID == target {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("scenario is unavailable")
	}
	if !automatic {
		c.state.Automatic = false
		c.survey++
	}
	c.next++
	c.state.Pending = &scenarioProposal{c.next, target, automatic}
	return c.snapshot(), nil
}

// ProfileForProposal retains unknown runtime metadata. It does not persist or
// activate anything; platforms compile and prepare this candidate separately.
func (c *ScenarioController) ProfileForProposal(token int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Pending == nil || c.state.Pending.Token != token {
		return "", fmt.Errorf("stale scenario transaction")
	}
	candidate := make(map[string]json.RawMessage, len(c.profile))
	for k, v := range c.profile {
		candidate[k] = v
	}
	candidate["active_scenario_id"], _ = json.Marshal(c.state.Pending.ScenarioID)
	data, err := json.Marshal(candidate)
	return string(data), err
}
func (c *ScenarioController) Commit(token, nowMS int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.state.Pending
	if p == nil || p.Token != token {
		return fmt.Errorf("stale scenario transaction")
	}
	if p.Automatic && c.state.Active != "" {
		c.departures = append(c.departures, scenarioDeparture{c.state.Selected, nowMS})
	}
	c.state.Selected = p.ScenarioID
	c.profile["active_scenario_id"], _ = json.Marshal(p.ScenarioID)
	if c.state.Active != "" {
		c.state.Active = p.ScenarioID
	}
	c.state.Pending = nil
	return nil
}
func (c *ScenarioController) Abort(token int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Pending == nil || c.state.Pending.Token != token {
		return fmt.Errorf("stale scenario transaction")
	}
	c.state.Pending = nil
	return nil
}
