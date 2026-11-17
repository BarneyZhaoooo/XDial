//go:build !windows

package libbox

import (
	"encoding/json"
	"testing"

	"github.com/kafeifei/xdial/core/config"
)

func generateNESessionForTest(t *testing.T, profile config.Profile, basePath string) neSession {
	t.Helper()
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := GenerateNESession(string(profileJSON), "", basePath)
	if err != nil {
		t.Fatal(err)
	}
	var session neSession
	if err := json.Unmarshal([]byte(encoded), &session); err != nil {
		t.Fatal(err)
	}
	plain, err := GenerateNEConfig(string(profileJSON), "", basePath)
	if err != nil {
		t.Fatal(err)
	}
	if session.ConfigJSON != plain {
		t.Fatal("session config differs from GenerateNEConfig for the same profile")
	}
	return session
}

func TestGenerateNESessionNamesAnyConnectRuntimeIdentity(t *testing.T) {
	vpn := config.Line{
		ID: "corp", Type: config.LineTypeVPN, Enabled: true,
		VPNServer: "vpn.example.com", VPNUsername: "user", VPNPassword: "secret",
	}
	profile := config.Profile{
		ID:    "profile",
		Lines: []config.Line{{ID: "direct", Type: config.LineTypeDirect, Enabled: true}, vpn},
		Scenarios: []config.Scenario{
			{ID: "office", DefaultLineID: "corp"},
			{ID: "home", DefaultLineID: "direct"},
		},
		ActiveScenarioID: "office",
	}
	basePath := t.TempDir()
	office := generateNESessionForTest(t, profile, basePath)
	if office.AnyConnect == nil || office.AnyConnect.LineID != "corp" ||
		office.AnyConnect.RuntimeIdentity == "" ||
		office.AnyConnect.RuntimeIdentity != office.LineRuntimeIdentities["corp"] {
		t.Fatalf("office session lacks the AnyConnect runtime identity: %+v", office)
	}
	if office.Tailscale != nil {
		t.Fatalf("office session names a Tailscale Line: %+v", office.Tailscale)
	}

	// Two Scenarios over the same Line produce the same identity, which is
	// what lets an in-tunnel switch borrow the connected session.
	profile.Scenarios = append(profile.Scenarios, config.Scenario{ID: "office-split", DefaultLineID: "corp"})
	profile.ActiveScenarioID = "office-split"
	split := generateNESessionForTest(t, profile, basePath)
	if split.AnyConnect == nil || split.AnyConnect.RuntimeIdentity != office.AnyConnect.RuntimeIdentity {
		t.Fatalf("same AnyConnect Line changed identity across Scenarios: %+v vs %+v", split.AnyConnect, office.AnyConnect)
	}

	profile.ActiveScenarioID = "home"
	home := generateNESessionForTest(t, profile, basePath)
	if home.AnyConnect != nil {
		t.Fatalf("Direct Scenario names an AnyConnect Line: %+v", home.AnyConnect)
	}

	// Changed credentials are a different capability.
	profile.ActiveScenarioID = "office"
	profile.Lines[1].VPNPassword = "rotated"
	rotated := generateNESessionForTest(t, profile, basePath)
	if rotated.AnyConnect == nil || rotated.AnyConnect.RuntimeIdentity == office.AnyConnect.RuntimeIdentity {
		t.Fatal("rotated AnyConnect credentials kept the old runtime identity")
	}
}

func TestGenerateNESessionNamesTailscaleRuntimeIdentity(t *testing.T) {
	profile := config.Profile{
		ID: "profile",
		Lines: []config.Line{
			{ID: "direct", Type: config.LineTypeDirect, Enabled: true},
			{ID: "tailnet", Type: config.LineTypeTailscale, Enabled: true},
		},
		Scenarios:        []config.Scenario{{ID: "scenario", DefaultLineID: "tailnet"}},
		ActiveScenarioID: "scenario",
		Tailscale:        config.TailscaleIdentity{Hostname: "xdial-mobile"},
	}
	session := generateNESessionForTest(t, profile, t.TempDir())
	if session.Tailscale == nil || session.Tailscale.LineID != "tailnet" ||
		session.Tailscale.RuntimeIdentity == "" ||
		session.Tailscale.RuntimeIdentity != session.LineRuntimeIdentities["tailnet"] {
		t.Fatalf("session lacks the Tailscale runtime identity: %+v", session)
	}
}
