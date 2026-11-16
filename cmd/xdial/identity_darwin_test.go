//go:build darwin

package main

import "testing"

func TestNetworkExtensionStatePathUsesFlavorAppGroup(t *testing.T) {
	formal := networkExtensionStatePathForIdentity(runtimeIdentityForFlavor(""))
	if formal != "/private/var/root/Library/Group Containers/SX9RG894L5.com.xd.xdial.network/NetworkExtension" {
		t.Fatalf("formal Network Extension state path = %q", formal)
	}

	debug := networkExtensionStatePathForIdentity(runtimeIdentityForFlavor("debug"))
	if debug != "/private/var/root/Library/Group Containers/SX9RG894L5.com.xd.xdial.debug.network/NetworkExtension" {
		t.Fatalf("debug Network Extension state path = %q", debug)
	}
}
