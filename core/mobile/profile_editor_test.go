package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeVPNEntryPreservesImportedConfiguration(t *testing.T) {
	result, err := AddAnyConnectScenario(testProfile, "Work", "vpn.example.invalid:443", "test-user", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal([]byte(result), &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["extension_metadata"]) != "{\"keep\":true}" || string(doc["mobile_automatic"]) != "false" {
		t.Fatal("metadata or intent lost")
	}
	var lines []map[string]any
	json.Unmarshal(doc["lines"], &lines)
	if len(lines) != 2 || lines[1]["type"] != "vpn" || lines[1]["allow_insecure"] == true {
		t.Fatal("incorrect or insecure entry")
	}
	if !strings.Contains(result, "test-password") {
		t.Fatal("host must receive credentials for secure storage")
	}
}
func TestNativeVPNEntryRejectsUnsafeOrMalformedServer(t *testing.T) {
	for _, server := range []string{"http://vpn.example.invalid", "https://user:password@vpn.example.invalid", "https://vpn.example.invalid/path", "https://vpn.example.invalid?q=x", "vpn.example.invalid:invalid"} {
		if _, err := AddAnyConnectScenario("", "Work", server, "user", "secret"); err == nil {
			t.Fatalf("accepted %s", server)
		}
	}
}
