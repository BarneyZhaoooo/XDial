//go:build !windows

package libbox

import "fmt"

// SecureStateStore is a synchronous platform credential boundary. Missing keys
// return nil bytes; writing nil deletes the key. Implementations must atomically
// encrypt at rest and authenticate both namespace and key, without logging data.
type SecureStateStore interface {
	ReadState(namespace, key string) ([]byte, error)
	WriteState(namespace, key string, value []byte) error
}

func (l *Libbox) SetSecureStateStore(store SecureStateStore) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running || l.startPreparing || l.cleaning {
		return fmt.Errorf("secure state store cannot change during a session")
	}
	l.platform.mu.Lock()
	defer l.platform.mu.Unlock()
	l.platform.secureStateStore = store
	return nil
}
func (p *xdPlatformInterface) TailscaleStateRead(namespace, key string) ([]byte, error) {
	p.mu.Lock()
	store := p.secureStateStore
	p.mu.Unlock()
	if store == nil {
		return nil, fmt.Errorf("secure identity storage unavailable")
	}
	value, err := store.ReadState(namespace, key)
	if err != nil {
		return nil, fmt.Errorf("secure identity read failed")
	}
	return value, nil
}
func (p *xdPlatformInterface) TailscaleStateWrite(namespace, key string, value []byte) error {
	p.mu.Lock()
	store := p.secureStateStore
	p.mu.Unlock()
	if store == nil {
		return fmt.Errorf("secure identity storage unavailable")
	}
	if err := store.WriteState(namespace, key, value); err != nil {
		return fmt.Errorf("secure identity write failed")
	}
	return nil
}
