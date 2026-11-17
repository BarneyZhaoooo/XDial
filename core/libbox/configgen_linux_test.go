//go:build linux && !android

package libbox

import "testing"

func TestLinuxPrivilegedProfileAdmission(t *testing.T) {
	for _, raw := range []string{`{"rule_sets":[{"url":"file:///root/key"}]}`, `{"subscriptions":[{"geoip_rule_set_url_template":"file:///root/{code}"}]}`, `{"lines":[{"native_options":{"tls":{"certificate_path":"/root/key"}}}]}`} {
		if e := validateLinuxProfileFiles(raw); e == nil {
			t.Fatal("root file capability admitted")
		}
	}
}
