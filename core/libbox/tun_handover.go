package libbox

import (
	"context"
	"errors"
	"fmt"
	"os"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

// TUN handover lets a Packet Tunnel Provider use the staged Scenario switch.
//
// On macOS every Box generation owns its own loopback ingress and the
// Provider moves new flows to the candidate after Commit. iOS has a single
// packetFlow: only one gVisor stack can read it at a time. With handover
// enabled the candidate is started without its TUN inbound, so it can be
// probed beside the active generation without touching user traffic. Commit
// then closes the source TUN inbound and opens the candidate's on the same
// file descriptor. The NetworkExtension session, its routes and DNS settings
// stay untouched; only flows already terminated by the source gVisor stack
// are reset.

// SetSwitchTUNHandover selects the Packet Tunnel switch contract. It must be
// called before the first Start; the setting applies to every later generation
// of this instance.
func (l *Libbox) SetSwitchTUNHandover(enabled bool) {
	l.mu.Lock()
	l.tunHandover = enabled
	l.mu.Unlock()
}

// errTUNHandoverLost marks a Commit whose source TUN inbound could neither be
// handed to the candidate nor reopened. The active generation keeps running
// without a data plane, so the Provider must end the tunnel.
var errTUNHandoverLost = errors.New("switch TUN handover lost the tunnel")

type tunInboundSpec struct {
	tag         string
	inboundType string
	options     any
}

// handoverBoxContext makes the context passed to box.New carry the same
// service registry and pause manager box.New would otherwise create
// privately. The generation's runtimeCtx can then create its TUN inbound
// later exactly as box.New would have.
func handoverBoxContext(ctx context.Context) context.Context {
	return pause.WithDefaultManager(service.ContextWithDefaultRegistry(ctx))
}

// takeTUNInbound finds the generation's single TUN inbound. With detach the
// inbound is removed from options so box.New never opens it.
func takeTUNInbound(options *option.Options, detach bool) (*tunInboundSpec, error) {
	index := -1
	for i, inbound := range options.Inbounds {
		if inbound.Type != C.TypeTun {
			continue
		}
		if index >= 0 {
			return nil, fmt.Errorf("switch TUN handover requires exactly one TUN inbound")
		}
		index = i
	}
	if index < 0 {
		return nil, fmt.Errorf("switch TUN handover requires a TUN inbound")
	}
	inbound := options.Inbounds[index]
	if inbound.Tag == "" {
		return nil, fmt.Errorf("switch TUN inbound must be tagged")
	}
	spec := &tunInboundSpec{
		tag:         inbound.Tag,
		inboundType: inbound.Type,
		options:     inbound.Options,
	}
	if detach {
		remaining := make([]option.Inbound, 0, len(options.Inbounds)-1)
		remaining = append(remaining, options.Inbounds[:index]...)
		remaining = append(remaining, options.Inbounds[index+1:]...)
		options.Inbounds = remaining
	}
	return spec, nil
}

func openTUNInbound(
	instance *box.Box,
	ctx context.Context,
	spec *tunInboundSpec,
) error {
	return instance.Inbound().Create(
		ctx,
		instance.Router(),
		instance.LogFactory().NewLogger(
			"inbound/"+spec.inboundType+"["+spec.tag+"]",
		),
		spec.tag,
		spec.inboundType,
		spec.options,
	)
}

func closeTUNInbound(instance *box.Box, spec *tunInboundSpec) error {
	err := instance.Inbound().Remove(spec.tag)
	if errors.Is(err, os.ErrInvalid) {
		return nil
	}
	return err
}

// moveTUNInbound closes the TUN inbound of from and opens it in to. When to
// cannot open it, the inbound is reopened in from; errTUNHandoverLost means
// neither side holds it.
func moveTUNInbound(
	from *box.Box,
	fromCtx context.Context,
	fromSpec *tunInboundSpec,
	to *box.Box,
	toCtx context.Context,
	toSpec *tunInboundSpec,
) error {
	if err := closeTUNInbound(from, fromSpec); err != nil {
		// Remove unregisters the inbound before closing it, so a failed
		// close still leaves the tag free for a fresh instance.
		if restoreErr := openTUNInbound(from, fromCtx, fromSpec); restoreErr != nil {
			return fmt.Errorf("%w: close: %v; reopen: %v", errTUNHandoverLost, err, restoreErr)
		}
		return fmt.Errorf("close TUN inbound: %w", err)
	}
	if err := openTUNInbound(to, toCtx, toSpec); err != nil {
		if restoreErr := openTUNInbound(from, fromCtx, fromSpec); restoreErr != nil {
			return fmt.Errorf("%w: open: %v; reopen: %v", errTUNHandoverLost, err, restoreErr)
		}
		return fmt.Errorf("open TUN inbound: %w", err)
	}
	return nil
}
