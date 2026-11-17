package libbox

import (
	"fmt"
	"strings"

	M "github.com/sagernet/sing/common/metadata"
)

type outboundCountryProbeEndpoint struct {
	outboundAddressProbeEndpoint
	parse func(body []byte) (string, error)
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
		parse: parseAppleCountryCode,
	},
	{
		outboundAddressProbeEndpoint: outboundAddressProbeEndpoint{
			url:         "https://1.1.1.1/cdn-cgi/trace",
			destination: M.ParseSocksaddrHostPortStr("1.1.1.1", "443"),
		},
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
	outbound, ctx, release, generation, err := l.borrowRunningOutbound(outboundTag, timeoutMS)
	if err != nil {
		return "", err
	}
	defer release()

	var probeErrors []string
	for index, endpoint := range outboundCountryProbeEndpoints {
		body, fetchErr := fetchOutboundProbe(ctx, outbound, endpoint.outboundAddressProbeEndpoint,
			len(outboundCountryProbeEndpoints)-index)
		if fetchErr != nil {
			probeErrors = append(probeErrors, endpoint.destination.String()+": "+outboundAddressProbeSafeCode(fetchErr))
			continue
		}
		code, parseErr := endpoint.parse(body)
		if parseErr != nil {
			probeErrors = append(probeErrors, endpoint.destination.String()+": invalid-country")
			continue
		}
		if !l.probeGenerationIsCurrent(generation) {
			return "", fmt.Errorf("connection changed during exit country probe")
		}
		return code, nil
	}
	return "", fmt.Errorf("exit country probe failed: %s", strings.Join(probeErrors, "; "))
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
