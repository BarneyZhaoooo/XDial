package config

import (
	"encoding/json"
	"testing"
)

func TestAndroidUsesCapturedDNSWithoutChangingApple(t *testing.T) {
	profile := &Profile{Lines: []Line{{ID: "direct", Type: LineTypeDirect, Enabled: true}}, Scenarios: []Scenario{{ID: "test", DefaultLineID: "direct"}}, ActiveScenarioID: "test"}
	android, err := GenerateSingBoxAndroid(profile, t.TempDir(), []string{"192.0.2.53", "192.0.2.54"})
	if err != nil {
		t.Fatal(err)
	}
	apple, err := GenerateSingBoxFor(profile, 0, "", PlatformNE, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data []byte
		want string
	}{{android, "192.0.2.53"}, {apple, "1.1.1.1"}} {
		var doc struct {
			DNS struct {
				Servers []struct {
					Tag    string `json:"tag"`
					Server string `json:"server"`
				} `json:"servers"`
			} `json:"dns"`
		}
		if err := json.Unmarshal(tc.data, &doc); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, server := range doc.DNS.Servers {
			if server.Tag == MobilePublicDNSTag {
				found = true
				if server.Server != tc.want {
					t.Fatalf("resolver=%s want %s", server.Server, tc.want)
				}
			}
		}
		if !found {
			t.Fatal("fallback transport missing")
		}
	}
	for _, servers := range [][]string{nil, {}, {"not-an-ip"}, {"192.0.2.53", "0.0.0.0"}} {
		if _, err := GenerateSingBoxAndroid(profile, t.TempDir(), servers); err == nil {
			t.Fatalf("accepted invalid DNS snapshot %v", servers)
		}
	}
}
