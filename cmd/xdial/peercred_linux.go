//go:build linux

package main

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
)

// The legacy daemon only admits its own effective user. A future root service
// needs an explicit installer-owned client policy; an arbitrary desktop user's
// UID or a request-supplied identity must not become an implicit authorization.
func peerAllowed(conn net.Conn) bool {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	var credential *unix.Ucred
	var queryErr error
	err = raw.Control(func(fd uintptr) {
		credential, queryErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	return err == nil && queryErr == nil && credential != nil && credential.Uid == uint32(os.Geteuid())
}
