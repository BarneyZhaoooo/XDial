//go:build !windows

package libbox

import (
	"errors"
	"strings"
	"testing"
)

type memorySecureState struct {
	values map[string][]byte
	fail   bool
}

func (s *memorySecureState) ReadState(ns, key string) ([]byte, error) {
	if s.fail {
		return nil, errors.New("sensitive-backend-error")
	}
	return s.values[ns+"/"+key], nil
}
func (s *memorySecureState) WriteState(ns, key string, value []byte) error {
	if s.fail {
		return errors.New("sensitive-backend-error")
	}
	s.values[ns+"/"+key] = value
	return nil
}
func TestSecureIdentityFailsClosedAndFollowsCapability(t *testing.T) {
	p := &xdPlatformInterface{}
	if _, err := p.TailscaleStateRead("one", "key"); err == nil {
		t.Fatal("missing storage allowed")
	}
	if err := p.TailscaleStateWrite("one", "key", []byte("fixture")); err == nil {
		t.Fatal("missing storage allowed write")
	}
	store := &memorySecureState{values: map[string][]byte{}}
	p.secureStateStore = store
	capability := p.lineRuntimeCapability()
	if err := capability.TailscaleStateWrite("one", "key", []byte("fixture")); err != nil {
		t.Fatal(err)
	}
	if b, _ := p.TailscaleStateRead("one", "key"); string(b) != "fixture" {
		t.Fatal("capability lost store")
	}
	if b, _ := p.TailscaleStateRead("two", "key"); b != nil {
		t.Fatal("namespace leaked")
	}
	store.fail = true
	if _, err := p.TailscaleStateRead("one", "key"); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("backend details escaped")
	}
}
