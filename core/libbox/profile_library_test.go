//go:build !windows

package libbox

import (
	"encoding/json"
	"github.com/kafeifei/xdial/core/config"
	"strings"
	"testing"
)

func TestNativeLibraryPreviewKeepsRemoteRulesAsDeclarations(t *testing.T) {
	library, err := NewProfileLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	// This URL is deliberately unreachable. Preview must not try to fetch it.
	content := `{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rule_set":[{"type":"remote","tag":"r","format":"binary","url":"https://rules.example.invalid/r.srs"}],"rules":[{"rule_set":"r","outbound":"direct"}],"final":"direct"}}`
	preview, err := PreviewNativeProfileImport(library, content, "Local fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Library string `json:"library"`
	}
	if json.Unmarshal([]byte(preview), &p) != nil || !strings.Contains(p.Library, "https://rules.example.invalid/r.srs") {
		t.Fatal("remote declaration lost")
	}
	if _, err = PreviewNativeProfileImport(library, "[Rule]\nRULE-SET,https://example.invalid/rules,DIRECT", "Unsupported", ""); err == nil {
		t.Fatal("legacy provider-expanding import admitted")
	}
}

func TestNativeLibraryTailscaleSetupUsesEditingSource(t *testing.T) {
	library, err := NewProfileLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	var first, second struct {
		Library string `json:"library"`
		LineID  string `json:"line_id"`
	}
	result, err := PrepareProfileLibraryTailscaleSetup(library)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(result), &first)
	preview, err := PreviewNativeProfileImport(first.Library, `{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`, "Second", "")
	if err != nil {
		t.Fatal(err)
	}
	library, err = CommitProfileLibraryPreview(first.Library, preview)
	if err != nil {
		t.Fatal(err)
	}
	result, err = PrepareProfileLibraryTailscaleSetup(library)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(result), &second)
	if first.LineID == second.LineID {
		t.Fatal("new source reused another source's setup line")
	}
	runtime, err := ProfileLibraryRuntime(second.Library)
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ParseRuntimeProfile([]byte(runtime))
	if err != nil {
		t.Fatal(err)
	}
	if p.FindLine(first.LineID).IdentityProfileID == p.FindLine(second.LineID).IdentityProfileID {
		t.Fatal("source identities collapsed")
	}
	repeated, err := PrepareProfileLibraryTailscaleSetup(second.Library)
	if err != nil {
		t.Fatal(err)
	}
	var again struct {
		LineID string `json:"line_id"`
	}
	json.Unmarshal([]byte(repeated), &again)
	if again.LineID != second.LineID {
		t.Fatal("source setup duplicated its own identity")
	}
}
