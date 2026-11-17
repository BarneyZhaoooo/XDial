package mobile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/kafeifei/xdial/core/config"
)

func setupID() (string, error) {
	b := make([]byte, 12)
	_, err := rand.Read(b)
	return "tail-" + hex.EncodeToString(b), err
}
func profileDocument(raw string) (map[string]json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if strings.TrimSpace(raw) == "" {
		raw = `{"lines":[{"id":"direct","name":"直连","type":"direct","enabled":true}],"scenarios":[{"id":"direct","name":"直连","default_line_id":"direct"}],"active_scenario_id":"direct"}`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc == nil {
		return nil, fmt.Errorf("invalid profile")
	}
	return doc, nil
}

// EnsureTailscaleSetup retains the selected Scenario. A pending login line never
// becomes a user-traffic binding. The host atomically stores profile then starts
// a no-ingress setup session using line_id; the result contains private config.
func EnsureTailscaleSetup(raw string) (string, error) {
	doc, err := profileDocument(raw)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(doc)
	profile, err := config.ParseRuntimeProfile(encoded)
	if err != nil {
		return "", err
	}
	lineID := ""
	for _, line := range profile.Lines {
		if line.Type == config.LineTypeTailscale {
			lineID = line.ID
			break
		}
	}
	if lineID == "" {
		lineID, err = setupID()
		if err != nil {
			return "", err
		}
		var lines []json.RawMessage
		if err = json.Unmarshal(doc["lines"], &lines); err != nil {
			return "", err
		}
		line, _ := json.Marshal(config.Line{ID: lineID, Name: "Tailscale", Type: config.LineTypeTailscale, Enabled: true})
		doc["lines"], _ = json.Marshal(append(lines, line))
	}
	encoded, _ = json.Marshal(doc)
	result, err := json.Marshal(struct {
		Profile string `json:"profile"`
		LineID  string `json:"line_id"`
	}{string(encoded), lineID})
	return string(result), err
}

// SelectTailscaleExit accepts only a currently online exit advertised by LocalAPI.
// It adds a new Scenario/Line instead of silently altering existing bindings.
func SelectTailscaleExit(raw, lineID, statusJSON, nodeID string) (string, error) {
	doc, err := profileDocument(raw)
	if err != nil {
		return "", err
	}
	var status struct {
		Backend string `json:"backend_state"`
		Nodes   []struct {
			ID, Name, IP string
			Online       bool
		} `json:"exit_nodes"`
	}
	if err = json.Unmarshal([]byte(statusJSON), &status); err != nil || status.Backend != "Running" {
		return "", fmt.Errorf("Tailscale is not signed in")
	}
	name, ip := "", ""
	for _, node := range status.Nodes {
		if node.ID == nodeID && node.Online {
			name, ip = node.Name, node.IP
			break
		}
	}
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.IsValid() || address.IsUnspecified() {
		return "", fmt.Errorf("exit node unavailable")
	}
	var lines, scenarios []map[string]json.RawMessage
	if err = json.Unmarshal(doc["lines"], &lines); err != nil {
		return "", err
	}
	if err = json.Unmarshal(doc["scenarios"], &scenarios); err != nil {
		return "", err
	}
	var selected map[string]json.RawMessage
	for _, line := range lines {
		var id, kind string
		json.Unmarshal(line["id"], &id)
		json.Unmarshal(line["type"], &kind)
		if id == lineID && kind == "tailscale" {
			selected = line
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("Tailscale line unavailable")
	}
	copyLine := map[string]json.RawMessage{}
	for key, value := range selected {
		copyLine[key] = value
	}
	id, err := setupID()
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "Tailscale"
	}
	copyLine["id"], _ = json.Marshal(id)
	copyLine["name"], _ = json.Marshal(name)
	copyLine["enabled"] = json.RawMessage("true")
	copyLine["tailscale_exit_node"], _ = json.Marshal(ip)
	delete(copyLine, "tailscale_auth_key")
	scenario := map[string]json.RawMessage{}
	scenario["id"], _ = json.Marshal(id)
	scenario["name"], _ = json.Marshal(name)
	scenario["default_line_id"], _ = json.Marshal(id)
	doc["lines"], _ = json.Marshal(append(lines, copyLine))
	doc["scenarios"], _ = json.Marshal(append(scenarios, scenario))
	doc["active_scenario_id"], _ = json.Marshal(id)
	doc["mobile_automatic"] = json.RawMessage("false")
	encoded, _ := json.Marshal(doc)
	profile, err := config.ParseRuntimeProfile(encoded)
	if err != nil {
		return "", err
	}
	if _, err = config.BuildConnectionPlan(profile); err != nil {
		return "", err
	}
	return string(encoded), nil
}

// ValidateTailscaleAuthURL keeps browser launch outside the core. Never log URLs.
func ValidateTailscaleAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
