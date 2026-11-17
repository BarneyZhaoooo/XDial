package config

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Compatible with the native schema3 ProfileSource. An absent fetchLineID on
// old records means Direct; it is explicit and immutable on new mobile imports.
type ProfileRemoteSource struct {
	URL             string  `json:"url"`
	FetchLineID     string  `json:"fetchLineID"`
	NodesOnly       bool    `json:"nodesOnly"`
	Format          string  `json:"format"`
	RefreshInterval float64 `json:"refreshInterval"`
}

func profileRemoteSource(raw json.RawMessage) (ProfileRemoteSource, error) {
	var source ProfileRemoteSource
	if json.Unmarshal(raw, &source) != nil || source.URL == "" {
		return source, fmt.Errorf("source has no remote address")
	}
	if source.FetchLineID == "" {
		source.FetchLineID = "direct"
	}
	return source, nil
}
func ProfileLibraryRemoteSource(raw, id string) (string, error) {
	l, err := decodeLibrary(raw)
	if err != nil {
		return "", err
	}
	for _, r := range l.Profiles {
		if r.ID == id {
			s, err := profileRemoteSource(r.Source)
			if err != nil {
				return "", err
			}
			return string(libraryJSON(s)), nil
		}
	}
	return "", fmt.Errorf("source unavailable")
}
func PreviewProfileLibraryRemoteImport(raw, importedJSON, name, sourceID string, updating bool, remoteURL, lineID string) (string, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil || len(remoteURL) > 4096 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || lineID == "" {
		return "", fmt.Errorf("invalid remote source")
	}
	return previewProfileLibraryImport(raw, importedJSON, name, sourceID, updating, &ProfileRemoteSource{URL: remoteURL, FetchLineID: lineID, Format: "auto", RefreshInterval: 86400})
}
