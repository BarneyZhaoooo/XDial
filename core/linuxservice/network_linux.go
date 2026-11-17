//go:build linux && !android

package linuxservice

import (
	"context"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/kafeifei/xdial/core/libbox"
	"github.com/vishvananda/netlink"
)

// Events invalidate captured DNS/interface facts. No timer polls the network,
// no interface is ranked, and no route decision is made here. The initial
// service withdraws capture on change; reconnect is an explicit user intent.
func (s *Service) observeNetwork() error {
	w, e := fsnotify.NewWatcher()
	if e != nil {
		return e
	}
	s.watcher = w
	if e = s.watchResolver(); e != nil {
		return e
	}
	links := make(chan netlink.LinkUpdate, 32)
	addresses := make(chan netlink.AddrUpdate, 32)
	failed := make(chan struct{}, 1)
	failure := func(error) {
		select {
		case failed <- struct{}{}:
		default:
		}
	}
	if e = netlink.LinkSubscribeWithOptions(links, s.ctx.Done(), netlink.LinkSubscribeOptions{ErrorCallback: failure}); e != nil {
		return e
	}
	if e = netlink.AddrSubscribeWithOptions(addresses, s.ctx.Done(), netlink.AddrSubscribeOptions{ErrorCallback: failure}); e != nil {
		return e
	}
	s.observer.Add(1)
	go func() {
		defer s.observer.Done()
		for {
			broken := false
			select {
			case <-s.ctx.Done():
				return
			case _, ok := <-links:
				broken = !ok
			case _, ok := <-addresses:
				broken = !ok
			case event, ok := <-w.Events:
				broken = !ok
				if ok && event.Name == "/run/nscd" {
					if e := s.watchNSSCache(); e != nil {
						broken = true
					}
				}
			case <-w.Errors:
				broken = true
			case <-failed:
				broken = true
			}
			select {
			case <-s.ctx.Done():
				return
			case <-s.gate:
			}
			if broken {
				s.networkFailed = true
			}
			if s.core != nil {
				_, _, identity, e := s.networkFacts()
				if broken || e != nil || identity != s.underlay || resolverIngressReady() != nil {
					s.revision++
					s.disconnect()
				}
			}
			s.gate <- struct{}{}
			if broken {
				return
			}
		}
	}()
	return nil
}

func (s *Service) observeResolverLease() {
	l := s.resolver
	if l == nil {
		return
	}
	s.observer.Add(1)
	go func() {
		defer s.observer.Done()
		select {
		case <-s.ctx.Done():
			return
		case <-l.Invalid():
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.gate:
		}
		defer func() { s.gate <- struct{}{} }()
		if s.resolver == l {
			s.revision++
			s.disconnect()
		}
	}()
}
func (s *Service) watchResolver() error {
	if e := s.watcher.Add("/etc"); e != nil {
		return e
	}
	if e := s.watcher.Add("/run"); e != nil {
		return e
	}
	if e := s.watchNSSCache(); e != nil {
		return e
	}
	path, e := filepath.EvalSymlinks("/etc/resolv.conf")
	if e != nil {
		return e
	}
	return s.watcher.Add(filepath.Dir(path))
}

func (s *Service) watchNSSCache() error {
	if _, e := os.Stat("/run/nscd"); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	return s.watcher.Add("/run/nscd")
}

// Cancel only the preparation lease. Join the callback before committing so a
// late deadline callback cannot cancel an already published generation.
func (s *Service) prepare(ctx context.Context, l *libbox.Libbox, work func() error) error {
	local, cancel := context.WithCancel(ctx)
	stopService := context.AfterFunc(s.ctx, cancel)
	defer stopService()
	defer cancel()
	done := make(chan struct{})
	stop := context.AfterFunc(local, func() { defer close(done); l.CancelPreparation() })
	e := work()
	if !stop() {
		<-done
	}
	if e == nil {
		e = local.Err()
	}
	return e
}
