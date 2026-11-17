//go:build !windows

package libbox

import (
	"context"
	"fmt"
	"time"
)

// ProbeAndroidUnderlayCountry is a bounded preparation lease, safe even when a
// previous OS TUN remains installed during recovery. Its independent sing-box
// Direct transport uses captured system DNS and protect/bind for every socket.
// No VPN identity, TUN reader, user rules or caller-supplied URL is involved.
func (l *Libbox) ProbeAndroidUnderlayCountry(systemDNSJSON string, timeoutMS int) (string, error) {
	if timeoutMS < 100 || timeoutMS > 10000 {
		return "", fmt.Errorf("country timeout out of range")
	}
	l.mu.Lock()
	if l.running || l.startPreparing || l.cleaning {
		l.mu.Unlock()
		return "", fmt.Errorf("country survey requires idle runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS)*time.Millisecond)
	l.startPreparing = true
	l.startPreparationCancel = cancel
	l.startPreparationUsers.Add(1)
	l.mu.Unlock()
	defer func() {
		cancel()
		l.mu.Lock()
		l.startPreparing = false
		l.startPreparationCancel = nil
		l.mu.Unlock()
		l.startPreparationUsers.Done()
	}()
	bootstrap, err := l.newAndroidBootstrap(ctx, systemDNSJSON)
	if err != nil {
		return "", fmt.Errorf("protected country transport unavailable")
	}
	defer bootstrap.instance.Close()
	return probeAndroidCountry(ctx, bootstrap)
}

func probeAndroidCountry(ctx context.Context, bootstrap *androidBootstrap) (string, error) {
	for index, endpoint := range outboundCountryProbeEndpoints {
		if ctx.Err() != nil {
			break
		}
		// Give each fixed service a share of the remaining deadline so a stalled
		// first endpoint cannot consume the fallback's entire budget.
		deadline, _ := ctx.Deadline()
		attempt, cancel := context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(outboundCountryProbeEndpoints)-index))
		resource := *bootstrap
		resource.ctx = attempt
		body, err := resource.fetch(endpoint.url, 4096)
		cancel()
		if err != nil {
			continue
		}
		code, err := endpoint.parse(body)
		if err == nil && ctx.Err() == nil {
			return code, nil
		}
	}
	return "", fmt.Errorf("country observation unavailable")
}
