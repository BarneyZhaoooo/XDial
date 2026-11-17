//go:build !windows

package libbox

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/kafeifei/xdial/core/config"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
)

// StartAndroidSession selects the effective protocol from the shared Profile.
// The host supplies TUN/protection/interface facts before calling it. Credentials
// remain in memory; no platform UI duplicates active-Line selection rules.
func (l *Libbox) StartAndroidSession(profileJSON, configJSON, systemDNSJSON string) error {
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return err
	}
	if _, err = config.BuildConnectionPlan(profile); err != nil {
		return err
	}
	line := profile.ActiveVPNLine()
	if line == nil {
		return l.StartStandalone(configJSON)
	}
	if strings.TrimSpace(line.VPNServer) == "" || strings.TrimSpace(line.VPNUsername) == "" || line.VPNPassword == "" {
		return fmt.Errorf("AnyConnect credentials are missing")
	}
	address, err := l.resolveAndroidServer(line.VPNServer, systemDNSJSON)
	if err != nil {
		return err
	}
	return l.StartResolvedWithInsecure(line.VPNServer, address, line.VPNUsername, line.VPNPassword, line.AllowInsecure, configJSON)
}

// The VPN server is a transport endpoint on the captured Underlay, not user
// traffic. Its bootstrap lookup uses a separate sing-box Direct DNS transport,
// with the same protected socket contract and OS DNS snapshot. No TUN or user
// route is installed by this temporary Box; the sole traffic reader is untouched.
func (l *Libbox) resolveAndroidServer(server, systemDNSJSON string) (string, error) {
	host, err := serverHostname(server)
	if err != nil {
		return "", err
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Is4() {
			return address.String(), nil
		}
		return "", fmt.Errorf("server endpoint is not IPv4")
	}
	var servers []string
	if err = json.Unmarshal([]byte(systemDNSJSON), &servers); err != nil {
		return "", fmt.Errorf("invalid system DNS snapshot")
	}
	profile, err := config.ParseRuntimeProfile([]byte(`{"lines":[{"id":"direct","type":"direct","enabled":true}],"scenarios":[{"id":"bootstrap","default_line_id":"direct"}],"active_scenario_id":"bootstrap"}`))
	if err != nil {
		return "", err
	}
	data, err := config.GenerateSingBoxAndroid(profile, "", servers)
	if err != nil {
		return "", err
	}
	parent, cancel := context.WithTimeout(context.Background(), serverAddressResolutionTimeout)
	defer cancel()
	ctx := boxContext(parent)
	l.mu.Lock()
	platform := l.platform.lineRuntimeCapability()
	l.mu.Unlock()
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, platform)
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		return "", err
	}
	options.Inbounds = nil
	options.Experimental = nil
	options.Log = &option.LogOptions{Disabled: true}
	instance, err := box.New(box.Options{Options: options, Context: ctx})
	if err != nil {
		return "", err
	}
	defer instance.Close()
	if err = instance.Start(); err != nil {
		return "", fmt.Errorf("bootstrap DNS unavailable")
	}
	router := service.FromContext[adapter.DNSRouter](ctx)
	transports := service.FromContext[adapter.DNSTransportManager](ctx)
	if router == nil || transports == nil {
		return "", fmt.Errorf("bootstrap resolver unavailable")
	}
	transport, found := directLineResolver(transports)
	if !found {
		return "", fmt.Errorf("bootstrap transport unavailable")
	}
	addresses, err := router.Lookup(ctx, host, adapter.DNSQueryOptions{Transport: transport, Strategy: C.DomainStrategyIPv4Only})
	if err != nil {
		return "", fmt.Errorf("server IPv4 lookup failed")
	}
	for _, address := range addresses {
		if address.Is4() {
			return address.String(), nil
		}
	}
	return "", fmt.Errorf("server endpoint has no IPv4 address")
}
