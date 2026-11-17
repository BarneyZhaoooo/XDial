//go:build windows

package libbox

import (
	"fmt"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
)

// Windows flow ingress does not own a Unix TUN descriptor. Never reinterpret
// one as a Windows HANDLE or silently create an adapter from mobile options.
func (p *xdPlatformInterface) defaultTunOpener(_ *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	return nil, fmt.Errorf("platform: Unix TUN descriptor adoption is unavailable on Windows")
}
