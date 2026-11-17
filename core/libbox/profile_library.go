package libbox

import (
	"encoding/json"
	"fmt"

	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/mobile"
	"github.com/kafeifei/xdial/core/subscription"
)

func NewProfileLibrary(profileJSON string) (string, error) {
	return config.NewProfileLibrary(profileJSON)
}
func ProfileLibraryRuntime(libraryJSON string) (string, error) {
	return config.ProfileLibraryRuntime(libraryJSON)
}
func ApplyProfileLibraryRuntime(libraryJSON, draftJSON string) (string, error) {
	return config.ApplyProfileLibraryRuntime(libraryJSON, draftJSON)
}
func ApplyProfileLibrarySelection(libraryJSON, draftJSON string) (string, error) {
	return config.ApplyProfileLibrarySelection(libraryJSON, draftJSON)
}
func ProfileLibrarySummary(libraryJSON string) (string, error) {
	return config.ProfileLibrarySummary(libraryJSON)
}
func SelectProfileLibrarySource(libraryJSON, sourceID string) (string, error) {
	return config.SelectProfileLibrarySource(libraryJSON, sourceID)
}
func RemoveProfileLibrarySource(libraryJSON, sourceID string) (string, error) {
	return config.RemoveProfileLibrarySource(libraryJSON, sourceID)
}
func ResolveProfileLibraryIdentityNamespace(libraryJSON, basePath, namespace string) (string, error) {
	return config.ResolveProfileLibraryIdentityNamespace(libraryJSON, basePath, namespace)
}
func CommitProfileLibraryPreview(libraryJSON, previewJSON string) (string, error) {
	return config.CommitProfileLibraryPreview(libraryJSON, previewJSON)
}

// PrepareProfileLibraryTailscaleSetup creates/reuses only the selected source's
// login line. The encrypted host commits the result before starting setup.
func PrepareProfileLibraryTailscaleSetup(raw string) (string, error) {
	summary, err := config.ProfileLibrarySummary(raw)
	if err != nil {
		return "", err
	}
	var selected struct {
		ID string `json:"editing_id"`
	}
	if err = json.Unmarshal([]byte(summary), &selected); err != nil {
		return "", err
	}
	runtime, err := config.ProfileLibraryRuntime(raw)
	if err != nil {
		return "", err
	}
	result, err := mobile.EnsureTailscaleSetupForSource(runtime, selected.ID)
	if err != nil {
		return "", err
	}
	var setup struct {
		Profile string `json:"profile"`
		LineID  string `json:"line_id"`
	}
	if err = json.Unmarshal([]byte(result), &setup); err != nil {
		return "", err
	}
	library, err := config.ApplyProfileLibraryRuntime(raw, setup.Profile)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(map[string]string{"library": library, "line_id": setup.LineID})
	return string(encoded), err
}

// PreviewNativeProfileImport accepts XDial/sing-box JSON only. Unlike the
// legacy all-format ImportProfile entrypoint it cannot expand remote providers
// through a host HTTP client. Referenced URL RuleSets remain declarations.
func PreviewNativeProfileImport(libraryJSON, content, name, sourceID string) (string, error) {
	return previewNativeProfileImport(libraryJSON, content, name, sourceID, "", "")
}
func ProfileLibraryRemoteSource(libraryJSON, sourceID string) (string, error) {
	return config.ProfileLibraryRemoteSource(libraryJSON, sourceID)
}
func previewNativeProfileImport(libraryJSON, content, name, sourceID, remoteURL, lineID string) (string, error) {
	var root map[string]json.RawMessage
	if len(content) > 2<<20 || json.Unmarshal([]byte(content), &root) != nil || (root["outbounds"] == nil && root["lines"] == nil && root["xdial"] == nil) {
		return "", fmt.Errorf("choose an XDial or sing-box JSON configuration")
	}
	updating := sourceID != ""
	var err error
	if !updating {
		sourceID, err = config.NewProfileSourceID()
		if err != nil {
			return "", err
		}
	}
	profile, err := config.ImportProfileDocument([]byte(content), sourceID)
	if err != nil {
		return "", err
	}
	if err = validateProfileNativeOptions(profile); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	if remoteURL != "" {
		return config.PreviewProfileLibraryRemoteImport(libraryJSON, string(encoded), name, sourceID, updating, remoteURL, lineID)
	}
	return config.PreviewProfileLibraryImport(libraryJSON, string(encoded), name, sourceID, updating)
}

func ValidateProfileSourceURL(rawURL string) error {
	return subscription.ValidateProfileSourceURL(rawURL)
}
