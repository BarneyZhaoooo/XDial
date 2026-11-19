//go:build !windows

package libbox

import (
	stdjson "encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// reportAnyConnectSessionClose 把一条 AnyConnect 会话为什么结束写进数据面日志。
//
// 线路局部恢复只留下“正在重连 / 已恢复”，看不出是服务器按策略结束了会话、
// 还是本端判定对端失联。sslcon 补丁在会话结束时已经记下了这些事实（协商到的
// DPD / keepalive / 空闲超时、谁先关、两条通道各自最后一次收发数据的时间），
// 这里只负责把它们交出去。内容全是数字和固定代码，不含地址、主机名和凭据，
// 所以公开字段和私有字段相同。
func reportAnyConnectSessionClose(sink DataPlaneLogSink, diagnostics stdjson.RawMessage) {
	if sink == nil {
		return
	}
	summary := anyConnectSessionCloseSummary(diagnostics)
	if summary == "" {
		return
	}
	sink.WriteDataPlaneLog("warn", summary, summary)
}

type anyConnectCloseTransport struct {
	DPDRequestsWritten      uint64 `json:"dpd_requests_written"`
	DPDResponsesReceived    uint64 `json:"dpd_responses_received"`
	PeerDPDRequestsReceived uint64 `json:"peer_dpd_requests_received"`
	KeepalivesReceived      uint64 `json:"keepalives_received"`
	DataFramesReceived      uint64 `json:"data_frames_received"`
	DataFramesWritten       uint64 `json:"data_frames_written"`
	LastFrameAgeMS          *int64 `json:"last_frame_age_ms"`
	LastDataReceivedAgeMS   *int64 `json:"last_data_received_age_ms"`
	LastDataWrittenAgeMS    *int64 `json:"last_data_written_age_ms"`
	LastFailureCode         string `json:"last_failure_code"`
	EverConnected           bool   `json:"ever_connected"`
	CurrentlyConnected      bool   `json:"currently_connected"`
}

func anyConnectSessionCloseSummary(diagnostics stdjson.RawMessage) string {
	var state struct {
		ForceDPDSeconds int   `json:"force_dpd_seconds"`
		SessionAgeMS    int64 `json:"session_age_ms"`
		Negotiated      struct {
			TLSDPDSeconds             int    `json:"tls_dpd_seconds"`
			TLSKeepaliveSeconds       int    `json:"tls_keepalive_seconds"`
			DTLSDPDSeconds            int    `json:"dtls_dpd_seconds"`
			DTLSKeepaliveSeconds      int    `json:"dtls_keepalive_seconds"`
			CSTPRekeySeconds          *int   `json:"cstp_rekey_seconds"`
			CSTPRekeyMethod           string `json:"cstp_rekey_method"`
			CSTPSessionTimeoutSeconds *int   `json:"cstp_session_timeout_seconds"`
			CSTPIdleTimeoutSeconds    *int   `json:"cstp_idle_timeout_seconds"`
		} `json:"negotiated"`
		TLS   anyConnectCloseTransport `json:"tls"`
		DTLS  anyConnectCloseTransport `json:"dtls"`
		Close *struct {
			Code               string `json:"code"`
			Channel            string `json:"channel"`
			ErrorClass         string `json:"error_class"`
			PeerReasonCode     string `json:"peer_reason_code"`
			PeerReasonCategory string `json:"peer_reason_category"`
		} `json:"close"`
	}
	if len(diagnostics) == 0 || stdjson.Unmarshal(diagnostics, &state) != nil {
		return ""
	}

	var line strings.Builder
	line.WriteString("anyconnect session closed:")
	if state.Close == nil {
		line.WriteString(" code=unknown")
	} else {
		fmt.Fprintf(&line, " code=%s channel=%s class=%s",
			anyConnectCloseToken(state.Close.Code),
			anyConnectCloseToken(state.Close.Channel),
			anyConnectCloseToken(state.Close.ErrorClass),
		)
		if state.Close.PeerReasonCode != "" || state.Close.PeerReasonCategory != "" {
			fmt.Fprintf(&line, " peer_reason=%s/%s",
				anyConnectCloseToken(state.Close.PeerReasonCode),
				anyConnectCloseToken(state.Close.PeerReasonCategory),
			)
		}
	}
	fmt.Fprintf(&line, " age=%s", anyConnectCloseAge(&state.SessionAgeMS))
	fmt.Fprintf(&line,
		" negotiated(dpd=%d keepalive=%d dtls_dpd=%d dtls_keepalive=%d idle_timeout=%s session_timeout=%s rekey=%s/%s)",
		state.Negotiated.TLSDPDSeconds,
		state.Negotiated.TLSKeepaliveSeconds,
		state.Negotiated.DTLSDPDSeconds,
		state.Negotiated.DTLSKeepaliveSeconds,
		anyConnectCloseSeconds(state.Negotiated.CSTPIdleTimeoutSeconds),
		anyConnectCloseSeconds(state.Negotiated.CSTPSessionTimeoutSeconds),
		anyConnectCloseSeconds(state.Negotiated.CSTPRekeySeconds),
		anyConnectCloseToken(state.Negotiated.CSTPRekeyMethod),
	)
	fmt.Fprintf(&line, " forced_dpd=%d", state.ForceDPDSeconds)
	line.WriteString(" tls(")
	line.WriteString(anyConnectCloseTransportSummary(state.TLS))
	line.WriteString(") dtls(")
	fmt.Fprintf(&line, "ever=%t up=%t ", state.DTLS.EverConnected, state.DTLS.CurrentlyConnected)
	line.WriteString(anyConnectCloseTransportSummary(state.DTLS))
	if state.DTLS.LastFailureCode != "" {
		fmt.Fprintf(&line, " failure=%s", anyConnectCloseToken(state.DTLS.LastFailureCode))
	}
	line.WriteString(")")
	return line.String()
}

func anyConnectCloseTransportSummary(transport anyConnectCloseTransport) string {
	return fmt.Sprintf(
		"dpd_out=%d dpd_resp=%d peer_dpd=%d keepalive_in=%d data_in=%d data_out=%d last_frame=%s last_data_in=%s last_data_out=%s",
		transport.DPDRequestsWritten,
		transport.DPDResponsesReceived,
		transport.PeerDPDRequestsReceived,
		transport.KeepalivesReceived,
		transport.DataFramesReceived,
		transport.DataFramesWritten,
		anyConnectCloseAge(transport.LastFrameAgeMS),
		anyConnectCloseAge(transport.LastDataReceivedAgeMS),
		anyConnectCloseAge(transport.LastDataWrittenAgeMS),
	)
}

// nil 表示服务器没有下发这一项；0 表示下发了 "none"。
func anyConnectCloseSeconds(seconds *int) string {
	switch {
	case seconds == nil:
		return "absent"
	case *seconds == 0:
		return "none"
	default:
		return strconv.Itoa(*seconds)
	}
}

func anyConnectCloseAge(milliseconds *int64) string {
	if milliseconds == nil {
		return "never"
	}
	return strconv.FormatFloat(float64(*milliseconds)/1000, 'f', 1, 64) + "s"
}

// 这些字段来自 sslcon 补丁里的固定代码表。仍按字符集收紧一遍：这一行会按公开
// 字段进统一日志，不能因为上游多了一个自由文本字段就把它带出去。
func anyConnectCloseToken(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) > 48 {
		return "invalid"
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= '0' && character <= '9',
			character == '-':
		default:
			return "invalid"
		}
	}
	return value
}
