//go:build !windows

package libbox

import "fmt"

// SocketProtector is the platform-owned pre-connect socket boundary. Android
// implementations must VpnService.protect and bind to the captured Underlay
// Network, returning an error if either step fails. The fd is borrowed: never
// close it or retain it after this synchronous call. No DNS or route decisions
// belong here. Implementations must be safe for concurrent protocol dialers.
type SocketProtector interface {
	ProtectSocket(fd int) error
}

// SetSocketProtector installs the callback before starting a session. Each
// candidate and pooled Line inherits it. The callback must refer to a stable
// service-owned network provider, not an Activity or a retired Box generation.
func (l *Libbox) SetSocketProtector(protector SocketProtector) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running || l.startPreparing || l.cleaning {
		return fmt.Errorf("platform: socket protector cannot change during a session")
	}
	l.platform.mu.Lock()
	defer l.platform.mu.Unlock()
	l.platform.socketProtector = protector
	return nil
}
