//go:build linux && !android

package main

import (
	"net"
	"os"
	"time"
)

// systemd validates the sender against the unit's main process. Standalone
// foreground operation has no NOTIFY_SOCKET and retains its existing behavior.
func notifyReady() error {
	path := os.Getenv("NOTIFY_SOCKET")
	if path == "" {
		return nil
	}
	c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if e != nil {
		return e
	}
	defer c.Close()
	if e = c.SetWriteDeadline(time.Now().Add(time.Second)); e != nil {
		return e
	}
	_, e = c.Write([]byte("READY=1"))
	return e
}
