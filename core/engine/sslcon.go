package engine

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"

	"sslcon/auth"
	"sslcon/base"
	"sslcon/rpc"
	"sslcon/session"
)

const forcedAnyConnectDPDSeconds = 5

type anyConnectDPDConfigurator interface {
	SetXDialForceDPD(seconds int)
}

type anyConnectSessionDiagnostics interface {
	XDialDiagnosticsJSON() string
}

type VPNConfig struct {
	Server        string
	DialAddress   string // optional numeric socket target; Server remains TLS SNI / HTTP identity
	Username      string
	Password      string
	AllowInsecure bool // true 时跳过服务器证书验证（自签场景 opt-in）
}

type VPNInfo struct {
	VPNAddress string
	VPNMask    string
	DNS        []string
	MTU        int
	ServerAddr string
}

type VPNClient struct {
	mu            sync.Mutex
	info          *VPNInfo
	socketControl func(int) error
}

func NewVPNClient() *VPNClient {
	return &VPNClient{}
}

// NewVPNClientWithSocketControl captures the host boundary for this client.
// Each successful sslcon session retains this control for its DTLS channel.
func NewVPNClientWithSocketControl(control func(int) error) *VPNClient {
	return &VPNClient{socketControl: control}
}

func (c *VPNClient) Connect(cfg VPNConfig) (*VPNInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if runtime.GOOS == "android" && (c.socketControl == nil || cfg.DialAddress == "") {
		return nil, fmt.Errorf("Android AnyConnect requires socket protection and a resolved dial address")
	}
	auth.Prof.SocketControl = nil
	if c.socketControl != nil {
		control := c.socketControl
		auth.Prof.SocketControl = func(_ string, _ string, raw syscall.RawConn) error {
			var controlErr error
			if err := raw.Control(func(fd uintptr) { controlErr = control(int(fd)) }); err != nil {
				return err
			}
			return controlErr
		}
	}
	// Android's host already provides network facts. Do not query netlink or
	// infer an interface through sslcon's standalone CLI discovery path.
	auth.Prof.Initialized = runtime.GOOS == "android"
	base.Cfg.NoTUN = true
	base.Cfg.InsecureSkipVerify = cfg.AllowInsecure // 默认 false=验证证书，自签需显式 opt-in
	base.Cfg.CiscoCompat = true
	base.Cfg.LogLevel = "Info"
	dpdConfig, ok := any(base.Cfg).(anyConnectDPDConfigurator)
	if !ok {
		return nil, fmt.Errorf(
			"AnyConnect runtime does not support the required DPD policy",
		)
	}
	dpdConfig.SetXDialForceDPD(forcedAnyConnectDPDSeconds)
	base.InitLog()

	auth.Prof.Host = cfg.Server
	auth.Prof.DialAddress = cfg.DialAddress
	auth.Prof.Username = cfg.Username
	auth.Prof.Password = cfg.Password

	if err := rpc.Connect(); err != nil {
		return nil, fmt.Errorf("AnyConnect connect: %w", err)
	}

	cSess := session.Sess.CSess
	if cSess == nil {
		return nil, fmt.Errorf("no session after connect")
	}

	c.info = &VPNInfo{
		VPNAddress: cSess.VPNAddress,
		VPNMask:    cSess.VPNMask,
		DNS:        cSess.DNS,
		MTU:        cSess.MTU,
		ServerAddr: cSess.ServerAddress,
	}

	return c.info, nil
}

func (c *VPNClient) Session() *session.ConnSession {
	return session.Sess.CSess
}

func (c *VPNClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	session.Sess.ActiveClose = true
	if cSess := session.Sess.CSess; cSess != nil {
		cSess.Close()
	}
	c.info = nil
}

// AnyConnectSessionDiagnostics returns a credential-free, structured snapshot
// from the patched sslcon I/O boundary. Keeping the capability behind a narrow
// interface lets ordinary source inspection still compile against the pristine
// vendored module, while production Make targets require the staged patch.
func AnyConnectSessionDiagnostics(cSess *session.ConnSession) string {
	if cSess == nil {
		return ""
	}
	diagnostics, ok := any(cSess).(anyConnectSessionDiagnostics)
	if !ok {
		return ""
	}
	return diagnostics.XDialDiagnosticsJSON()
}
