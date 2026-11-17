//go:build !windows

package libbox

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAndroidProfileSourceRequiresProtectionAndKeepsIdle(t *testing.T) {
	library, err := NewProfileLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	l := New(nil)
	fetch := func() (string, error) {
		return l.FetchAndroidProfilePreview(library, "https://example.invalid/config", "direct", "Fixture", "", `["192.0.2.53"]`, t.TempDir())
	}
	if _, err = fetch(); err == nil {
		t.Fatal("unprotected import accepted")
	}
	p := &testSocketProtector{err: errors.New("secret fixture")}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	_, err = fetch()
	if err == nil || strings.Contains(err.Error(), "secret") || p.fd == 0 {
		t.Fatal("protection not enforced", err)
	}
	if l.running || l.startPreparing || l.platform.tunFD != 0 {
		t.Fatal("import acquired traffic or leaked preparation")
	}
}
func TestAndroidProfileSourceCancellationReleasesLease(t *testing.T) {
	library, _ := NewProfileLibrary("")
	l := New(nil)
	p := &blockingPreparationProtector{entered: make(chan struct{}), release: make(chan struct{})}
	l.SetSocketProtector(p)
	l.SetNetworkInterfaces(`[{"name":"test0","index":7,"type":"wifi","facts":{"mtu":1500,"flags":17,"addresses":["192.0.2.10/24"]}}]`)
	l.SetDefaultInterface("test0", 7)
	done := make(chan error, 1)
	base := t.TempDir()
	go func() {
		_, err := l.FetchAndroidProfilePreview(library, "https://example.invalid/config", "direct", "Fixture", "", `["192.0.2.53"]`, base)
		done <- err
	}()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no protected DNS")
	}
	if err := l.SetSocketProtector(&testSocketProtector{}); err == nil {
		t.Fatal("allowed transport replacement")
	}
	l.CancelPreparation()
	stopped := make(chan error, 1)
	go func() { stopped <- l.Stop() }()
	close(p.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled import returned preview")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("import stuck")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop stuck")
	}
}
