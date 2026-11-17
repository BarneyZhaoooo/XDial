//go:build linux && !android

package libbox

import "testing"

func TestLinuxPrivilegedProfileAdmission(t *testing.T) {
	for _, raw := range []string{`{"subscriptions":[{"GEOIP_RULE_SET_URL_TEMPLATE":"file:///root/{code}","lines":[{"Native_Options":{"tls":{"key_path":"/root/key"}}}]}]}`, `{"subscriptions":[{"lines":[{"Native_Options":{}}]}]}`, `{"rules":[{"URL":"FILE:///root/key"}]}`, `{"rule_sets":[{"url":"file:///root/key"}]}`, `{"subscriptions":[{"geoip_rule_set_url_template":"file:///root/{code}"}]}`, `{"lines":[{"native_options":{"tls":{"certificate_path":"/root/key"}}}]}`} {
		if e := validateLinuxProfileFiles(raw); e == nil {
			t.Fatal("root file capability admitted")
		}
	}
}
