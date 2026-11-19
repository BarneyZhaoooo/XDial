//go:build !windows

package libbox

import (
	"net/http"
	"strings"
	"testing"

	"sslcon/session"

	"github.com/kafeifei/xdial/core/engine"
)

// 用打过补丁的 sslcon 真实生成诊断，字段名一旦和这里的解析对不上就在这失败。
func TestAnyConnectSessionCloseSummaryNamesWhoClosedAndWhatWasNegotiated(t *testing.T) {
	header := http.Header{}
	header.Set("X-CSTP-DPD", "30")
	header.Set("X-CSTP-Keepalive", "20")
	header.Set("X-DTLS-DPD", "30")
	header.Set("X-DTLS-Keepalive", "20")
	header.Set("X-CSTP-Idle-Timeout", "180")
	header.Set("X-CSTP-Session-Timeout", "none")
	header.Set("X-CSTP-Rekey-Time", "86400")
	header.Set("X-CSTP-Rekey-Method", "ssl")
	cSess := (&session.Session{}).NewConnSession(&header)

	cSess.XDialRecordWrite("tls", 0x03, nil)
	cSess.XDialRecordFrame("tls", 0x04)
	cSess.XDialRecordFrame("tls", 0x00)
	// 服务器的终止帧：一个原因码加一段说明文字。
	cSess.XDialRecordPeerClose(
		"tls",
		"peer-terminate",
		append([]byte{0xb0}, []byte("Idle Timeout for user@vpn.example.com")...),
	)

	diagnostics := validAnyConnectDiagnostics(engine.AnyConnectSessionDiagnostics(cSess))
	if diagnostics == nil {
		t.Fatal("patched sslcon produced no diagnostics")
	}
	summary := anyConnectSessionCloseSummary(diagnostics)
	for _, want := range []string{
		"anyconnect session closed: code=peer-terminate channel=tls class=peer",
		"peer_reason=b0/idle-timeout",
		"negotiated(dpd=30 keepalive=20 dtls_dpd=30 dtls_keepalive=20 idle_timeout=180 session_timeout=none rekey=86400/ssl)",
		"tls(dpd_out=1 dpd_resp=1 peer_dpd=0 keepalive_in=0 data_in=1 data_out=0",
		"last_data_out=never",
		"dtls(ever=false up=false",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary lacks %q:\n%s", want, summary)
		}
	}
	// 服务器给的说明文字只归类，不外传。
	for _, leaked := range []string{"example.com", "user@", "Idle Timeout for"} {
		if strings.Contains(summary, leaked) {
			t.Fatalf("summary leaks %q:\n%s", leaked, summary)
		}
	}

	sink := &recordingDataPlaneLogSink{}
	reportAnyConnectSessionClose(sink, diagnostics)
	lines := sink.snapshot()
	if len(lines) != 1 || lines[0].level != "warn" ||
		lines[0].summary != summary || lines[0].detail != summary {
		t.Fatalf("sink received %+v", lines)
	}
}

func TestAnyConnectSessionCloseSummaryMarksAbsentNegotiation(t *testing.T) {
	cSess := (&session.Session{}).NewConnSession(&http.Header{})
	cSess.XDialCloseWithReason("tls-dpd-timeout", "tls", "liveness", "timeout")

	summary := anyConnectSessionCloseSummary(
		validAnyConnectDiagnostics(engine.AnyConnectSessionDiagnostics(cSess)),
	)
	for _, want := range []string{
		"code=tls-dpd-timeout channel=tls class=timeout",
		"idle_timeout=absent session_timeout=absent rekey=absent/-",
		"last_data_in=never",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary lacks %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "peer_reason=") {
		t.Fatalf("a local close has no peer reason:\n%s", summary)
	}
}

func TestAnyConnectSessionCloseSummaryRejectsFreeText(t *testing.T) {
	summary := anyConnectSessionCloseSummary([]byte(
		`{"close":{"code":"host vpn.example.com refused","channel":"tls","error_class":"peer"}}`,
	))
	if strings.Contains(summary, "example.com") ||
		!strings.Contains(summary, "code=invalid channel=tls class=peer") {
		t.Fatalf("summary = %s", summary)
	}
	if got := anyConnectSessionCloseSummary(nil); got != "" {
		t.Fatalf("empty diagnostics produced %q", got)
	}
	reportAnyConnectSessionClose(nil, []byte(`{"close":{"code":"tls-read-eof"}}`))
}
