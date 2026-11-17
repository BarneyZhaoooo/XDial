//go:build !darwin && !linux

package main

import "net"

// This legacy Unix-socket daemon has no authenticated transport on other OSes.
// The native desktop service must supply its own OS-backed peer identity.
func peerAllowed(net.Conn) bool { return false }
