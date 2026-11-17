package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrAndroidAcquisitionProtocolNeedsLease is temporary: protocol-owned resource
// sessions must acquire the shared identity/session lease before they can run
// alongside a traffic runtime. Never replace such a fetch Line with Direct.
var ErrAndroidAcquisitionProtocolNeedsLease = errors.New("resource protocol requires an isolated session lease")

// GenerateAndroidAcquisition reuses the core's exact-Line DNS compiler. The
// resulting capability has no system ingress, user RuleSet, listener or cache.
// Native local DNS transports are replaced with captured OS facts before any
// runtime is created; a platform callback still owns protection/binding sockets.
func GenerateAndroidAcquisition(profile *Profile, lineID, basePath string, systemDNS []string) ([]byte, error) {
	return generateResourceAcquisition(profile, lineID, basePath, systemDNS, false)
}

// GenerateAppleResourceAcquisition is the same exact-Line capability for Apple
// callers whose sockets XDial never captures. It keeps the OS Underlay routing
// instead of Android's callback-selected network.
func GenerateAppleResourceAcquisition(profile *Profile, lineID, basePath string, systemDNS []string) ([]byte, error) {
	return generateResourceAcquisition(profile, lineID, basePath, systemDNS, true)
}

func generateResourceAcquisition(profile *Profile, lineID, basePath string, systemDNS []string, osRouted bool) ([]byte, error) {
	server, err := validateTransparentProxySystemDNS(systemDNS)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("resource profile is missing")
	}
	if lineID != builtinDirectLineID {
		line := profile.FindLine(lineID)
		if line == nil || !line.Enabled {
			return nil, fmt.Errorf("resource Line is unavailable")
		}
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var isolated Profile
	if err = json.Unmarshal(encoded, &isolated); err != nil {
		return nil, err
	}
	isolated.Scenarios = []Scenario{{ID: "resource", DefaultLineID: lineID}}
	isolated.ActiveScenarioID = "resource"
	isolated.RuleSets = nil
	isolated.Subscriptions = nil
	// Reuse exact-Line DNS compilation through a capability that cannot create
	// ingress. No temporary listener, credentials or synthetic interface is used.
	data, err := generateSingBox(&isolated, 0, "", PlatformTransparentProxy, basePath, nil, systemDNS, "", &transparentProxyIngress{androidResource: true, osRoutedResource: osRouted})
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	var endpoints []json.RawMessage
	if err = json.Unmarshal(document["endpoints"], &endpoints); err == nil && len(endpoints) != 0 {
		return nil, ErrAndroidAcquisitionProtocolNeedsLease
	}
	var outbounds []map[string]any
	if err = json.Unmarshal(document["outbounds"], &outbounds); err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		if outbound["type"] == "vpn" {
			return nil, ErrAndroidAcquisitionProtocolNeedsLease
		}
	}
	var dns map[string]any
	if err = json.Unmarshal(document["dns"], &dns); err != nil {
		return nil, err
	}
	servers, ok := dns["servers"].([]any)
	if !ok {
		return nil, fmt.Errorf("resource DNS is unavailable")
	}
	for i, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("resource DNS is invalid")
		}
		if entry["type"] == "local" {
			servers[i] = map[string]any{"type": "udp", "tag": entry["tag"], "server": server, "server_port": 53}
		}
	}
	document["dns"], err = json.Marshal(dns)
	if err != nil {
		return nil, err
	}
	delete(document, "inbounds")
	delete(document, "experimental")
	document["log"] = json.RawMessage(`{"disabled":true}`)
	return json.Marshal(document)
}
