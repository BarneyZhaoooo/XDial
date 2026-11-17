package libbox

import (
	"bytes"
	"context"
	"fmt"

	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/engine"
	"github.com/sagernet/sing-box/option"
	jsonext "github.com/sagernet/sing/common/json"
)

// PrepareAndroidSwitch builds resources and a complete candidate while the
// stable ingress and source keep serving. All temporary sockets belong to the
// source's captured protected Underlay; Stop cancels the entire borrower.
func (l *Libbox) PrepareAndroidSwitch(profileJSON, systemDNSJSON, basePath, interfacesJSON, defaultName string, defaultIndex int) error {
	l.mu.Lock()
	if !l.running || l.cleaning || l.packetIngress == nil || l.androidSwitchCancel != nil || l.switchCommitInProgress || l.switchPreparing || l.preparedSwitch != nil || l.retiredSwitch != nil || l.switchRetireInProgress {
		l.mu.Unlock()
		return fmt.Errorf("Android switch is unavailable in the current state")
	}
	if l.androidRefreshCancel != nil {
		l.androidRefreshCancel()
	}
	parent, cancel := context.WithCancel(l.runtimeCtx)
	l.androidSwitchCancel = cancel
	l.runtimeUsers.Add(1)
	ingress := l.packetIngress
	platform := l.platform.lineRuntimeCapability()
	l.mu.Unlock()
	defer func() { cancel(); l.mu.Lock(); l.androidSwitchCancel = nil; l.mu.Unlock(); l.runtimeUsers.Done() }()
	preparation := New(nil)
	preparation.platform = platform
	generated, err := preparation.prepareAndroidConfigContext(parent, profileJSON, systemDNSJSON, basePath)
	if err != nil {
		return err
	}
	ctx := boxContext(parent)
	options, err := jsonext.UnmarshalExtendedContext[option.Options](ctx, []byte(generated))
	if err != nil {
		return err
	}
	if len(options.Inbounds) != 1 || options.Inbounds[0].Type != "tun" {
		return fmt.Errorf("candidate must preserve packet ingress")
	}
	target, err := jsonext.Marshal(options.Inbounds[0])
	if err != nil {
		return err
	}
	if !bytes.Equal(target, ingress.template) {
		return fmt.Errorf("candidate requires different system VPN settings")
	}
	options.Inbounds = nil
	data, err := jsonext.Marshal(options)
	if err != nil {
		return err
	}
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return err
	}
	line := profile.ActiveVPNLine()
	tail, err := config.ActiveTailscaleLine(profile)
	if err != nil {
		return err
	}
	lineID, identity, tailID, tailIdentity := "", "", "", ""
	var vpnConfig *engine.VPNConfig
	if tail != nil {
		tailID = tail.ID
		tailIdentity, err = lineRuntimeIdentity(profile, tail)
		if err != nil {
			return err
		}
	}
	if tail != nil {
		if err = validateAndroidTailscaleTransport(string(data), tailID, tailIdentity); err != nil {
			return err
		}
	}
	if line != nil {
		lineID = line.ID
		identity, err = lineRuntimeIdentity(profile, line)
		if err != nil {
			return err
		}
		address, err := preparation.resolveAndroidServerContext(parent, line.VPNServer, systemDNSJSON)
		if err != nil {
			return err
		}
		value := startVPNConfig(line.VPNServer, address, line.VPNUsername, line.VPNPassword, line.AllowInsecure)
		vpnConfig = &value
	}
	if parent.Err() != nil {
		return fmt.Errorf("Android switch cancelled")
	}
	return l.prepareSwitchContext(parent, string(data), lineID, identity, vpnConfig, tailID, tailIdentity, interfacesJSON, defaultName, defaultIndex)
}

// AndroidSwitchRequiresReconnect classifies the process-exclusive protocol
// boundary before preparing a candidate. The host persists the requested scene
// and fully reconnects only for this deliberate case; ordinary candidate errors
// leave the committed scene and traffic untouched.
func (l *Libbox) AndroidSwitchRequiresReconnect(profileJSON string) (bool, error) {
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return false, err
	}
	if _, err = config.BuildConnectionPlan(profile); err != nil {
		return false, err
	}
	anyIdentity, tailIdentity := "", ""
	if line := profile.ActiveVPNLine(); line != nil {
		anyIdentity, err = lineRuntimeIdentity(profile, line)
		if err != nil {
			return false, err
		}
	}
	tail, err := config.ActiveTailscaleLine(profile)
	if err != nil {
		return false, err
	}
	if tail != nil {
		tailIdentity, err = lineRuntimeIdentity(profile, tail)
		if err != nil {
			return false, err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running || l.cleaning || l.packetIngress == nil {
		return false, fmt.Errorf("Android connection is unavailable")
	}
	return (anyIdentity != "" && l.activeAnyConnectCapability != nil && anyIdentity != l.anyConnectRuntimeIdentity) ||
		(tailIdentity != "" && l.activeTailscaleCapability != nil && tailIdentity != l.tailscaleRuntimeIdentity), nil
}
