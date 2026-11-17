//go:build !windows

package libbox

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/kafeifei/xdial/core/config"
	"github.com/kafeifei/xdial/core/subscription"
)

func lineFetchProfileJSON(t *testing.T, lines ...config.Line) string {
	t.Helper()
	profile := config.Profile{
		Lines:            lines,
		Scenarios:        []config.Scenario{{ID: "traffic", Name: "Traffic", DefaultLineID: "direct"}},
		ActiveScenarioID: "traffic",
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The local Shadowsocks-none fixture only records the destination it is asked
// to reach and then refuses; no DNS or HTTPS packet leaves the test machine.
// It proves the Apple path resolves and connects through the chosen Line with
// no socket protector installed, and that failure does not fall back to Direct.
func TestImportProfileSourceViaLineUsesSelectedLine(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	targets := make(chan string, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				kind := make([]byte, 1)
				if _, err := io.ReadFull(conn, kind); err != nil || kind[0] != 1 {
					return
				}
				address := make([]byte, 6)
				if _, err := io.ReadFull(conn, address); err != nil {
					return
				}
				select {
				case targets <- fmt.Sprintf("%s:%d", net.IP(address[:4]), binary.BigEndian.Uint16(address[4:])):
				default:
				}
			}()
		}
	}()
	profileJSON := lineFetchProfileJSON(t, config.Line{
		ID: "source", Name: "Source", Type: config.LineTypeShadowsocks, Enabled: true,
		SSServer: "127.0.0.1", SSPort: listener.Addr().(*net.TCPAddr).Port, SSMethod: "none", SSPass: "fixture",
	})
	l := New(nil)
	_, err = l.ImportProfileSourceViaLine(profileJSON, "https://profiles.example.invalid/p.json", "source", `["192.0.2.53"]`, t.TempDir(), "imported", false)
	if err == nil {
		t.Fatal("refusing Line produced a document")
	}
	select {
	case target := <-targets:
		if target != "1.1.1.1:443" {
			t.Fatalf("source DNS did not go through the selected Line's resolver: %s", target)
		}
	default:
		t.Fatal("source fetch bypassed the selected Line")
	}
	if l.running || l.startPreparing {
		t.Fatal("source fetch left runtime state behind")
	}
}

func TestImportProfileSourceViaLineRefusesProtocolOwnedLines(t *testing.T) {
	for _, kind := range []config.LineType{config.LineTypeVPN, config.LineTypeTailscale} {
		profileJSON := lineFetchProfileJSON(t, config.Line{
			ID: "protocol", Name: "Protocol", Type: kind, Enabled: true,
			VPNServer: "vpn.invalid", TailscaleExitNode: "100.64.0.1",
		})
		_, err := New(nil).ImportProfileSourceViaLine(profileJSON, "https://profiles.example.invalid/p.json", "protocol", `["192.0.2.53"]`, t.TempDir(), "imported", false)
		if !errors.Is(err, ErrProfileSourceLineUnsupported) {
			t.Fatalf("%s Line was not refused: %v", kind, err)
		}
	}
}

func TestImportProfileSourceViaLineRejectsUnsafeInput(t *testing.T) {
	profileJSON := lineFetchProfileJSON(t)
	for _, tc := range []struct{ url, line string }{
		{"http://profiles.example.invalid/p.json", "direct"},
		{"https://user:pass@profiles.example.invalid/p.json", "direct"},
		{"https://127.0.0.1/p.json", "direct"},
		{"https://profiles.example.invalid/p.json", ""},
		{"https://profiles.example.invalid/p.json", "missing"},
	} {
		if _, err := New(nil).ImportProfileSourceViaLine(profileJSON, tc.url, tc.line, `["192.0.2.53"]`, t.TempDir(), "imported", false); err == nil {
			t.Fatalf("accepted %s via %q", tc.url, tc.line)
		}
	}
}

// Only non-native documents expand rule sources, and only through the caller's
// path: a Line-fetched Surge/Clash source must not reach the default network.
func TestImportProfileDocumentExpandsOnlyThroughCallerPath(t *testing.T) {
	surge := "[Proxy]\nJP = ss, jp.example.com, 8388, encrypt-method=aes-128-gcm, password=fixture\n\n[Proxy Group]\nManual = select, JP\n\n[Rule]\nRULE-SET,https://rules.example.com/list,Manual\nFINAL,Manual\n"
	calls := 0
	expand := func(parsed *subscription.ParseResult) error {
		calls++
		return subscription.ExpandRulesetsStrictWithFetch(context.Background(), parsed, func(context.Context, string, int64) ([]byte, error) {
			return []byte("DOMAIN-SUFFIX,example.org\n"), nil
		})
	}
	if _, err := importProfileDocument(surge, "auto", "imported", false, expand); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("rule sources were not expanded through the caller path: %d", calls)
	}
	if _, err := importProfileDocument(surge, "auto", "imported", true, expand); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("lines-only import expanded rule sources")
	}
}
