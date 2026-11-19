//go:build !windows

package libbox

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
)

type recordedDataPlaneLog struct {
	level   string
	summary string
	detail  string
}

type recordingDataPlaneLogSink struct {
	mu    sync.Mutex
	lines []recordedDataPlaneLog
}

func (s *recordingDataPlaneLogSink) WriteDataPlaneLog(level, summary, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, recordedDataPlaneLog{level, summary, detail})
}

func (s *recordingDataPlaneLogSink) snapshot() []recordedDataPlaneLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedDataPlaneLog(nil), s.lines...)
}

func TestDataPlaneLogRedaction(t *testing.T) {
	tests := []struct {
		name    string
		message string
		detail  string
		summary string
	}{
		{
			name:    "platform prefix and colors",
			message: "\x1b[31mERROR\x1b[0m[0012] [\x1b[38;5;120m2893417512\x1b[0m 2m33s] connection: connection download closed: unexpected EOF",
			detail:  "[2893417512 2m33s] connection: connection download closed: unexpected EOF",
			summary: "[2893417512 2m33s] connection: connection download closed: unexpected EOF",
		},
		{
			name:    "raw read reset keeps its reason",
			message: "WARN[0153] [77 1.52s] connection: connection download interrupted by upstream: reason=reset: raw read: connection reset by peer",
			detail:  "[77 1.52s] connection: connection download interrupted by upstream: reason=reset: raw read: connection reset by peer",
			summary: "[77 1.52s] connection: connection download interrupted by upstream: reason=reset: raw read: connection reset by peer",
		},
		{
			// The device showed this one with its reason taken for a token.
			name:    "long errno reason",
			message: "WARN[0001] [7 22m53s] connection: connection upload interrupted by upstream: reason=errno-socket-is-not-connected: close tcp 192.0.2.7:51234->198.51.100.9:443: shutdown: socket is not connected",
			detail:  "[7 22m53s] connection: connection upload interrupted by upstream: reason=errno-socket-is-not-connected: close tcp 192.0.2.7:51234->198.51.100.9:443: shutdown: socket is not connected",
			summary: "[7 22m53s] connection: connection upload interrupted by upstream: reason=errno-socket-is-not-connected: close tcp <redacted> shutdown: socket is not connected",
		},
		{
			name:    "socket addresses",
			message: "ERROR[0001] connection: connection upload closed: write tcp 192.0.2.7:51234->198.51.100.9:443: write: network is down",
			detail:  "connection: connection upload closed: write tcp 192.0.2.7:51234->198.51.100.9:443: write: network is down",
			summary: "connection: connection upload closed: write tcp <redacted> write: network is down",
		},
		{
			name:    "domain, port and bracketed IPv6",
			message: "ERROR[0001] connection: open connection to internal.example.com:8443 using outbound/direct[direct]: dial tcp [2001:db8::1]:8443: connect: no route to host",
			detail:  "connection: open connection to internal.example.com:8443 using outbound/direct[direct]: dial tcp [2001:db8::1]:8443: connect: no route to host",
			summary: "connection: open connection to <redacted> using outbound/direct[direct]: dial tcp <redacted> connect: no route to host",
		},
		{
			name:    "dns question and bare IPv6",
			message: "ERROR[0001] dns: exchange failed for wiki. IN AAAA: no answer from 2001:db8::53",
			detail:  "dns: exchange failed for wiki. IN AAAA: no answer from 2001:db8::53",
			summary: "dns: exchange failed for <redacted> IN AAAA: no answer from <redacted>",
		},
		{
			name:    "single-label lookup",
			message: "ERROR[0001] lookup wiki on 10.0.0.1:53: no such host",
			detail:  "lookup wiki on 10.0.0.1:53: no such host",
			summary: "lookup <redacted> on <redacted> no such host",
		},
		{
			name:    "url",
			message: "ERROR[0001] fetch https://example.com/list.srs?id=7: 403",
			detail:  "fetch https://example.com/list.srs?id=7: 403",
			summary: "fetch <redacted> 403",
		},
		{
			// userinfo 是凭据：从它开始整段丢弃，私有字段也不留。
			name:    "url with userinfo",
			message: "ERROR[0001] fetch https://user:hunter2@example.com/list.srs: 403",
			detail:  "fetch https:// <redacted>",
			summary: "fetch <redacted> <redacted>",
		},
		{
			name:    "opaque token",
			message: "ERROR[0001] reject session 0123456789abcdef0123456789abcdef0123",
			detail:  "reject session 0123456789abcdef0123456789abcdef0123",
			summary: "reject session <redacted>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := dataPlaneLogDetail(test.message)
			if detail != test.detail {
				t.Fatalf("detail = %q, want %q", detail, test.detail)
			}
			if summary := dataPlaneLogSummary(detail); summary != test.summary {
				t.Fatalf("summary = %q, want %q", summary, test.summary)
			}
		})
	}
}

// sing 的 SOCKS / HTTP 入站把认证失败的用户名和密码原样拼进错误。凭据连私有
// 字段也不能进。
func TestDataPlaneLogDropsCredentialsFromBothFields(t *testing.T) {
	secrets := []string{"session-user.4f2a", "p@ss word,with=odd:chars"}
	messages := []string{
		"ERROR[0003] inbound/socks[transparent-proxy-in]: process connection from 127.0.0.1:50000: socks5: authentication failed, username=" +
			secrets[0] + ", password=" + secrets[1],
		"ERROR[0003] inbound/http[in]: http: authentication failed, username=" +
			secrets[0] + ", password=" + secrets[1],
		"ERROR[0003] outbound: handshake rejected, Password: " + secrets[1],
	}
	for _, message := range messages {
		detail := dataPlaneLogDetail(message)
		summary := dataPlaneLogSummary(detail)
		for _, field := range []string{detail, summary} {
			for _, secret := range secrets {
				if strings.Contains(field, secret) {
					t.Fatalf("credential %q leaked into %q", secret, field)
				}
			}
			if !strings.HasSuffix(field, dataPlaneLogRedacted) {
				t.Fatalf("credential tail not marked in %q", field)
			}
		}
		if !strings.Contains(detail, "authentication failed") &&
			!strings.Contains(detail, "handshake rejected") {
			t.Fatalf("detail lost the failure itself: %q", detail)
		}
	}
}

func TestDataPlaneLogWriterForwardsOnlyWarnAndAbove(t *testing.T) {
	sink := &recordingDataPlaneLogSink{}
	writer := newDataPlaneLogWriter(sink, time.Now)
	for _, level := range []log.Level{log.LevelInfo, log.LevelDebug, log.LevelTrace} {
		writer.WriteMessage(level, "INFO[0001] inbound connection to example.com:443")
	}
	if lines := sink.snapshot(); len(lines) != 0 {
		t.Fatalf("info and below reached the sink: %+v", lines)
	}
	writer.WriteMessage(log.LevelWarn, "WARN[0001] one")
	writer.WriteMessage(log.LevelError, "ERROR[0001] two")
	writer.WriteMessage(log.LevelFatal, "FATAL[0001] three")
	lines := sink.snapshot()
	want := []recordedDataPlaneLog{
		{"warn", "one", "one"},
		{"error", "two", "two"},
		{"error", "three", "three"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("line %d = %+v, want %+v", index, lines[index], want[index])
		}
	}
}

func TestDataPlaneLogWriterLimitsRateAndReportsWhatItDropped(t *testing.T) {
	sink := &recordingDataPlaneLogSink{}
	now := time.Unix(1_000, 0)
	writer := newDataPlaneLogWriter(sink, func() time.Time { return now })

	for index := 0; index < dataPlaneLogBurst+25; index++ {
		writer.WriteMessage(log.LevelError, "ERROR[0001] burst")
	}
	if got := len(sink.snapshot()); got != dataPlaneLogBurst {
		t.Fatalf("burst forwarded %d lines, want %d", got, dataPlaneLogBurst)
	}

	// 低级别的行不占额度，也不算作被丢弃。
	writer.WriteMessage(log.LevelInfo, "INFO[0001] ignored")

	now = now.Add(3 * dataPlaneLogRefillEvery)
	writer.WriteMessage(log.LevelWarn, "WARN[0002] after")
	lines := sink.snapshot()[dataPlaneLogBurst:]
	if len(lines) != 2 {
		t.Fatalf("after refill got %+v", lines)
	}
	if lines[0].level != "warn" || lines[0].summary != "suppressed 25 lines over the rate limit" {
		t.Fatalf("suppression note = %+v", lines[0])
	}
	if lines[1] != (recordedDataPlaneLog{"warn", "after", "after"}) {
		t.Fatalf("line after refill = %+v", lines[1])
	}

	// 三个额度里用掉了一个，剩下两个；计数已清零。
	writer.WriteMessage(log.LevelError, "ERROR[0002] a")
	writer.WriteMessage(log.LevelError, "ERROR[0002] b")
	writer.WriteMessage(log.LevelError, "ERROR[0002] c")
	if got := len(sink.snapshot()); got != dataPlaneLogBurst+4 {
		t.Fatalf("forwarded %d lines, want %d", got, dataPlaneLogBurst+4)
	}
}

// relayThroughDataPlane 起一代真实的 sing-box（SOCKS 入站、直连出站、日志级别
// 与生成器一致），经它连到 origin，返回应用一侧和源站一侧的连接。
func relayThroughDataPlane(t *testing.T, sink DataPlaneLogSink) (application, origin *net.TCPConn) {
	t.Helper()
	originListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = originListener.Close() })

	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ingressPort := reserve.Addr().(*net.TCPAddr).Port
	_ = reserve.Close()

	platform := &xdPlatformInterface{}
	platform.setDefaultInterface("test0", 1)
	l := &Libbox{platform: platform}
	l.SetDataPlaneLogSink(sink)
	configJSON := fmt.Sprintf(`{
		"log":{"level":"info"},
		"inbounds":[{
			"type":"socks","tag":"test-in",
			"listen":"127.0.0.1","listen_port":%d
		}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct"}
	}`, ingressPort)
	if err := l.StartStandalone(configJSON); err != nil {
		t.Fatalf("StartStandalone: %v", err)
	}
	t.Cleanup(func() { _ = l.Stop() })

	conn, err := net.DialTimeout(
		"tcp",
		fmt.Sprintf("127.0.0.1:%d", ingressPort),
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("dial ingress: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	socks5Connect(t, conn, originListener.Addr().(*net.TCPAddr))

	_ = originListener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	accepted, err := originListener.Accept()
	if err != nil {
		t.Fatalf("accept origin: %v", err)
	}
	t.Cleanup(func() { _ = accepted.Close() })
	_ = accepted.SetDeadline(time.Now().Add(5 * time.Second))

	application, origin = conn.(*net.TCPConn), accepted.(*net.TCPConn)
	// 先确认两个方向都通，再让测试去打断它。
	if _, err := application.Write([]byte("ping")); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(origin, buffer); err != nil {
		t.Fatalf("origin read: %v", err)
	}
	if _, err := origin.Write([]byte("pong")); err != nil {
		t.Fatalf("write pong: %v", err)
	}
	if _, err := io.ReadFull(application, buffer); err != nil {
		t.Fatalf("application read: %v", err)
	}
	return application, origin
}

// 上游的 RST 经过真实的入站包装层（SOCKS 握手连接、流量计数）后，应用一侧
// 读到的必须仍是重置；数据面同时在 warn 级别说明原因，且公开字段不带地址。
func TestDataPlaneForwardsUpstreamResetAndReportsIt(t *testing.T) {
	sink := &recordingDataPlaneLogSink{}
	application, origin := relayThroughDataPlane(t, sink)
	originAddress := origin.LocalAddr().String()

	if err := origin.SetLinger(0); err != nil {
		t.Fatal(err)
	}
	if err := origin.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := application.Read(make([]byte, 1))
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("application read = %v, want connection reset", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		var reported *recordedDataPlaneLog
		for _, line := range sink.snapshot() {
			if strings.Contains(line.summary, "interrupted by upstream") {
				line := line
				reported = &line
			}
		}
		if reported != nil {
			if reported.level != "warn" || !strings.Contains(reported.summary, "reason=reset") {
				t.Fatalf("reset reported as %+v", *reported)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reset never reached the sink: %+v", sink.snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, line := range sink.snapshot() {
		if strings.Contains(line.summary, "127.0.0.1") || strings.Contains(line.summary, originAddress) {
			t.Fatalf("summary carries an address: %q", line.summary)
		}
	}
}

// 上游正常结束仍是干净的 EOF，而且一条正常的流不产生任何 warn 行：info 级别的
// "inbound connection to …" 不能漏到出口。
func TestDataPlaneKeepsUpstreamFinCleanAndQuiet(t *testing.T) {
	sink := &recordingDataPlaneLogSink{}
	application, origin := relayThroughDataPlane(t, sink)

	if _, err := origin.Write([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	if err := origin.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	received, err := io.ReadAll(application)
	if err != nil {
		t.Fatalf("application read = %v, want clean EOF", err)
	}
	if string(received) != "bye" {
		t.Fatalf("application received %q", received)
	}
	// 上游只结束了它的发送方向，应用这一侧还能继续发。
	if _, err := application.Write([]byte("late")); err != nil {
		t.Fatalf("write after upstream FIN: %v", err)
	}
	late := make([]byte, 4)
	if _, err := io.ReadFull(origin, late); err != nil || string(late) != "late" {
		t.Fatalf("origin received %q: %v", late, err)
	}
	if err := application.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("origin read = %v, want EOF", err)
	}
	time.Sleep(50 * time.Millisecond)
	if lines := sink.snapshot(); len(lines) != 0 {
		t.Fatalf("a clean flow reached the sink: %+v", lines)
	}
}

// 应用发完请求先结束发送方向、再等应答，是 TCP 的正常用法。数据面给每条出站连接
// 套的跟踪包装不转发半关闭，上游连接曾因此被整个关掉，应答丢失。
func TestDataPlaneForwardsApplicationHalfCloseAndDeliversTheReply(t *testing.T) {
	sink := &recordingDataPlaneLogSink{}
	application, origin := relayThroughDataPlane(t, sink)

	if err := application.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("origin read = %v, want EOF", err)
	}
	if _, err := origin.Write([]byte("reply")); err != nil {
		t.Fatalf("origin could not answer after the application's FIN: %v", err)
	}
	if err := origin.Close(); err != nil {
		t.Fatal(err)
	}
	received, err := io.ReadAll(application)
	if err != nil {
		t.Fatalf("application read = %v, want the reply and a clean EOF", err)
	}
	if string(received) != "reply" {
		t.Fatalf("application received %q", received)
	}
	time.Sleep(50 * time.Millisecond)
	if lines := sink.snapshot(); len(lines) != 0 {
		t.Fatalf("a clean flow reached the sink: %+v", lines)
	}
}
