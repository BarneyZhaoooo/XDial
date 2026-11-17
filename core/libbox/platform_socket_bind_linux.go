//go:build linux && !android

package libbox

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
)

// Only binds to the already captured Underlay interface. It never selects an
// alternative interface, adds routes, or falls back after a permission failure.
func bindSocketToInterface(fd, interfaceIndex int) error {
	if fd < 0 || interfaceIndex <= 0 {
		return fmt.Errorf("platform: invalid socket or interface index")
	}
	iface, err := net.InterfaceByIndex(interfaceIndex)
	if err != nil {
		return fmt.Errorf("platform: captured Underlay interface unavailable: %w", err)
	}
	if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface.Name); err != nil {
		return fmt.Errorf("platform: bind socket to captured Underlay: %w", err)
	}
	return nil
}
