//go:build !windows

package libbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
)

func TestAndroidRefreshPreservesCacheOnFailureAndCancellation(t *testing.T) {
	for _, failure := range []string{"network", "invalid", "protocol", "cancelled", "generation"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.json")
			old := []byte(`{"version":3,"rules":[{"domain":["old.invalid"]}]}`)
			if err := writeNERuleSetCache(path, old); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			current := true
			calls := 0
			result := refreshAndroidRuleCopies(ctx, []pendingNERuleSetRefresh{{LocalPath: path, InputFormat: "source", StoredFormat: "source", FetchLineID: "specified"}}, func(item pendingNERuleSetRefresh) ([]byte, error) {
				calls++
				if item.FetchLineID != "specified" {
					t.Fatal("changed acquisition Line")
				}
				switch failure {
				case "network":
					return nil, errors.New("refused")
				case "protocol":
					return nil, config.ErrAndroidAcquisitionProtocolNeedsLease
				case "cancelled":
					cancel()
				case "generation":
					current = false
				}
				return []byte("invalid"), nil
			}, func() bool { return current })
			stored, err := os.ReadFile(path)
			if err != nil || string(stored) != string(old) {
				t.Fatal("failed refresh destroyed valid cache", err)
			}
			if calls != 1 || result.Updated != 0 || result.Failed+result.Deferred != 1 {
				t.Fatal(result, calls)
			}
		})
	}
}

func TestAndroidRefreshUsesOnlyStaleActiveRules(t *testing.T) {
	root := t.TempDir()
	profile, err := config.ParseRuntimeProfile([]byte(androidRemoteProfile))
	if err != nil {
		t.Fatal(err)
	}
	item := offlineTestInfo(t, androidRemoteProfile, root)
	content := []byte(`{"version":3,"rules":[{"domain":["old.invalid"]}]}`)
	if err = ImportOfflineRuleSet(androidRemoteProfile, item.ID, item.CacheKey, root, content); err != nil {
		t.Fatal(err)
	}
	pending, err := materializeNERuleSetsWithFetchForRule(profile, root, func(*config.RuleSet) neRuleSetFetcher { t.Fatal("warm cache tried to fetch"); return nil }, nil)
	if err != nil || len(pending) != 0 {
		t.Fatal("fresh copy scheduled network", err)
	}
	path := filepath.Join(root, "xdial-rule-sets", item.CacheKey)
	old := time.Now().Add(-48 * time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	profile, _ = config.ParseRuntimeProfile([]byte(androidRemoteProfile))
	pending, err = materializeNERuleSetsWithFetchForRule(profile, root, func(*config.RuleSet) neRuleSetFetcher { return nil }, nil)
	if err != nil || len(pending) != 1 {
		t.Fatal("stale cache did not prepare refresh", err)
	}
	updated := []byte(`{"version":3,"rules":[{"domain":["new.invalid"]}]}`)
	result := refreshAndroidRuleCopies(context.Background(), pending, func(pendingNERuleSetRefresh) ([]byte, error) { return updated, nil }, func() bool { return true })
	if result.Updated != 1 || result.Failed != 0 || result.Deferred != 0 {
		t.Fatal(result)
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(updated) {
		t.Fatal("new copy not saved", err)
	}
	profile, _ = config.ParseRuntimeProfile([]byte(androidRemoteProfile))
	profile.Scenarios[0].Bindings = nil
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	pending, err = materializeNERuleSetsWithFetchForRule(profile, root, func(*config.RuleSet) neRuleSetFetcher { t.Fatal("inactive rule fetched"); return nil }, nil)
	if err != nil || len(pending) != 0 {
		t.Fatal("inactive rule refreshed", err)
	}
}

func TestAndroidRefreshRequiresCommittedRuntime(t *testing.T) {
	if _, err := New(nil).RefreshAndroidRuleSets(androidRemoteProfile, `["192.0.2.53"]`, t.TempDir()); err == nil {
		t.Fatal("uncommitted refresh allowed")
	}
}

func TestAndroidStopCancelsAndRetiresBackgroundRefresh(t *testing.T) {
	root := t.TempDir()
	item := offlineTestInfo(t, androidRemoteProfile, root)
	content := []byte(`{"version":3,"rules":[{"domain":["old.invalid"]}]}`)
	if err := ImportOfflineRuleSet(androidRemoteProfile, item.ID, item.CacheKey, root, content); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "xdial-rule-sets", item.CacheKey)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	l := New(nil)
	p := &blockingPreparationProtector{entered: make(chan struct{}), release: make(chan struct{})}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	if err := l.StartStandalone(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { defer close(finished); l.RefreshAndroidRuleSets(androidRemoteProfile, `["192.0.2.53"]`, root) }()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		close(p.release)
		l.Stop()
		t.Fatal("refresh did not request protected DNS")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- l.Stop() }()
	close(p.release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh lease did not retire")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop blocked on retired refresh")
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(content) {
		t.Fatal("cancelled refresh overwrote cache", err)
	}
	if l.running {
		t.Fatal("refresh revived stopped core")
	}
}
