//go:build linux && !android

package linuxcapture

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"sync"
)

// Session serializes one installation identity through its private directory.
// Its nft socket must stay in the core-owning service, not a short-lived helper.
// Close removes capture before policy routes. Only then may the caller stop the
// core listener. A returned cleanup error preserves the journal for recovery.
type Session struct {
	mu       sync.Mutex
	state    *stateDir
	k        *kernel
	j        *journal
	closeErr error
}

// Recover removes only an exact stale journal's policy routes. The kernel has
// already removed the socket-owned capture table; a present table is never
// adopted or deleted by name. A live owner holds the directory flock.
func Recover(dir string) error {
	s, e := openState(dir)
	if e != nil {
		return e
	}
	defer s.close()
	j, e := s.read()
	if e != nil || j == nil {
		return e
	}
	boot, ns, e := kernelIdentity()
	if e != nil {
		return e
	}
	if boot != j.Boot {
		return s.remove()
	} // previous boot has no surviving network objects
	if ns != j.NetNS {
		return errors.New("capture journal belongs to another network namespace")
	}
	k, e := openKernel()
	if e != nil {
		return e
	}
	defer k.close()
	present, e := k.tablePresent(j)
	if e != nil {
		return e
	}
	if present {
		return errors.New("capture owner still present; recovery deferred")
	}
	if e = k.cleanupRoutes(j); e != nil {
		return e
	}
	return s.remove()
}

// Install requires a ready TProxy listener and a trusted, explicitly reserved
// Plan. It makes no DNS change, creates no device, and accepts no shell program.
// A journal must be reconciled by Recover before another session is installed.
func Install(ctx context.Context, dir string, p Plan) (*Session, error) {
	return install(ctx, dir, p, nil)
}

func install(ctx context.Context, dir string, p Plan, after func(int) error) (*Session, error) {
	if e := p.Validate(); e != nil {
		return nil, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s, e := openState(dir)
	if e != nil {
		return nil, e
	}
	prior, e := s.read()
	if e != nil || prior != nil {
		s.close()
		return nil, errors.Join(e, errors.New("capture journal requires recovery"))
	}
	k, e := openKernel()
	if e != nil {
		s.close()
		return nil, e
	}
	failBefore := func(e error) (*Session, error) { k.close(); s.close(); return nil, e }
	boot, ns, e := kernelIdentity()
	if e != nil {
		return failBefore(e)
	}
	var token [20]byte
	if _, e = rand.Read(token[:]); e != nil {
		return failBefore(e)
	}
	// Discover loopback as an OS fact, not by an interface-name convention.
	interfaces, e := net.Interfaces()
	if e != nil {
		return failBefore(e)
	}
	loop := 0
	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback != 0 {
			if loop != 0 {
				return failBefore(errors.New("ambiguous loopback interface"))
			}
			loop = i.Index
		}
	}
	if loop == 0 {
		return failBefore(errors.New("loopback interface unavailable"))
	}
	j := &journal{Version: 1, Boot: boot, NetNS: ns, Token: hex.EncodeToString(token[:16]), Realm: binary.BigEndian.Uint32(token[16:])&0x7fffffff | 1, Loopback: loop, Plan: p}
	if e = k.preflight(j); e != nil {
		return failBefore(e)
	}
	if e = s.write(j); e != nil {
		return failBefore(e)
	}
	session := &Session{state: s, k: k, j: j}
	for step := 1; step <= 3; step++ {
		if e = ctx.Err(); e != nil {
			break
		}
		j.Intent = step
		if e = s.write(j); e != nil {
			break
		}
		switch step {
		case 1:
			e = k.rt.RouteAdd(route(j))
		case 2:
			e = k.rt.RuleAdd(rule(j))
		case 3:
			e = k.installCapture(j)
		}
		if e != nil {
			break
		}
		if after != nil {
			if e = after(step); e != nil {
				break
			}
		}
	}
	if e == nil {
		e = ctx.Err()
	}
	if e != nil {
		return nil, errors.Join(e, session.Close())
	}
	return session, nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.k == nil {
		return s.closeErr
	}
	// Even if a netlink delete fails, closing the owned nft socket withdraws
	// capture. Recovery never guesses whether an unacknowledged operation ran.
	err := s.k.removeCapture(s.j)
	if err == nil {
		err = s.k.cleanupRoutes(s.j)
	}
	if err == nil {
		err = s.state.remove()
	}
	s.k.close()
	s.state.close()
	s.k = nil
	s.closeErr = err
	return err
}
