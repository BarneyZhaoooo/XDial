package mobile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/kafeifei/xdial/core/config"
)

type scenarioEdit struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	LineID string `json:"line_id"`
	RuleID string `json:"rule_id"`
	Offset int    `json:"offset"`
}

type editorChoice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ScenarioEditor exposes presentation only. It never returns protocol fields,
// credentials, source URLs or condition contents to a scenario picker.
func ScenarioEditor(raw, id string) (string, error) {
	p, err := config.ParseProfile([]byte(raw))
	if err != nil {
		return "", err
	}
	choices := []editorChoice{}
	lines := []editorChoice{}
	rules := []editorChoice{}
	for _, s := range p.Scenarios {
		choices = append(choices, editorChoice{s.ID, s.Name, true})
	}
	for _, line := range p.Lines {
		lines = append(lines, editorChoice{line.ID, line.Name, line.Enabled})
	}
	for _, rule := range p.RuleSets {
		rules = append(rules, editorChoice{rule.ID, rule.Name, rule.Enabled})
	}
	if p.FindLine("direct") == nil {
		lines = append(lines, editorChoice{"direct", "Direct", true})
	}
	view := map[string]any{"scenarios": choices, "lines": lines, "rules": rules, "selected": p.ActiveScenarioID}
	if id != "" {
		_, scenes, _, index, err := scenarioDocument(raw, id)
		if err != nil {
			return "", err
		}
		s := p.Scenarios[index]
		rows := []map[string]any{}
		for _, binding := range s.Bindings {
			rows = append(rows, map[string]any{"rule_id": binding.RuleSetID, "line_id": binding.LineID, "partial": len(binding.ConditionIDs), "legacy_subscription": binding.SubscriptionID != ""})
		}
		var order []string
		json.Unmarshal(scenes[index]["match_order"], &order)
		view["scenario"] = map[string]any{"id": id, "name": s.Name, "line_id": s.DefaultLineID, "bindings": rows, "source_order": len(order) > 0}
	}
	data, err := json.Marshal(view)
	return string(data), err
}

// EditScenario is an offline immutable edit. Hosts commit the returned runtime
// through the library revision CAS and only while their system tunnel is idle.
// Raw object copies preserve unknown metadata, partial bindings and provenance.
func EditScenario(raw, command string) (string, error) {
	if len(command) > 8192 {
		return "", fmt.Errorf("scenario edit exceeds limit")
	}
	var edit scenarioEdit
	decoder := json.NewDecoder(strings.NewReader(command))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&edit); err != nil {
		return "", fmt.Errorf("invalid scenario edit")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", fmt.Errorf("invalid trailing scenario edit")
	}
	var root map[string]json.RawMessage
	var scenes []map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &root) != nil || root == nil || json.Unmarshal(root["scenarios"], &scenes) != nil {
		return "", fmt.Errorf("invalid scenarios")
	}
	p, err := config.ParseProfile([]byte(raw))
	if err != nil {
		return "", err
	}
	index := -1
	for i, s := range p.Scenarios {
		if s.ID == edit.ID {
			if index >= 0 {
				return "", fmt.Errorf("duplicate scenario")
			}
			index = i
		}
	}
	field := func(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
	validName := func() bool {
		edit.Name = strings.TrimSpace(edit.Name)
		return edit.Name != "" && utf8.ValidString(edit.Name) && utf8.RuneCountInString(edit.Name) <= 80 && !strings.ContainsAny(edit.Name, "\x00\r\n")
	}
	validLine := func() bool {
		l := p.FindLine(edit.LineID)
		if l != nil {
			return l.Enabled
		}
		return edit.LineID == "direct"
	}
	if edit.Action == "create" {
		if !validName() || !validLine() || len(scenes) >= 256 {
			return "", fmt.Errorf("invalid new scenario")
		}
		var entropy [16]byte
		if _, err = rand.Read(entropy[:]); err != nil {
			return "", err
		}
		id := "scene-" + hex.EncodeToString(entropy[:])
		scenes = append(scenes, map[string]json.RawMessage{"id": field(id), "name": field(edit.Name), "default_line_id": field(edit.LineID), "bindings": field([]any{})})
		if p.ActiveScenarioID == "" {
			root["active_scenario_id"] = field(id)
		}
	} else {
		if index < 0 {
			return "", fmt.Errorf("scenario unavailable")
		}
		scene := scenes[index]
		bindings := []map[string]json.RawMessage{}
		if v := scene["bindings"]; len(v) > 0 && json.Unmarshal(v, &bindings) != nil {
			return "", fmt.Errorf("invalid bindings")
		}
		bindingIndex := -1
		for i, b := range bindings {
			var id string
			json.Unmarshal(b["rule_set_id"], &id)
			if id == edit.RuleID {
				if bindingIndex >= 0 {
					return "", fmt.Errorf("duplicate binding")
				}
				bindingIndex = i
			}
		}
		switch edit.Action {
		case "rename":
			if !validName() {
				return "", fmt.Errorf("invalid scenario name")
			}
			scene["name"] = field(edit.Name)
		case "default":
			if !validLine() {
				return "", fmt.Errorf("line unavailable")
			}
			scene["default_line_id"] = field(edit.LineID)
			delete(scene, "default_subscription_id")
		case "bind":
			rule := p.FindRuleSet(edit.RuleID)
			if rule == nil || !rule.Enabled || !validLine() {
				return "", fmt.Errorf("rule or line unavailable")
			}
			if bindingIndex < 0 {
				bindings = append(bindings, map[string]json.RawMessage{"rule_set_id": field(edit.RuleID)})
				bindingIndex = len(bindings) - 1
			}
			bindings[bindingIndex]["line_id"] = field(edit.LineID)
			delete(bindings[bindingIndex], "subscription_id")
		case "unbind":
			if bindingIndex < 0 {
				return "", fmt.Errorf("binding unavailable")
			}
			// Remove only this binding's private condition positions from source order.
			remove := map[string]bool{edit.RuleID: true}
			if rule := p.FindRuleSet(edit.RuleID); rule != nil {
				for _, c := range rule.Conditions {
					remove[c.ID] = true
				}
			}
			var order []string
			json.Unmarshal(scene["match_order"], &order)
			kept := []string{}
			for _, id := range order {
				if !remove[id] {
					kept = append(kept, id)
				}
			}
			scene["match_order"] = field(kept)
			bindings = append(bindings[:bindingIndex], bindings[bindingIndex+1:]...)
		case "full_rule":
			if bindingIndex < 0 {
				return "", fmt.Errorf("binding unavailable")
			}
			delete(bindings[bindingIndex], "condition_ids")
		case "list_order":
			delete(scene, "match_order")
		case "move":
			var order []string
			json.Unmarshal(scene["match_order"], &order)
			target := bindingIndex + edit.Offset
			if len(order) > 0 || bindingIndex < 0 || (edit.Offset != -1 && edit.Offset != 1) || target < 0 || target >= len(bindings) {
				return "", fmt.Errorf("change to list order before moving a binding")
			}
			bindings[bindingIndex], bindings[target] = bindings[target], bindings[bindingIndex]
		case "delete":
			if edit.ID == p.ActiveScenarioID || len(scenes) <= 1 {
				return "", fmt.Errorf("choose another scenario before deleting")
			}
			var ssids []string
			json.Unmarshal(scene["match_ssids"], &ssids)
			var regions map[string]string
			json.Unmarshal(root["region_scenarios"], &regions)
			if len(ssids) > 0 {
				return "", fmt.Errorf("remove automatic conditions before deleting")
			}
			for _, id := range regions {
				if id == edit.ID {
					return "", fmt.Errorf("remove automatic conditions before deleting")
				}
			}
			scenes = append(scenes[:index], scenes[index+1:]...)
		default:
			return "", fmt.Errorf("unknown scenario edit")
		}
		if edit.Action == "bind" || edit.Action == "unbind" || edit.Action == "full_rule" || edit.Action == "move" {
			scene["bindings"] = field(bindings)
		}
	}
	root["scenarios"] = field(scenes)
	data, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	updated, err := config.ParseProfile(data)
	if err != nil {
		return "", err
	}
	if err = config.ValidateProfileLibraryRuntime(updated); err != nil {
		return "", err
	}
	return string(data), nil
}
