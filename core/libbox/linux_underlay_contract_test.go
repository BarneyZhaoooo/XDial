package libbox

import "testing"

func TestKernelRoutedUnderlayDoesNotInventDefault(t *testing.T) {
	p := &xdPlatformInterface{kernelRoutedUnderlay: true}
	snapshot := `[{"name":"lo","index":1,"type":"other","facts":{"mtu":65536,"flags":5,"addresses":["127.0.0.1/8"]}}]`
	next, e := p.switchCandidate(snapshot, "", -1)
	if e != nil {
		t.Fatal(e)
	}
	if next.defaultInterface != nil || !next.kernelRoutedUnderlay {
		t.Fatal("candidate invented an interface")
	}
	if e = (&platformInterfaceMonitor{platform: next}).Start(); e != nil {
		t.Fatal(e)
	}
	owner := next.lineRuntimeCapability()
	if owner.defaultInterface != nil || !owner.kernelRoutedUnderlay {
		t.Fatal("capability changed Underlay")
	}
	ordinary := &xdPlatformInterface{}
	if _, e = ordinary.switchCandidate(snapshot, "", -1); e == nil {
		t.Fatal("Apple/Android missing-interface guard relaxed")
	}
	if e = (&platformInterfaceMonitor{platform: ordinary}).Start(); e == nil {
		t.Fatal("ordinary start guard relaxed")
	}
}
