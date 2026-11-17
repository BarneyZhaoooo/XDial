package mobile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kafeifei/xdial/core/config"
)

func scenarioDocument(raw, id string) (map[string]json.RawMessage, []map[string]json.RawMessage, map[string]string, int, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil, nil, -1, fmt.Errorf("scenario unavailable")
	}
	p, err := config.ParseRuntimeProfile([]byte(raw))
	if err != nil {
		return nil, nil, nil, -1, fmt.Errorf("invalid profile")
	}
	var root map[string]json.RawMessage
	var scenes []map[string]json.RawMessage
	regions := map[string]string{}
	if json.Unmarshal([]byte(raw), &root) != nil || json.Unmarshal(root["scenarios"], &scenes) != nil {
		return nil, nil, nil, -1, fmt.Errorf("invalid scenarios")
	}
	if value, ok := root["region_scenarios"]; ok && json.Unmarshal(value, &regions) != nil {
		return nil, nil, nil, -1, fmt.Errorf("invalid region map")
	}
	if regions == nil {
		regions = map[string]string{}
	}
	index := -1
	for i, s := range p.Scenarios {
		if s.ID == id {
			if index != -1 {
				return nil, nil, nil, -1, fmt.Errorf("duplicate scenario")
			}
			index = i
		}
	}
	if index < 0 {
		return nil, nil, nil, -1, fmt.Errorf("scenario unavailable")
	}
	return root, scenes, regions, index, nil
}

// ScenarioConditions is presentation data; unknown Profile/Scenario fields are
// retained by the editor. Conditions belong to global scenarios, not sources.
func ScenarioConditions(raw, id string) (string, error) {
	_, scenes, regions, index, err := scenarioDocument(raw, id)
	if err != nil {
		return "", err
	}
	ssids := []string{}
	if value, ok := scenes[index]["match_ssids"]; ok && json.Unmarshal(value, &ssids) != nil {
		return "", fmt.Errorf("invalid SSIDs")
	}
	if ssids == nil {
		ssids = []string{}
	}
	countries := []string{}
	for code, target := range regions {
		if target == id {
			countries = append(countries, code)
		}
	}
	sort.Strings(countries)
	data, err := json.Marshal(map[string]any{"ssids": ssids, "countries": countries})
	return string(data), err
}

// UpdateScenarioConditions accepts one SSID per line, and comma/space/newline
// separated country codes. A condition already owned by another scenario is
// rejected instead of silently stealing that scenario's activation condition.
func UpdateScenarioConditions(raw, id, ssidText, countryText string) (string, error) {
	root, scenes, regions, index, err := scenarioDocument(raw, id)
	if err != nil {
		return "", err
	}
	if len(ssidText) > 4096 || len(countryText) > 2048 {
		return "", fmt.Errorf("conditions exceed limit")
	}
	ssids := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(ssidText, "\n") {
		ssid := strings.TrimSpace(line)
		if ssid == "" {
			continue
		}
		if !utf8.ValidString(ssid) || len(ssid) > 32 || strings.ContainsAny(ssid, "\x00\r") {
			return "", fmt.Errorf("invalid SSID")
		}
		if !seen[ssid] {
			seen[ssid] = true
			ssids = append(ssids, ssid)
		}
	}
	if len(ssids) > 64 {
		return "", fmt.Errorf("too many SSIDs")
	}
	for i, scene := range scenes {
		if i == index {
			continue
		}
		var existing []string
		if value, ok := scene["match_ssids"]; ok && json.Unmarshal(value, &existing) != nil {
			return "", fmt.Errorf("invalid SSIDs")
		}
		for _, ssid := range existing {
			if seen[strings.TrimSpace(ssid)] {
				return "", fmt.Errorf("SSID belongs to another scenario")
			}
		}
	}
	for code, target := range regions {
		if target == id {
			delete(regions, code)
		}
	}
	for _, word := range strings.FieldsFunc(countryText, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		code := strings.ToUpper(word)
		if code != "*" && (len(code) != 2 || code == "XX" || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z') {
			return "", fmt.Errorf("invalid country code")
		}
		if target, ok := regions[code]; ok && target != id {
			return "", fmt.Errorf("country belongs to another scenario")
		}
		regions[code] = id
	}
	scenes[index]["match_ssids"], _ = json.Marshal(ssids)
	root["scenarios"], _ = json.Marshal(scenes)
	root["region_scenarios"], _ = json.Marshal(regions)
	data, err := json.Marshal(root)
	return string(data), err
}
