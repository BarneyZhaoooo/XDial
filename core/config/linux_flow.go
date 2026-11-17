package config

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// GenerateLinuxFlow reuses the ordered Scenario/DNS compiler and lowers only
// the platform admission surface. It does not fetch, listen, or install rules.
// The first native resolver backend deliberately accepts only one unicast OS
// resolver. Stub/search/split-DNS support requires a protected native backend;
// flattening it to public DNS or the daemon's default resolver is forbidden.
func GenerateLinuxFlow(profile *Profile, basePath, systemDNS string, bypassMark uint32) ([]byte, error) {
	ip, e := netip.ParseAddr(systemDNS)
	if e != nil || !ip.Is4() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip == netip.MustParseAddr("255.255.255.255") {
		return nil, fmt.Errorf("Linux DNS requires a captured unicast resolver backend")
	}
	if bypassMark == 0 || bypassMark&(bypassMark-1) != 0 {
		return nil, fmt.Errorf("Linux bypass mark is not reserved")
	}
	if profile == nil || profile.ActiveScenario() == nil {
		return nil, fmt.Errorf("missing active scenario")
	}
	profile, e = ExpandRuleSetGroups(profile)
	if e != nil {
		return nil, e
	}
	ids, _ := effectiveActiveTargetIDs(profile, profile.ActiveScenario())
	expandGroupLineIDs(profile, ids)
	for i := range profile.Lines {
		l := &profile.Lines[i]
		if !ids[l.ID] {
			continue
		}
		if l.Type == LineTypeVPN || l.Type == LineTypeTailscale {
			return nil, fmt.Errorf("Linux protocol requires a protected capability lease")
		}
		// NativeOptions can contain certificate/key paths. A privileged service
		// must not read arbitrary client paths; admit audited subsets separately.
		if len(l.NativeOptions) != 0 {
			return nil, fmt.Errorf("Linux privileged native options require an audited capability")
		}
	}
	selectors := activeApplicationRuleSetSelectors(profile, profile.ActiveScenario())
	predicates := map[string]map[string]any{}
	for _, s := range selectors {
		var match map[string]any
		switch s.Kind {
		case ApplicationProcessSelectorExactPath:
			match = map[string]any{"process_path": []string{s.Value}}
		case ApplicationProcessSelectorPathPrefix:
			match = map[string]any{"process_path_regex": []string{"^" + regexp.QuoteMeta(s.Value) + "/"}}
		case ApplicationProcessSelectorName:
			if strings.ContainsAny(s.Value, "*?") {
				return nil, fmt.Errorf("Linux process-name wildcards require a native matching adapter")
			}
			match = map[string]any{"process_name": []string{s.Value}}
		default:
			return nil, fmt.Errorf("macOS bundle identities cannot authorize Linux processes")
		}
		predicates[ApplicationSOCKSUsername("", s)] = match
	}
	data, e := generateSingBox(profile, 0, "", PlatformTransparentProxy, basePath, nil, []string{systemDNS}, "", &transparentProxyIngress{nativeFlow: true})
	if e != nil {
		return nil, e
	}
	var d map[string]any
	if e = json.Unmarshal(data, &d); e != nil {
		return nil, e
	}
	if in, ok := d["inbounds"].([]any); ok && len(in) > 0 {
		return nil, fmt.Errorf("native flow compiler created a listener")
	}
	delete(d, "inbounds")
	route := d["route"].(map[string]any)
	route["default_mark"] = bypassMark
	route["find_process"] = true
	delete(d, "experimental")
	d["log"] = map[string]any{"disabled": true}
	dns := d["dns"].(map[string]any)
	for i, v := range dns["servers"].([]any) {
		s := v.(map[string]any)
		// Both Direct resolution and proxy endpoint bootstrap use captured OS
		// DNS. Proxy-owned application DNS remains on its exact compiled Line.
		if s["type"] == "local" || s["tag"] == desktopPublicDNSTag {
			dns["servers"].([]any)[i] = map[string]any{"type": "udp", "tag": s["tag"], "server": systemDNS, "server_port": 53}
		}
	}
	var lower func(any) error
	lower = func(v any) error {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				if e := lower(child); e != nil {
					return e
				}
			}
		case map[string]any:
			if raw, ok := x["auth_user"]; ok {
				users, ok := raw.([]any)
				if !ok || len(users) == 0 {
					return fmt.Errorf("invalid native application predicate")
				}
				var children []any
				for _, u := range users {
					key, ok := u.(string)
					if !ok {
						return fmt.Errorf("invalid application identity")
					}
					m, ok := predicates[key]
					if !ok {
						return fmt.Errorf("unknown application identity")
					}
					children = append(children, m)
				}
				delete(x, "auth_user")
				x["type"] = "logical"
				x["mode"] = "or"
				x["rules"] = children
			}
			for _, child := range x {
				if e := lower(child); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e = lower(route["rules"]); e != nil {
		return nil, e
	}
	if e = lower(dns["rules"]); e != nil {
		return nil, e
	}
	return json.Marshal(d)
}
