//go:build !windows

package libbox

import (
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	E "github.com/sagernet/sing/common/exceptions"
	"syscall"
)

// defaultTunOpener 是真实路径:dup 传入的 NE fd 后交给 tun.New。
// 参照 sing-box 官方 experimental/libbox/service.go 的 OpenInterface(dup(fd) + tun.New)。
func (p *xdPlatformInterface) defaultTunOpener(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	if p.tunFD <= 0 {
		return nil, E.New("platform: tun fd not set (call Libbox.SetTunFD with the NE packetFlow fd before Start)")
	}
	// sing-tun 会按值复制 options。真实接口名必须在 tun.New 之前写入，否则其内部
	// 会一直保留占位名 "utun"，Start 时再把接口监控器覆盖成错误名称。
	name, err := tunnelNameFromFileDescriptor(p.tunFD)
	if err != nil {
		return nil, E.Cause(err, "query tun name")
	}
	if name != "" {
		options.Name = name
		if options.InterfaceMonitor != nil {
			options.InterfaceMonitor.RegisterMyInterface(name)
		}
	}
	dupFd, err := syscall.Dup(p.tunFD)
	if err != nil {
		return nil, E.Cause(err, "dup tun file descriptor")
	}
	options.FileDescriptor = dupFd
	device, err := tun.New(*options)
	if err != nil {
		_ = syscall.Close(dupFd)
		return nil, err
	}
	// 非 Darwin 平台没有通用的 fd→名称查询；保留原来的创建后读取作为回退。
	if name == "" {
		name, err = device.Name()
		if err != nil {
			_ = device.Close()
			return nil, E.Cause(err, "read tun interface name")
		}
		options.Name = name
		if options.InterfaceMonitor != nil {
			options.InterfaceMonitor.RegisterMyInterface(name)
		}
	}
	return wrapTunWithDiagnostics(device, p), nil
}

// UnderNetworkExtension 返回 true:XDial Apple 数据面始终跑在 NEPacketTunnelProvider 里。
// sing-box 据此把 MTU 默认收敛到 4064 等 NE 友好参数。
