//go:build !windows

package libbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kafeifei/xdial/core/config"
)

// Offline metadata deliberately excludes source URLs, local paths and rule
// contents. CacheKey lets a file-picker reject a result for a changed source.
type offlineRuleInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	CacheKey  string `json:"cache_key"`
	State     string `json:"state"`
	Bytes     int64  `json:"bytes"`
	UpdatedAt int64  `json:"updated_at"`
}

func offlineRuleLocation(rule *config.RuleSet, basePath string) (path, storedFormat string, err error) {
	if basePath == "" || !filepath.IsAbs(basePath) || rule.Type != config.RuleSetTypeURL || rule.URL == "" {
		return "", "", fmt.Errorf("offline rule source unavailable")
	}
	inputFormat := neRuleSetFormat(rule)
	storedFormat = inputFormat
	if storedFormat == "text" {
		storedFormat = "source"
	}
	if storedFormat != "source" && storedFormat != "binary" {
		return "", "", fmt.Errorf("unsupported offline rule format")
	}
	path = neRuleSetCachePath(filepath.Join(basePath, "xdial-rule-sets"), "profile", rule.ID, rule.URL, inputFormat, storedFormat)
	return path, storedFormat, nil
}

// ListOfflineRuleSets never creates resources or contacts their source. Inactive
// rules can be inspected/imported by an explicit user action without activation.
func ListOfflineRuleSets(profileJSON, basePath string) (string, error) {
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return "", fmt.Errorf("offline rule profile invalid")
	}
	result := make([]offlineRuleInfo, 0)
	for i := range profile.RuleSets {
		rule := &profile.RuleSets[i]
		if rule.Type != config.RuleSetTypeURL {
			continue
		}
		path, format, err := offlineRuleLocation(rule, basePath)
		if err != nil {
			return "", err
		}
		item := offlineRuleInfo{ID: rule.ID, Name: rule.Name, Format: neRuleSetFormat(rule), CacheKey: filepath.Base(path), State: "missing"}
		_, _, info, cacheErr := readValidatedNERuleSet(path, format, maxNERuleSetBytes)
		if cacheErr == nil {
			item.State = "valid"
			item.Bytes, item.UpdatedAt = info.Size(), info.ModTime().Unix()
			if time.Since(info.ModTime()) > neRuleSetCacheFreshFor {
				item.State = "stale"
			}
		} else if !os.IsNotExist(cacheErr) {
			item.State = "invalid"
		}
		result = append(result, item)
	}
	data, err := json.Marshal(result)
	return string(data), err
}

func offlineRuleTarget(profileJSON, ruleID, cacheKey, basePath string) (*config.RuleSet, string, string, error) {
	profile, err := config.ParseRuntimeProfile([]byte(profileJSON))
	if err != nil {
		return nil, "", "", fmt.Errorf("offline rule profile invalid")
	}
	rule := profile.FindRuleSet(ruleID)
	if rule == nil {
		return nil, "", "", fmt.Errorf("offline rule no longer exists")
	}
	path, format, err := offlineRuleLocation(rule, basePath)
	if err != nil {
		return nil, "", "", err
	}
	if cacheKey == "" || filepath.Base(path) != cacheKey {
		return nil, "", "", fmt.Errorf("offline rule source changed")
	}
	return rule, path, format, nil
}

// ImportOfflineRuleSet uses exactly the runtime cache identity and validators.
// Invalid/oversized imports leave the old valid copy intact. The caller chooses
// the source explicitly; importing does not bind a rule or change live routing.
func ImportOfflineRuleSet(profileJSON, ruleID, cacheKey, basePath string, content []byte) error {
	rule, path, format, err := offlineRuleTarget(profileJSON, ruleID, cacheKey, basePath)
	if err != nil {
		return err
	}
	if len(content) == 0 || int64(len(content)) > maxNERuleSetBytes {
		return fmt.Errorf("offline rule exceeds size limit")
	}
	if neRuleSetFormat(rule) == "text" {
		content, err = convertTextRuleSet(content)
		if err != nil {
			return fmt.Errorf("offline text rule invalid")
		}
	}
	if _, err = validateNERuleSet(content, format, maxNERuleSetBytes); err != nil {
		return fmt.Errorf("offline rule content invalid")
	}
	if err = writeNERuleSetCache(path, content); err != nil {
		return fmt.Errorf("offline rule could not be saved")
	}
	return nil
}

// RemoveOfflineRuleSet removes only the selected hashed cache entry. It never
// deletes the declared source or any user file chosen by the platform picker.
func RemoveOfflineRuleSet(profileJSON, ruleID, cacheKey, basePath string) error {
	_, path, _, err := offlineRuleTarget(profileJSON, ruleID, cacheKey, basePath)
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("offline rule could not be removed")
	}
	return nil
}
