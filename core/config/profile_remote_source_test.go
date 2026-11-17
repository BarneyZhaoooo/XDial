package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRemoteProfilePreviewPinsSourceAndKeepsGlobalEdits(t *testing.T) {
	base, err := NewProfileLibrary(libraryLegacy)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ImportProfileDocument([]byte(libraryIncoming), "online")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(p)
	preview, err := PreviewProfileLibraryRemoteImport(base, string(data), "Online", "online", false, "https://config.example/profile?token=fixture", "direct")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := CommitProfileLibraryPreview(base, preview)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := ProfileLibraryRemoteSource(stored, "online")
	if err != nil || !strings.Contains(metadata, `"fetchLineID":"direct"`) {
		t.Fatal(metadata, err)
	}
	summary, _ := ProfileLibrarySummary(stored)
	if strings.Contains(summary, "token") || strings.Contains(summary, "config.example") || !strings.Contains(summary, `"remote":true`) {
		t.Fatal("source summary leaked address or lost kind")
	}
	if _, err = PreviewProfileLibraryImport(stored, string(data), "Online", "online", true); err == nil {
		t.Fatal("local file forged remote provenance")
	}
	for _, pair := range [][2]string{{"https://changed.example/profile", "direct"}, {"https://config.example/profile?token=fixture", "vpn"}} {
		if _, err = PreviewProfileLibraryRemoteImport(stored, string(data), "Online", "online", true, pair[0], pair[1]); err == nil {
			t.Fatal("changed origin accepted")
		}
	}
	l, _ := decodeLibrary(stored)
	l.Scenarios[1]["name"] = libraryJSON("User name")
	l.Scenarios[1]["default_line_id"] = libraryJSON("direct")
	edited, err := encodeLibrary(l)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = PreviewProfileLibraryRemoteImport(edited, string(data), "Online", "online", true, "https://config.example/profile?token=fixture", "direct")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := CommitProfileLibraryPreview(edited, preview)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := decodeLibrary(updated)
	if libraryString(after.Scenarios[1], "name") != "User name" || libraryString(after.Scenarios[1], "default_line_id") != "direct" || after.ActiveScenarioID != "work" {
		t.Fatal("refresh replaced user choices")
	}
	if _, err = CommitProfileLibraryPreview(updated, preview); err == nil {
		t.Fatal("stale preview accepted")
	}
}
func TestRemoteSourceAcquisitionLineIsAStoredDependency(t *testing.T) {
	base, _ := NewProfileLibrary(libraryLegacy)
	p, _ := ImportProfileDocument([]byte(libraryIncoming), "online")
	data, _ := json.Marshal(p)
	preview, err := PreviewProfileLibraryRemoteImport(base, string(data), "Online", "online", false, "https://config.example/profile", "vpn")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := CommitProfileLibraryPreview(base, preview)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := decodeLibrary(stored)
	l.Profiles[0].Profile["lines"] = libraryJSON([]libraryObject{{"id": libraryJSON("direct"), "type": libraryJSON("direct"), "enabled": libraryJSON(true)}})
	l.Scenarios[0]["default_line_id"] = libraryJSON("direct")
	if _, err = encodeLibrary(l); err == nil {
		t.Fatal("removed acquisition dependency")
	}
}
