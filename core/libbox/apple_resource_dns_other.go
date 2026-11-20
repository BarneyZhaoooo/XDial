//go:build !darwin || !cgo

package libbox

// Apple packet-tunnel resource acquisition requires the Apple system resolver.
func appleResourceDNS() []string { return nil }
