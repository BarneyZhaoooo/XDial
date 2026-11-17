//go:build linux && !android

package linuxservice

import (
	"errors"
	"io"
	"os"
	"strings"
)

// A shared resolver's native IPC/cache loses the original application's socket
// identity. Do not silently admit that path as per-application DNS capture.
// The forthcoming native NSS ingress must provide kernel-authenticated caller
// metadata before these configurations can be supported.
func resolverIngressReady() error {
	f, e := os.Open("/etc/nsswitch.conf")
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil {
		return e
	}
	if len(b) > 16384 {
		return errors.New("NSS declaration exceeds bound")
	}
	if e = validateNSSHosts(string(b)); e != nil {
		return e
	}
	if _, e = os.Lstat("/run/nscd/socket"); !os.IsNotExist(e) {
		return errors.New("shared NSS cache requires a caller-aware ingress")
	}
	return nil
}
func validateNSSHosts(text string) error {
	found := false
	allowed := map[string]bool{"files": true, "dns": true, "myhostname": true, "mymachines": true, "mdns": true, "mdns4": true, "mdns6": true, "mdns_minimal": true, "mdns4_minimal": true, "mdns6_minimal": true}
	for _, line := range strings.Split(text, "\n") {
		line = strings.SplitN(line, "#", 2)[0]
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != "hosts" {
			continue
		}
		if found {
			return errors.New("ambiguous NSS hosts declaration")
		}
		found = true
		// Strip action clauses, preserving module boundaries even without spaces.
		s := parts[1]
		var modules strings.Builder
		action := false
		for _, r := range s {
			if r == '[' {
				if action {
					return errors.New("invalid NSS action")
				}
				action = true
				modules.WriteByte(' ')
			} else if r == ']' {
				if !action {
					return errors.New("invalid NSS action")
				}
				action = false
				modules.WriteByte(' ')
			} else if !action {
				modules.WriteRune(r)
			}
		}
		if action {
			return errors.New("unterminated NSS action")
		}
		dns := false
		for _, name := range strings.Fields(modules.String()) {
			if !allowed[name] {
				return errors.New("NSS module requires a caller-aware ingress")
			}
			dns = dns || name == "dns"
		}
		if !dns {
			return errors.New("no application DNS socket ingress")
		}
	}
	if !found {
		return errors.New("missing NSS hosts declaration")
	}
	return nil
}
