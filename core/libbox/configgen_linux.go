//go:build linux && !android

package libbox

import (
	"encoding/json"
	"fmt"
	"github.com/kafeifei/xdial/core/config"
	"strings"
)

// GenerateLinuxConfig is cache-only. The service owns basePath and network
// facts; neither a client path nor raw sing-box instructions cross this API.
func GenerateLinuxConfig(profileJSON, systemDNS, basePath string, bypassMark uint32) (string, error) {
	if err := validateLinuxProfileFiles(profileJSON); err != nil {
		return "", err
	}
	return generatePacketTunnelConfigWithFetcher(profileJSON, basePath, func(p *config.Profile) ([]byte, error) {
		return config.GenerateLinuxFlow(p, basePath, systemDNS, bypassMark)
	}, nil)
}

func GenerateLinuxResolvedConfig(profileJSON, basePath string, bypassMark uint32) (string, error) {
	if err := validateLinuxProfileFiles(profileJSON); err != nil {
		return "", err
	}
	return generatePacketTunnelConfigWithFetcher(profileJSON, basePath, func(p *config.Profile) ([]byte, error) {
		return config.GenerateLinuxResolvedFlow(p, basePath, bypassMark)
	}, nil)
}

// validateLinuxProfileFiles runs before materialization may open any local
// source. Root-owned caches are allowed only after compiler materialization.
func validateLinuxProfileFiles(profileJSON string) error {
	var value any
	if err := json.Unmarshal([]byte(profileJSON), &value); err != nil {
		return err
	}
	var visit func(any) error
	visit = func(v any) error {
		switch x := v.(type) {
		case []any:
			for _, c := range x {
				if e := visit(c); e != nil {
					return e
				}
			}
		case map[string]any:
			for k, c := range x {
				if k == "native_options" && c != nil {
					return fmt.Errorf("native options require privileged admission")
				}
				if k == "url" || k == "geoip_rule_set_url_template" {
					if text, ok := c.(string); ok && strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "file:") {
						return fmt.Errorf("client local rule files are not service capabilities")
					}
				}
				if e := visit(c); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return visit(value)
}
