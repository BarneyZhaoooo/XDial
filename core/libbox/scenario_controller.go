//go:build !windows

package libbox

import "github.com/kafeifei/xdial/core/mobile"

// PickScenario exposes the platform-independent mobile policy to gomobile.
func PickScenario(scenariosJSON, regionsJSON, ssid, country string) (string, error) {
	return mobile.PickScenario(scenariosJSON, regionsJSON, ssid, country)
}

type ScenarioController struct{ controller *mobile.ScenarioController }

func NewScenarioController(profileJSON, regionsJSON string, automatic bool) (*ScenarioController, error) {
	controller, err := mobile.NewScenarioController(profileJSON, regionsJSON, automatic)
	if err != nil {
		return nil, err
	}
	return &ScenarioController{controller}, nil
}
func (c *ScenarioController) Snapshot() string { return c.controller.Snapshot() }
func (c *ScenarioController) SetAutomatic(enabled bool) error {
	return c.controller.SetAutomatic(enabled)
}
func (c *ScenarioController) SetConnected(connected bool) error {
	return c.controller.SetConnected(connected)
}
func (c *ScenarioController) BeginSurvey() int64 { return c.controller.BeginSurvey() }
func (c *ScenarioController) CompleteSurvey(ticket int64, ssid, country string, nowMS int64) (string, error) {
	return c.controller.CompleteSurvey(ticket, ssid, country, nowMS)
}
func (c *ScenarioController) SelectScenario(target string) (string, error) {
	return c.controller.SelectScenario(target)
}
func (c *ScenarioController) ProfileForProposal(token int64) (string, error) {
	return c.controller.ProfileForProposal(token)
}
func (c *ScenarioController) Commit(token, nowMS int64) error {
	return c.controller.Commit(token, nowMS)
}
func (c *ScenarioController) Abort(token int64) error { return c.controller.Abort(token) }

// ConnectionController shares intent and recovery policy across mobile hosts.
type ConnectionController struct{ controller *mobile.ConnectionController }

func NewConnectionController() *ConnectionController {
	return &ConnectionController{mobile.NewConnectionController()}
}
func (c *ConnectionController) RequestConnection(enabled bool) {
	c.controller.RequestConnection(enabled)
}
func (c *ConnectionController) ObserveUnderlay(identity string) {
	c.controller.ObserveUnderlay(identity)
}
func (c *ConnectionController) NextAttempt(nowMS int64) int64 { return c.controller.NextAttempt(nowMS) }
func (c *ConnectionController) CompleteAttempt(epoch int64, success, retryable bool, nowMS int64) {
	c.controller.CompleteAttempt(epoch, success, retryable, nowMS)
}
func (c *ConnectionController) Snapshot() string { return c.controller.Snapshot() }
