//go:build !windows

package libbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func offlineTestInfo(t *testing.T, profile, root string) offlineRuleInfo {
	t.Helper()
	raw, err := ListOfflineRuleSets(profile, root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "https:") || strings.Contains(raw, root) {
		t.Fatal("offline metadata discloses source or path")
	}
	var entries []offlineRuleInfo
	if err = json.Unmarshal([]byte(raw), &entries); err != nil || len(entries) != 1 {
		t.Fatal("invalid offline metadata", err)
	}
	return entries[0]
}

func TestOfflineImportValidatesAndPreservesPreviousCopy(t *testing.T) {
	root := t.TempDir()
	item := offlineTestInfo(t, androidRemoteProfile, root)
	if item.State != "missing" {
		t.Fatal(item)
	}
	content := []byte(`{"version":3,"rules":[{"domain_suffix":["example.invalid"]}]}`)
	if err := ImportOfflineRuleSet(androidRemoteProfile, item.ID, item.CacheKey, root, content); err != nil {
		t.Fatal(err)
	}
	item = offlineTestInfo(t, androidRemoteProfile, root)
	if item.State != "valid" || item.Bytes != int64(len(content)) || item.UpdatedAt == 0 {
		t.Fatal(item)
	}
	for _, bad := range [][]byte{nil, []byte("not json"), []byte(`{"version":999,"rules":[]}`), make([]byte, maxNERuleSetBytes+1)} {
		if err := ImportOfflineRuleSet(androidRemoteProfile, item.ID, item.CacheKey, root, bad); err == nil {
			t.Fatal("invalid import accepted")
		}
	}
	path := filepath.Join(root, "xdial-rule-sets", item.CacheKey)
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(content) {
		t.Fatal("failed import destroyed previous copy", err)
	}
	if _, err = GenerateAndroidConfig(androidRemoteProfile, `["192.0.2.53"]`, root); err != nil {
		t.Fatal("offline copy not usable by runtime", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if offlineTestInfo(t, androidRemoteProfile, root).State != "stale" {
		t.Fatal("stale valid cache not distinguished")
	}
	if err = RemoveOfflineRuleSet(androidRemoteProfile, item.ID, item.CacheKey, root); err != nil {
		t.Fatal(err)
	}
	if offlineTestInfo(t, androidRemoteProfile, root).State != "missing" {
		t.Fatal("cache not removed")
	}
}

func TestOfflinePickerRejectsChangedSourceAndPathInjection(t *testing.T) {
	root := t.TempDir()
	item := offlineTestInfo(t, androidRemoteProfile, root)
	changed := strings.Replace(androidRemoteProfile, "list.json", "other.json", 1)
	for _, profile := range []string{changed, strings.Replace(androidRemoteProfile, `"format":"source"`, `"format":"binary"`, 1)} {
		if err := ImportOfflineRuleSet(profile, item.ID, item.CacheKey, root, []byte("{}")); err == nil {
			t.Fatal("stale picker source accepted")
		}
	}
	for _, key := range []string{"", "../untouched", root + "/untouched"} {
		if err := RemoveOfflineRuleSet(androidRemoteProfile, item.ID, key, root); err == nil {
			t.Fatal("unbound path accepted")
		}
	}
}

func TestOfflineTextConversionAndCorruptCache(t *testing.T) {
	root := t.TempDir()
	profile := strings.Replace(androidRemoteProfile, `"format":"source"`, `"format":"text"`, 1)
	item := offlineTestInfo(t, profile, root)
	if err := ImportOfflineRuleSet(profile, item.ID, item.CacheKey, root, []byte("DOMAIN-SUFFIX,example.invalid\nIP-CIDR,192.0.2.0/24")); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateAndroidConfig(profile, `["192.0.2.53"]`, root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "xdial-rule-sets", item.CacheKey)
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if offlineTestInfo(t, profile, root).State != "invalid" {
		t.Fatal("corruption reported as valid")
	}
}

func TestOfflineBinaryCopyAndInactiveRuleStayOffline(t *testing.T) {
	root := t.TempDir()
	profile := strings.Replace(androidRemoteProfile, `"format":"source"`, `"format":"binary"`, 1)
	// Browsing and importing an unbound resource do not activate its binding.
	profile = strings.Replace(profile, `"bindings":[{"rule_set_id":"rules","line_id":"direct"}]`, `"bindings":[]`, 1)
	item := offlineTestInfo(t, profile, root)
	if _, err := os.Stat(filepath.Join(root, "xdial-rule-sets")); !os.IsNotExist(err) {
		t.Fatal("browsing created resources")
	}
	content := encodeRuleSetBinary(t, []byte(`{"version":3,"rules":[{"domain_suffix":["example.invalid"]}]}`))
	if err := ImportOfflineRuleSet(profile, item.ID, item.CacheKey, root, content); err != nil {
		t.Fatal(err)
	}
	if offlineTestInfo(t, profile, root).State != "valid" {
		t.Fatal("valid SRS rejected")
	}
	data, err := GenerateAndroidConfig(profile, `["192.0.2.53"]`, root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(data, item.CacheKey) {
		t.Fatal("unbound imported rule became active")
	}
}
