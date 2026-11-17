package libbox

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/subscription"
	"time"
)

// FetchAndroidProfilePreview owns one bounded protected acquisition lease and
// returns an offline CAS preview. It never applies imported policy or expands
// referenced providers. Stop/CancelPreparation cancel DNS, HTTP and response IO.
func (l *Libbox) FetchAndroidProfilePreview(libraryJSON, rawURL, lineID, name, sourceID, systemDNSJSON, basePath string) (string, error) {
	if err := subscription.ValidateProfileSourceURL(rawURL); err != nil {
		return "", err
	}
	if sourceID != "" {
		old, err := config.ProfileLibraryRemoteSource(libraryJSON, sourceID)
		if err != nil {
			return "", err
		}
		var s config.ProfileRemoteSource
		if json.Unmarshal([]byte(old), &s) != nil || s.URL != rawURL || s.FetchLineID != lineID || s.NodesOnly || (s.Format != "" && s.Format != "auto" && s.Format != "json") {
			return "", fmt.Errorf("source address, Line or format changed")
		}
	}
	runtime, err := config.ProfileLibraryRuntime(libraryJSON)
	if err != nil {
		return "", err
	}
	profile, err := config.ParseRuntimeProfile([]byte(runtime))
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	if l.running || l.startPreparing || l.cleaning {
		l.mu.Unlock()
		return "", fmt.Errorf("source fetch requires an idle runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	resource, err := l.newAndroidAcquisition(ctx, profile, lineID, systemDNSJSON, basePath)
	if err != nil {
		return "", fmt.Errorf("selected acquisition Line unavailable")
	}
	defer resource.instance.Close()
	content, err := resource.fetchResource(rawURL, 2<<20, true)
	if err != nil || ctx.Err() != nil {
		return "", fmt.Errorf("protected source download failed or cancelled")
	}
	preview, err := previewNativeProfileImport(libraryJSON, string(content), name, sourceID, rawURL, lineID)
	if ctx.Err() != nil {
		return "", fmt.Errorf("source download cancelled")
	}
	return preview, err
}
