package libbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/subscription"
)

// MaxLineProfileSourceBytes matches the Apple URLSession importer's bound.
const MaxLineProfileSourceBytes int64 = 4 << 20

// The body and strict rule-set expansion share one bound on the Line session.
const lineProfileSourceTimeout = 60 * time.Second

// ErrProfileSourceLineUnsupported marks Lines whose protocol owns a process
// identity (Tailscale, AnyConnect). They need the shared session lease before a
// resource fetch may run beside traffic; never replace them with Direct.
var ErrProfileSourceLineUnsupported = errors.New("profile source Line type cannot fetch resources yet")

// ImportProfileSourceViaLine downloads one profile source through exactly
// lineID of the runtime profile and imports it, returning the Profile JSON that
// ImportProfile would return. DNS, HTTPS, redirects, the body and every
// referenced rule source use that Line; failure never falls back to Direct.
//
// Unlike FetchAndroidProfilePreview no socket protector is installed: Apple
// calls this either from the app while no XDial capture exists, or inside the
// transparent-proxy provider, whose own sockets XDial never captures. The
// session keeps the OS Underlay routing. It never touches the caller's runtime;
// CancelPreparation stops DNS, HTTP and response IO.
func (l *Libbox) ImportProfileSourceViaLine(profileJSON, rawURL, lineID, systemDNSJSON, basePath, namespace string, nodesOnly bool) (string, error) {
	if err := subscription.ValidateProfileSourceURL(rawURL); err != nil {
		return "", err
	}
	if lineID == "" {
		return "", fmt.Errorf("profile source Line is missing")
	}
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return "", err
	}
	var servers []string
	if json.Unmarshal([]byte(systemDNSJSON), &servers) != nil {
		return "", fmt.Errorf("invalid system DNS snapshot")
	}
	data, err := config.GenerateAppleResourceAcquisition(profile, lineID, basePath, servers)
	if errors.Is(err, config.ErrAndroidAcquisitionProtocolNeedsLease) {
		return "", ErrProfileSourceLineUnsupported
	}
	if err != nil {
		return "", err
	}
	outboundTag, resolverTag, err := resourceAcquisitionTags(data)
	if err != nil {
		return "", err
	}

	l.mu.Lock()
	if l.running || l.startPreparing || l.cleaning {
		l.mu.Unlock()
		return "", fmt.Errorf("source fetch requires an idle runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), lineProfileSourceTimeout)
	l.startPreparing = true
	l.startPreparationCancel = cancel
	l.startPreparationUsers.Add(1)
	l.mu.Unlock()
	defer func() {
		cancel()
		l.mu.Lock()
		l.startPreparing = false
		l.startPreparationCancel = nil
		l.mu.Unlock()
		l.startPreparationUsers.Done()
	}()

	resource, err := newResourceBox(boxContext(ctx), data, outboundTag, resolverTag)
	if err != nil {
		return "", fmt.Errorf("selected source Line unavailable")
	}
	defer resource.instance.Close()
	content, err := resource.fetchResource(rawURL, MaxLineProfileSourceBytes, true)
	if err != nil || ctx.Err() != nil {
		return "", fmt.Errorf("source download through the selected Line failed or was cancelled")
	}
	imported, err := importProfileDocument(string(content), "auto", namespace, nodesOnly, func(parsed *subscription.ParseResult) error {
		return subscription.ExpandRulesetsStrictWithFetch(ctx, parsed, func(_ context.Context, url string, limit int64) ([]byte, error) {
			return resource.fetchResource(url, limit, false)
		})
	})
	if ctx.Err() != nil {
		return "", fmt.Errorf("source import through the selected Line was cancelled")
	}
	return imported, err
}

func resourceAcquisitionTags(data []byte) (outboundTag, resolverTag string, err error) {
	var target struct {
		DNS struct {
			Final string `json:"final"`
		} `json:"dns"`
		Route struct {
			Final string `json:"final"`
		} `json:"route"`
	}
	if json.Unmarshal(data, &target) != nil || target.DNS.Final == "" || target.Route.Final == "" {
		return "", "", fmt.Errorf("resource Line has no exact transport")
	}
	return target.Route.Final, target.DNS.Final, nil
}
