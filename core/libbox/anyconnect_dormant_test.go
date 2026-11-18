//go:build !windows

package libbox

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"sslcon/session"
)

func TestAnyConnectSessionEndedIdle(t *testing.T) {
	tests := []struct {
		name        string
		diagnostics string
		want        bool
	}{
		{
			// The line recorded on the machine that showed the reconnect loop.
			name: "server disconnects a minute after the last data frame",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{"last_data_received_age_ms":72400,"last_data_written_age_ms":193700},
				"dtls":{},"close":{"code":"peer-disconnect"}}`,
			want: true,
		},
		{
			name: "data moved inside the idle window",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{"last_data_received_age_ms":5000,"last_data_written_age_ms":193700},
				"dtls":{},"close":{"code":"peer-disconnect"}}`,
		},
		{
			name: "data on the DTLS channel counts",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{"last_data_received_age_ms":90000},
				"dtls":{"last_data_written_age_ms":1000},"close":{"code":"peer-terminate"}}`,
		},
		{
			name: "no data at all for longer than the timeout",
			diagnostics: `{"session_age_ms":61000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{},"dtls":{},"close":{"code":"peer-disconnect"}}`,
			want: true,
		},
		{
			name: "session younger than the timeout",
			diagnostics: `{"session_age_ms":30000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{},"dtls":{},"close":{"code":"peer-disconnect"}}`,
		},
		{
			name: "server names the idle timeout itself",
			diagnostics: `{"session_age_ms":5000,"negotiated":{},"tls":{},"dtls":{},
				"close":{"code":"peer-terminate","peer_reason_category":"idle-timeout"}}`,
			want: true,
		},
		{
			name: "server announced no idle timeout",
			diagnostics: `{"session_age_ms":200000,"negotiated":{},"tls":{},"dtls":{},
				"close":{"code":"peer-disconnect"}}`,
		},
		{
			name: "idle timeout disabled",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":0},
				"tls":{},"dtls":{},"close":{"code":"peer-disconnect"}}`,
		},
		{
			name: "server gives another reason",
			diagnostics: `{"session_age_ms":5000,"negotiated":{},"tls":{},"dtls":{},
				"close":{"code":"peer-terminate","peer_reason_category":"duplicate-session"}}`,
		},
		{
			name: "the client declared the peer dead",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{},"dtls":{},"close":{"code":"tls-dpd-timeout"}}`,
		},
		{
			name: "the connection ended without a disconnect frame",
			diagnostics: `{"session_age_ms":200000,"negotiated":{"cstp_idle_timeout_seconds":60},
				"tls":{},"dtls":{},"close":{"code":"tls-read-eof"}}`,
		},
		{name: "no close recorded", diagnostics: `{"session_age_ms":200000}`},
		{name: "no diagnostics", diagnostics: ``},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := anyConnectSessionEndedIdle([]byte(test.diagnostics)); got != test.want {
				t.Fatalf("ended idle = %t, want %t", got, test.want)
			}
		})
	}
}

// newIdleCapableSession is a session as the patched sslcon builds it, from a
// server that announces an idle timeout.
func newIdleCapableSession() *session.ConnSession {
	header := http.Header{}
	header.Set("X-CSTP-DPD", "60")
	header.Set("X-CSTP-Keepalive", "300")
	header.Set("X-CSTP-Idle-Timeout", "60")
	return (&session.Session{}).NewConnSession(&header)
}

// endSessionForIdleness is the server applying its idle policy: a disconnect
// frame that says so. (The timing rule is covered above; a test cannot sit
// idle for the timeout.)
func endSessionForIdleness(t *testing.T, l *Libbox) {
	t.Helper()
	sess := l.activeAnyConnectCapability.snapshot().session
	if sess == nil {
		t.Fatal("no live AnyConnect session to end")
	}
	sess.XDialRecordPeerClose("tls", "peer-disconnect", append([]byte{0x01}, "Idle Timeout"...))
}

func startIdleCapableAnyConnect(t *testing.T, vpn *gatedRecoveryVPNClient) (*Libbox, *gateTestCallback, *recordingDataPlaneLogSink) {
	t.Helper()
	vpn.newSession = newIdleCapableSession
	l, cb := startGatedAnyConnect(t, vpn)
	sink := &recordingDataPlaneLogSink{}
	l.SetDataPlaneLogSink(sink)
	return l, cb, sink
}

func dialThroughLine(line *anyConnectLineRuntime) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := line.DialUDP(ctx, nil, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 53), Port: 53})
	if conn != nil {
		_ = conn.Close()
	}
	return err
}

func TestAnyConnectIdleDisconnectLeavesLineDormantUntilUsed(t *testing.T) {
	vpn := &gatedRecoveryVPNClient{}
	l, cb, sink := startIdleCapableAnyConnect(t, vpn)
	capability := l.activeAnyConnectCapability
	line := capability.snapshot().line

	endSessionForIdleness(t, l)

	eventually(t, "line reported idle", func() bool { return cb.has(`"line_idle"`) })
	stays(t, "dormant line left alone", 100*time.Millisecond, func() bool {
		return vpn.count() == 1 && !line.available() && capability.available() &&
			l.IsRunning() && cb.errorCount() == 0 && !cb.has("line_reconnecting")
	})
	if snapshot := capability.snapshot(); !snapshot.dormant || snapshot.session != nil || snapshot.bridge != nil {
		t.Fatalf("dormant capability = %+v", snapshot)
	}

	// The first use re-establishes the session and proceeds over it.
	if err := dialThroughLine(line); err != nil {
		t.Fatalf("first use of a dormant line: %v", err)
	}
	if vpn.count() != 2 {
		t.Fatalf("connects = %d, want exactly one on demand", vpn.count())
	}
	if !line.available() || capability.snapshot().dormant {
		t.Fatal("line did not come back after its first use")
	}
	if !cb.has("line_reconnecting") || !cb.has("line_connected") {
		t.Fatalf("host was not told about the on-demand reconnect: %v", cb.status)
	}
	if cb.errorCount() != 0 || !l.IsRunning() {
		t.Fatal("waking a dormant line disturbed the data plane")
	}

	lines := sink.snapshot()
	var summaries []string
	for _, line := range lines {
		summaries = append(summaries, line.summary)
	}
	for _, want := range []string{"anyconnect session closed: code=peer-disconnect", "anyconnect line dormant", "anyconnect line waking"} {
		found := false
		for _, summary := range summaries {
			if strings.HasPrefix(summary, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("data-plane log lacks %q: %q", want, summaries)
		}
	}

	// The re-established session is monitored like any other.
	endSessionForIdleness(t, l)
	eventually(t, "second idle disconnect parks again", func() bool {
		return capability.snapshot().dormant
	})
	if vpn.count() != 2 {
		t.Fatalf("connects = %d after the second idle disconnect", vpn.count())
	}
}

// Any other end of the session is a failure of the Line and is repaired at
// once, whether or not the server announced an idle timeout.
func TestAnyConnectOtherSessionEndsStillReconnectImmediately(t *testing.T) {
	vpn := &gatedRecoveryVPNClient{}
	l, cb, _ := startIdleCapableAnyConnect(t, vpn)
	capability := l.activeAnyConnectCapability

	capability.snapshot().session.XDialCloseWithReason("tls-dpd-timeout", "tls", "liveness", "timeout")

	eventually(t, "immediate reconnect", func() bool {
		return vpn.count() == 2 && capability.snapshot().session != nil
	})
	if capability.snapshot().dormant || cb.has(`"line_idle"`) {
		t.Fatal("a dead peer was treated as an idle disconnect")
	}
}

// Uses that arrive while the Line is being woken wait for that one login.
func TestAnyConnectUsesShareOneWake(t *testing.T) {
	vpn := &gatedRecoveryVPNClient{
		hold:    map[int]chan struct{}{2: make(chan struct{})},
		entered: map[int]chan struct{}{2: make(chan struct{})},
	}
	l, cb, sink := startIdleCapableAnyConnect(t, vpn)
	// Runs before the Stop registered by the start helper, which would
	// otherwise wait forever on a connect this test failed to release.
	var release sync.Once
	releaseConnect := func() { release.Do(func() { close(vpn.hold[2]) }) }
	t.Cleanup(releaseConnect)
	line := l.activeAnyConnectCapability.snapshot().line
	endSessionForIdleness(t, l)
	eventually(t, "line reported idle", func() bool { return cb.has(`"line_idle"`) })

	results := make(chan error, 3)
	for range 3 {
		go func() { results <- dialThroughLine(line) }()
	}
	select {
	case <-vpn.entered[2]:
	case <-time.After(2 * time.Second):
		t.Fatal("no use started the wake")
	}
	select {
	case err := <-results:
		t.Fatalf("a use returned before the line was back: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseConnect()
	for range 3 {
		if err := <-results; err != nil {
			t.Fatalf("use during wake: %v", err)
		}
	}
	if vpn.count() != 2 {
		t.Fatalf("connects = %d, want one shared wake", vpn.count())
	}
	wakes := 0
	for _, line := range sink.snapshot() {
		if strings.HasPrefix(line.summary, "anyconnect line waking") {
			wakes++
		}
	}
	if wakes != 1 {
		t.Fatalf("one wake was logged %d times", wakes)
	}
}

// A wake that cannot reach the server fails the uses that asked for it, the
// same way a recovering Line does, and takes the usual failure path.
func TestAnyConnectFailedWakeFailsClosed(t *testing.T) {
	vpn := &gatedRecoveryVPNClient{results: map[int]error{1: nil}, fallback: syscall.ECONNREFUSED}
	l, cb, _ := startIdleCapableAnyConnect(t, vpn)
	line := l.activeAnyConnectCapability.snapshot().line
	endSessionForIdleness(t, l)
	eventually(t, "line reported idle", func() bool { return cb.has(`"line_idle"`) })

	if err := dialThroughLine(line); !errors.Is(err, errAnyConnectLineUnavailable) {
		t.Fatalf("use of an unreachable line = %v, want unavailable", err)
	}
	if vpn.count() != 1+maxAnyConnectReconnectAttempts {
		t.Fatalf("connects = %d, want the usual recovery budget", vpn.count())
	}
	eventually(t, "failed wake reported as a Line failure", func() bool {
		return cb.errorCount() > 0
	})
}

// A Scenario switch must not be refused, or fail its readiness proof, because
// the Line it shares happens to be dormant: proving the Line ready wakes it.
func TestAnyConnectDormantLineSurvivesSwitch(t *testing.T) {
	vpn := &gatedRecoveryVPNClient{}
	l, cb, _ := startIdleCapableAnyConnect(t, vpn)
	capability := l.activeAnyConnectCapability
	endSessionForIdleness(t, l)
	eventually(t, "line reported idle", func() bool { return cb.has(`"line_idle"`) })

	envelope := networkReadinessTestEnvelope(t, switchAnyConnectConfig, map[string]string{"company": "vpn"}, true)
	if err := l.PrepareSwitchForNetwork(envelope, gateNetworkContext("B", "test0", 1, gateBothNetworkInterfaces), "192.0.2.1"); err != nil {
		t.Fatalf("prepare switch with a dormant line: %v", err)
	}
	if _, err := l.EnsurePreparedSwitchLinesReady(1000); err != nil {
		t.Fatalf("readiness with a dormant line: %v", err)
	}
	if err := l.CommitPreparedSwitch(); err != nil {
		t.Fatalf("commit switch with a dormant line: %v", err)
	}
	if err := l.RetireCommittedSwitch(); err != nil {
		t.Fatal(err)
	}
	if vpn.count() != 2 || capability.snapshot().session == nil {
		t.Fatalf("readiness proof did not wake the line: connects = %d", vpn.count())
	}
	if l.activeAnyConnectCapability != capability || cb.errorCount() != 0 || !l.IsRunning() {
		t.Fatal("switch replaced or failed the shared line")
	}

	// The committed generation monitors the session the proof established.
	endSessionForIdleness(t, l)
	eventually(t, "idle disconnect after the switch parks again", func() bool {
		return capability.snapshot().dormant
	})
}
