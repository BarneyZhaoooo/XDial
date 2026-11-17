//go:build linux && !android

package linuxservice

import "testing"

func TestNSSIngressAdmission(t *testing.T) {
	for _, s := range []string{
		"hosts: files dns\n", "hosts: files mdns4_minimal [NOTFOUND=return] dns myhostname # local names\n",
		"passwd: files systemd\nhosts: files[NOTFOUND=continue]dns\n",
	} {
		if e := validateNSSHosts(s); e != nil {
			t.Errorf("valid hosts declaration: %v", e)
		}
	}
	for _, s := range []string{
		"hosts: files resolve [!UNAVAIL=return] dns", "hosts: files resolve[!UNAVAIL=return]dns",
		"hosts: files unknown dns", "hosts: files", "passwd: files", "hosts: files dns\nhosts: files dns",
		"hosts: files [SUCCESS=return dns", "hosts: files ] dns", "hosts: files [[SUCCESS=return]] dns",
	} {
		if e := validateNSSHosts(s); e == nil {
			t.Errorf("accepted unsupported hosts declaration %q", s)
		}
	}
}
