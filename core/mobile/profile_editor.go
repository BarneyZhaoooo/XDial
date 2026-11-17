package mobile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kafeifei/xdial/core/config"
	"net"
	"net/url"
	"strings"
)

// AddAnyConnectScenario adds a native-form entry without discarding imported
// scenarios, rules or unknown metadata. The host must encrypt the returned
// document, which contains credentials. Never put it in diagnostics.
func AddAnyConnectScenario(profileJSON, name, server, username, password string) (string, error) {
	name = strings.TrimSpace(name)
	server = strings.TrimSpace(server)
	username = strings.TrimSpace(username)
	if name == "" || server == "" || username == "" || password == "" {
		return "", fmt.Errorf("required fields are missing")
	}
	raw := server
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return "", fmt.Errorf("server must be an HTTPS host with optional port")
	}
	if strings.ContainsAny(endpoint.Hostname(), " \t\n") {
		return "", fmt.Errorf("invalid server host")
	}
	if port := endpoint.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", fmt.Errorf("invalid server port")
		}
	}
	var document map[string]json.RawMessage
	if strings.TrimSpace(profileJSON) == "" {
		document = map[string]json.RawMessage{}
	} else if err = json.Unmarshal([]byte(profileJSON), &document); err != nil || document == nil {
		return "", fmt.Errorf("invalid existing profile")
	}
	var lines, scenarios []json.RawMessage
	if raw := document["lines"]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &lines); err != nil {
			return "", fmt.Errorf("invalid existing lines")
		}
	}
	if raw := document["scenarios"]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &scenarios); err != nil {
			return "", fmt.Errorf("invalid existing scenarios")
		}
	}
	entropy := make([]byte, 12)
	if _, err = rand.Read(entropy); err != nil {
		return "", fmt.Errorf("cannot create identity")
	}
	id := "vpn-" + hex.EncodeToString(entropy)
	line, _ := json.Marshal(config.Line{ID: id, Name: name, Type: config.LineTypeVPN, Enabled: true, VPNServer: server, VPNUsername: username, VPNPassword: password})
	scenario, _ := json.Marshal(config.Scenario{ID: id, Name: name, DefaultLineID: id})
	document["lines"], _ = json.Marshal(append(lines, line))
	document["scenarios"], _ = json.Marshal(append(scenarios, scenario))
	document["active_scenario_id"], _ = json.Marshal(id)
	document["mobile_automatic"] = json.RawMessage("false")
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	profile, err := config.ParseRuntimeProfile(encoded)
	if err != nil {
		return "", err
	}
	if _, err = config.BuildConnectionPlan(profile); err != nil {
		return "", err
	}
	return string(encoded), nil
}
