package libbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

type outboundCountryProbeEndpoint struct {
	outboundAddressProbeEndpoint
	// label names the service in diagnostics; it carries no address.
	label string
	parse func(body []byte) (string, error)
}

// outboundCountryEvidence explains where a region code came from: which
// service answered, after which failed attempts, and how long it took.
type outboundCountryEvidence struct {
	Code       string   `json:"code,omitempty"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Attempts   []string `json:"attempts,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// Apple's region service answers with the bare code and is reachable from
// mainland China; the numeric Cloudflare trace needs no DNS at all.
var outboundCountryProbeEndpoints = []outboundCountryProbeEndpoint{
	{
		outboundAddressProbeEndpoint: outboundAddressProbeEndpoint{
			url:          "https://gspe1-ssl.ls.apple.com/pep/gcc",
			destination:  M.ParseSocksaddrHostPortStr("gspe1-ssl.ls.apple.com", "443"),
			lineResolved: true,
		},
		label: "apple-gcc",
		parse: parseAppleCountryCode,
	},
	{
		outboundAddressProbeEndpoint: outboundAddressProbeEndpoint{
			url:         "https://1.1.1.1/cdn-cgi/trace",
			destination: M.ParseSocksaddrHostPortStr("1.1.1.1", "443"),
		},
		label: "cloudflare-trace",
		parse: parseCloudflareCountryCode,
	},
}

// ProbeOutboundCountry returns the two-letter country or region code of the
// public exit seen through one exact running outbound.
//
// iOS asks this for the Direct Line to pick a Scenario while connected: the
// app's own requests are routed by the active Scenario and its DNS belongs to
// the tunnel, so only the engine can ask where the local network exits.
// Endpoints are fixed; callers cannot supply a URL.
func (l *Libbox) ProbeOutboundCountry(outboundTag string, timeoutMS int) (string, error) {
	evidence, err := l.probeOutboundCountry(outboundTag, timeoutMS)
	return evidence.Code, err
}

// ProbeOutboundCountryEvidence is ProbeOutboundCountry with its evidence as
// JSON: the code (when found), the service that answered, each failed attempt
// as "service:code", and the duration. Failures still return the evidence.
func (l *Libbox) ProbeOutboundCountryEvidence(outboundTag string, timeoutMS int) (string, error) {
	evidence, err := l.probeOutboundCountry(outboundTag, timeoutMS)
	data, encodeErr := json.Marshal(evidence)
	if encodeErr != nil {
		return "", encodeErr
	}
	return string(data), err
}

func (l *Libbox) probeOutboundCountry(outboundTag string, timeoutMS int) (outboundCountryEvidence, error) {
	started := time.Now()
	var evidence outboundCountryEvidence
	finish := func() { evidence.DurationMS = time.Since(started).Milliseconds() }
	outbound, ctx, release, generation, err := l.borrowRunningOutbound(outboundTag, timeoutMS)
	if err != nil {
		finish()
		return evidence, err
	}
	defer release()

	var probeErrors []string
	for index, endpoint := range outboundCountryProbeEndpoints {
		body, fetchErr := fetchOutboundProbe(ctx, outbound, endpoint.outboundAddressProbeEndpoint,
			len(outboundCountryProbeEndpoints)-index)
		if fetchErr != nil {
			probeErrors = append(probeErrors, endpoint.destination.String()+": "+outboundAddressProbeSafeCode(fetchErr))
			evidence.Attempts = append(evidence.Attempts, endpoint.label+":"+outboundAddressProbeSafeCode(fetchErr))
			continue
		}
		code, parseErr := endpoint.parse(body)
		if parseErr != nil {
			probeErrors = append(probeErrors, endpoint.destination.String()+": invalid-country")
			evidence.Attempts = append(evidence.Attempts, endpoint.label+":invalid-country")
			continue
		}
		if !l.probeGenerationIsCurrent(generation) {
			finish()
			return evidence, fmt.Errorf("connection changed during exit country probe")
		}
		evidence.Code = code
		evidence.Endpoint = endpoint.label
		finish()
		return evidence, nil
	}
	finish()
	return evidence, fmt.Errorf("exit country probe failed: %s", strings.Join(probeErrors, "; "))
}

func parseAppleCountryCode(body []byte) (string, error) {
	return validateCountryCode(string(body))
}

func parseCloudflareCountryCode(body []byte) (string, error) {
	for _, line := range strings.Split(string(body), "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "loc="); found {
			return validateCountryCode(value)
		}
	}
	return "", fmt.Errorf("trace has no loc line")
}

func validateCountryCode(raw string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	// XX and T1 are Cloudflare's "unknown" and Tor placeholders.
	if len(code) != 2 || code == "XX" || code == "T1" {
		return "", fmt.Errorf("not a region code")
	}
	for _, character := range code {
		if character < 'A' || character > 'Z' {
			return "", fmt.Errorf("not a region code")
		}
	}
	return code, nil
}
