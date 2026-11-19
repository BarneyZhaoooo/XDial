//go:build !windows

package libbox

import (
	"context"
	stdjson "encoding/json"

	"sslcon/session"
)

// A server that enforces an idle timeout ends a session nobody is using. The
// Line used to answer by logging in again at once, only to be idle and ended
// again a minute later: a Line that carries a handful of internal names logged
// in every few minutes, all day, for nothing.
//
// Such a Line now goes dormant instead. It keeps its lease, its place in the
// committed generation and its ready task; only the transport is gone. The
// first connection or DNS query routed to it re-establishes the session through
// the capability's one recovery owner, with the usual budget and gates, and
// waits for it. Every other way a session ends still recovers immediately.

// anyConnectSessionEndedIdle reports whether the server ended the session
// because it was idle. It takes the server's own word for it when the
// disconnect names an idle timeout. Otherwise it requires the two facts that
// make the policy the only reading: the server announced an idle timeout, and
// no data moved in either direction for at least that long before it
// disconnected. DPD and keepalive frames do not count as data, on the server
// or here.
func anyConnectSessionEndedIdle(diagnostics stdjson.RawMessage) bool {
	var state struct {
		SessionAgeMS int64 `json:"session_age_ms"`
		Negotiated   struct {
			CSTPIdleTimeoutSeconds *int `json:"cstp_idle_timeout_seconds"`
		} `json:"negotiated"`
		TLS   anyConnectCloseTransport `json:"tls"`
		DTLS  anyConnectCloseTransport `json:"dtls"`
		Close *struct {
			Code               string `json:"code"`
			PeerReasonCategory string `json:"peer_reason_category"`
		} `json:"close"`
	}
	if len(diagnostics) == 0 || stdjson.Unmarshal(diagnostics, &state) != nil || state.Close == nil {
		return false
	}
	if state.Close.Code != "peer-disconnect" && state.Close.Code != "peer-terminate" {
		return false
	}
	if state.Close.PeerReasonCategory == "idle-timeout" {
		return true
	}
	idleTimeout := state.Negotiated.CSTPIdleTimeoutSeconds
	if idleTimeout == nil || *idleTimeout <= 0 {
		return false
	}
	// A direction that never carried data has been idle for the whole session.
	quietMS := state.SessionAgeMS
	for _, age := range []*int64{
		state.TLS.LastDataReceivedAgeMS,
		state.TLS.LastDataWrittenAgeMS,
		state.DTLS.LastDataReceivedAgeMS,
		state.DTLS.LastDataWrittenAgeMS,
	} {
		if age != nil && *age < quietMS {
			quietMS = *age
		}
	}
	return quietMS >= int64(*idleTimeout)*1000
}

// parkDormant retires the closed session without starting a recovery. It
// reports false when the capability has moved on (another session, a recovery
// already under way, or stopped), in which case the caller recovers as usual.
func (c *anyConnectRuntimeCapability) parkDormant(closed *session.ConnSession) bool {
	c.mu.Lock()
	if c.stopped || c.session != closed || c.recovery != nil ||
		c.owner == nil || c.ownerCtx == nil || c.ownerCtx.Err() != nil {
		c.mu.Unlock()
		return false
	}
	owner, bridge := c.owner, c.bridge
	if c.line != nil && bridge != nil {
		c.line.deactivate(bridge)
	}
	c.bridge = nil
	c.session = nil
	c.sessionInterface = ""
	c.dnsJSON = "[]"
	c.revision++
	c.dormant = true
	c.mu.Unlock()

	owner.syncAnyConnectCapability(c)
	if bridge != nil {
		bridge.Close()
	}
	if c.vpn != nil {
		c.vpn.Disconnect()
	}
	owner.notifyAnyConnectCapabilityDormant(c)
	return true
}

// wakeForUse is what a dial or DNS query calls when it finds the Line without
// a transport. A dormant Line is re-established and the caller waits for the
// outcome; so does a caller that arrives while that is under way. A Line that
// lost its session any other way is recovering on its own and stays
// fail-closed for new uses, as before.
func (c *anyConnectRuntimeCapability) wakeForUse(ctx context.Context) error {
	c.mu.Lock()
	onDemand := c.dormant || (c.recovery != nil && c.recovery.onDemand)
	c.mu.Unlock()
	if !onDemand {
		return errAnyConnectLineUnavailable
	}
	return c.ensureTransport(ctx, false)
}

func (l *Libbox) notifyAnyConnectCapabilityDormant(c *anyConnectRuntimeCapability) {
	l.mu.Lock()
	active := l.running && !l.cleaning && l.activeAnyConnectCapability == c
	sink := l.dataPlaneLogSink
	l.mu.Unlock()
	if sink != nil {
		const line = "anyconnect line dormant: the server ended an idle session; it reconnects on first use"
		sink.WriteDataPlaneLog("warn", line, line)
	}
	if active {
		l.notifyStatus(anyConnectLineIdleStatusJSON())
	}
}

func (l *Libbox) reportAnyConnectLineWake() {
	l.mu.Lock()
	sink := l.dataPlaneLogSink
	l.mu.Unlock()
	if sink != nil {
		const line = "anyconnect line waking: a connection needs the dormant line"
		sink.WriteDataPlaneLog("warn", line, line)
	}
}

// anyConnectLineIdleStatusJSON tells the host that the Line is dormant. It is
// not a failure and not a recovery in progress: the Line stays ready.
func anyConnectLineIdleStatusJSON() string {
	data, err := stdjson.Marshal(struct {
		Status      string `json:"status"`
		LineType    string `json:"line_type"`
		Attempt     int    `json:"attempt"`
		MaxAttempts int    `json:"max_attempts"`
		Reason      string `json:"reason"`
	}{
		Status:      "line_idle",
		LineType:    "vpn",
		MaxAttempts: maxAnyConnectReconnectAttempts,
		Reason:      "server-idle-timeout",
	})
	if err != nil {
		return `{"status":"line_idle","line_type":"vpn"}`
	}
	return string(data)
}
