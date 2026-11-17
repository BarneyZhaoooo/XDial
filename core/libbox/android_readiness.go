package libbox

import (
	"encoding/json"
	"fmt"
	"github.com/kafeifei/xdial/core/config"
)

// AndroidSessionReady distinguishes a started engine from an authenticated,
// reachable exit. The host keeps showing preparation until this returns true.
func (l *Libbox) AndroidSessionReady(profileJSON string) (bool, error) {
	return l.androidSessionReady(profileJSON, false)
}

func (l *Libbox) AndroidPreparedSwitchReady(profileJSON string) (bool, error) {
	return l.androidSessionReady(profileJSON, true)
}

func (l *Libbox) androidSessionReady(profileJSON string, prepared bool) (bool, error) {
	l.mu.Lock()
	running := l.running && !l.cleaning && !l.switchCommitInProgress
	if prepared {
		running = running && l.preparedSwitch != nil
	}
	l.mu.Unlock()
	if !running {
		return false, fmt.Errorf("connection is not running")
	}
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return false, err
	}
	line, err := config.ActiveTailscaleLine(profile)
	if err != nil {
		return false, err
	}
	if line == nil {
		return true, nil
	}
	tag := "tailscale-" + line.ID
	statusFunc, probeFunc, domainsFunc := l.TailscaleStatus, l.ProbeTailscalePeer, l.TailscaleDNSCaptureDomains
	if prepared {
		statusFunc, probeFunc, domainsFunc = l.PreparedSwitchTailscaleStatus, l.ProbePreparedSwitchTailscalePeer, l.preparedSwitchTailscaleDNSCaptureDomains
	}
	raw, err := statusFunc(tag)
	if err != nil {
		return false, err
	}
	var status struct {
		Backend       string `json:"backend_state"`
		MagicDNSReady bool   `json:"magic_dns_ready"`
		Nodes         []struct {
			IP               string
			Online, Selected bool
		} `json:"exit_nodes"`
	}
	if err = json.Unmarshal([]byte(raw), &status); err != nil {
		return false, err
	}
	if status.Backend == "NeedsLogin" || status.Backend == "NeedsMachineAuth" {
		return false, fmt.Errorf("Tailscale authorization required")
	}
	if status.Backend != "Running" {
		return false, nil
	}
	ready := false
	for _, node := range status.Nodes {
		if node.IP == line.TailscaleExitNode && node.Online && node.Selected {
			ready = true
			break
		}
	}
	if !ready {
		return false, nil
	}
	if line.TailscaleMagicDNS {
		// Android uses the NE native endpoint transport, not desktop memory-only
		// hosts. Require its owned-domain view; never install a desktop snapshot
		// into the different transport or fall back for tailnet names.
		if !status.MagicDNSReady {
			return false, nil
		}
		rawDomains, domainErr := domainsFunc(tag)
		if domainErr != nil {
			return false, domainErr
		}
		var domains []string
		if json.Unmarshal([]byte(rawDomains), &domains) != nil || len(domains) == 0 || len(domains) > 2048 {
			return false, nil
		}
	}
	probe, err := probeFunc(tag, line.TailscaleExitNode, 1500)
	if err != nil {
		return false, err
	}
	var result struct {
		TSMP struct{ Success bool } `json:"tsmp"`
	}
	if err = json.Unmarshal([]byte(probe), &result); err != nil {
		return false, err
	}
	return result.TSMP.Success, nil
}
