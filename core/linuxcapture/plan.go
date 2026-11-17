// Package linuxcapture owns Linux system capture resources. It does not select
// application routes or DNS: captured TCP/UDP still enters sing-box.
package linuxcapture

import (
	"errors"
	"net/netip"
)

// Plan is a trusted service/installer reservation, not a client supplied route
// program. The caller must compile BypassMark into every core egress socket and
// start the matching TProxy listener before Install. IPv6 is not yet supported.
// No default mark/table/priority is silently imposed on an existing Underlay.
type Plan struct {
	CaptureMark uint32 `json:"captureMark"`
	BypassMark  uint32 `json:"bypassMark"`
	Table       int    `json:"table"`
	Priority    int    `json:"priority"`
	Listener    string `json:"listener"`
}

func (p Plan) Validate() error {
	oneBit := func(v uint32) bool { return v != 0 && v&(v-1) == 0 }
	if !oneBit(p.CaptureMark) || !oneBit(p.BypassMark) || p.CaptureMark == p.BypassMark {
		return errors.New("capture and bypass require distinct reserved mark bits")
	}
	if p.Table < 256 || p.Table > 2147483647 || p.Priority < 1 || p.Priority >= 32766 {
		return errors.New("invalid reserved route table or rule priority")
	}
	a, err := netip.ParseAddrPort(p.Listener)
	if err != nil || !a.Addr().Is4() || !a.Addr().IsLoopback() || a.Port() == 0 {
		return errors.New("capture requires a numeric IPv4 loopback listener")
	}
	return nil
}
