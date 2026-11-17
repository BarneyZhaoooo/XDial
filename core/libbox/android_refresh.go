package libbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kafeifei/xdial/core/config"
)

type androidRefreshReport struct {
	Updated  int `json:"updated"`
	Failed   int `json:"failed"`
	Deferred int `json:"deferred"`
}

// RefreshAndroidRuleSets is called only after traffic readiness/commit. It
// keeps the router's loaded snapshot, retires with that runtime generation and
// never substitutes a failed acquisition Line. Results contain only counts.
func (l *Libbox) RefreshAndroidRuleSets(profileJSON, systemDNSJSON, basePath string) (string, error) {
	l.mu.Lock()
	if !l.running || l.cleaning || l.runtimeCtx == nil || l.switchCommitInProgress || l.switchPreparing || l.preparedSwitch != nil || l.androidSwitchCancel != nil || l.androidRefreshCancel != nil {
		l.mu.Unlock()
		return "", fmt.Errorf("rule refresh requires a committed connection")
	}
	parent, cancel := context.WithTimeout(l.runtimeCtx, 60*time.Second)
	l.androidRefreshCancel = cancel
	generation := l.boxGeneration
	l.runtimeUsers.Add(1)
	l.mu.Unlock()
	defer l.runtimeUsers.Done()
	defer func() { cancel(); l.mu.Lock(); l.androidRefreshCancel = nil; l.mu.Unlock() }()
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return "", fmt.Errorf("rule refresh profile invalid")
	}
	pending, err := materializeNERuleSetsWithFetchForRule(profile, basePath, func(*config.RuleSet) neRuleSetFetcher { return nil }, nil)
	if err != nil {
		return "", fmt.Errorf("rule refresh cache unavailable")
	}
	result := refreshAndroidRuleCopies(parent, pending, func(item pendingNERuleSetRefresh) ([]byte, error) {
		resource, err := l.newAndroidAcquisition(parent, profile, item.FetchLineID, systemDNSJSON, basePath)
		if err != nil {
			return nil, err
		}
		defer resource.instance.Close()
		return resource.fetch(item.URL, maxNERuleSetBytes)
	}, func() bool { return l.probeGenerationIsCurrent(generation) })
	data, err := json.Marshal(result)
	return string(data), err
}

func refreshAndroidRuleCopies(ctx context.Context, pending []pendingNERuleSetRefresh, fetch func(pendingNERuleSetRefresh) ([]byte, error), current func() bool) androidRefreshReport {
	var result androidRefreshReport
	for index, item := range pending {
		if ctx.Err() != nil || !current() {
			result.Deferred += len(pending) - index
			break
		}
		content, err := fetch(item)
		if errors.Is(err, config.ErrAndroidAcquisitionProtocolNeedsLease) {
			result.Deferred++
			continue
		}
		if err == nil && item.InputFormat == "text" {
			content, err = convertTextRuleSet(content)
		}
		if err == nil {
			_, err = validateNERuleSet(content, item.StoredFormat, maxNERuleSetBytes)
		}
		if ctx.Err() != nil || !current() {
			result.Deferred += len(pending) - index
			break
		}
		if err == nil {
			err = writeNERuleSetCache(item.LocalPath, content)
		}
		if err != nil {
			result.Failed++
		} else {
			result.Updated++
		}
	}
	return result
}
