//go:build linux && !android

package linuxinstall

import (
	"errors"
	"testing"
)

func TestReviewDropIns(t *testing.T) {
	ok := func(string) error { return nil }
	for _, paths := range []string{"", "/usr/lib/systemd/system/service.d/10-timeout-abort.conf",
		"/usr/lib/systemd/system/service.d/10-timeout-abort.conf /usr/lib/systemd/system/service.d/50-x.conf"} {
		if e := reviewDropIns(paths, ok); e != nil {
			t.Fatalf("%q: %v", paths, e)
		}
	}
	for _, paths := range []string{
		"/etc/systemd/system/xdial-service.service.d/override.conf",
		"/etc/systemd/system/service.d/10-site.conf",
		"/run/systemd/system/service.d/10-runtime.conf",
		"/usr/local/lib/systemd/system/service.d/10-local.conf",
		"/usr/lib/systemd/system/xdial-service.service.d/10-vendor.conf",
		"/usr/lib/systemd/system/xdial-.service.d/10-prefix.conf",
		"/usr/lib/systemd/system/service.d/../xdial-service.service.d/x.conf",
		"/usr/lib/systemd/system/service.d/sub/x.conf",
		"/usr/lib/systemd/system/service.d/10-timeout-abort.conf /etc/systemd/system/xdial-service.service.d/override.conf",
	} {
		if reviewDropIns(paths, ok) == nil {
			t.Fatalf("accepted %q", paths)
		}
	}
	if reviewDropIns("/usr/lib/systemd/system/service.d/10-timeout-abort.conf", func(string) error { return errors.New("symlink") }) == nil {
		t.Fatal("accepted unsafe vendor file")
	}
}
