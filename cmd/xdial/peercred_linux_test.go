//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"testing"
)

func TestLinuxPeerCredentialUsesKernelSocket(t *testing.T) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@xdial-test-" + hex.EncodeToString(id[:]), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	if !peerAllowed(server) {
		server.Close()
		t.Fatal("same effective user rejected")
	}
	server.Close()
	if peerAllowed(server) {
		t.Fatal("closed socket accepted")
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if peerAllowed(a) {
		t.Fatal("non Unix peer accepted")
	}
}
