// Package server is cairn serve: the one part of Cairn that opens the database for
// the API and listens on the network (docs/DESIGN.md §5.3, §8; decision D20). Run
// starts it. The bind rules are plain functions, so each is tested in a table with
// no socket. The reach guard (internal/core/reach_guard_test.go) holds the CLI, the
// MCP adapter and the client to no database and no listener; this package is the
// exemption, by name, that the server needs.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// NoKeysLine is printed when a loopback server starts with no active key. Every call
// is refused with 401 until a key exists, so the operator is told at the start.
const NoKeysLine = "cairn serve: no active API key: every call will be refused with 401 until a key exists"

// Loopback reports whether every address in ips is a loopback address: 127.0.0.0/8
// or ::1, and the IPv4-mapped form of either. An empty list is not loopback. A host
// with no addresses, and the empty host that means every interface, are therefore
// refused as network binds, never trusted as local.
func Loopback(ips []netip.Addr) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !ip.Unmap().IsLoopback() {
			return false
		}
	}
	return true
}

// ResolveHost returns the addresses of host, for the bind rules. An IP literal is its
// own address; a name is looked up, so "localhost" gives its loopback addresses. The
// empty host means every interface and has no address to check, so it returns nil,
// which Loopback refuses.
func ResolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	return ips, nil
}

// RequireTransport refuses a bind that is not loopback unless TLS is set, or
// --behind-proxy says a proxy in front terminates it (D20). Without one of the two,
// the API keys of every call would cross the network in the clear. The error names
// both ways to meet the rule.
func RequireTransport(loopback, tls, behindProxy bool) error {
	if loopback || tls || behindProxy {
		return nil
	}
	return errors.New("the address is not loopback, so it needs TLS: give --tls-cert and --tls-key, or --behind-proxy if a proxy in front terminates TLS")
}

// RequireKeys refuses a bind that is not loopback when no API key is active (DESIGN
// §8). A loopback server with no key may start; the caller prints NoKeysLine.
func RequireKeys(loopback bool, activeKeys int) error {
	if loopback || activeKeys > 0 {
		return nil
	}
	return errors.New("the address is not loopback, so it needs at least one active API key (DESIGN §8)")
}

// CleartextLine is the warning printed when --behind-proxy lets a non-loopback server
// run without TLS files. The server then speaks plain HTTP on that address, and only
// the proxy keeps the keys off the wire, and nothing stops a client that can reach the
// port from going round the proxy (DESIGN §5.3, D20). It returns "" when the bind has
// TLS of its own, or is loopback, where there is nothing to warn of.
func CleartextLine(loopback, tls, behindProxy bool) string {
	if loopback || tls || !behindProxy {
		return ""
	}
	return "cairn serve: warning: this server serves plain HTTP on a non-loopback address, with no TLS of its own: the proxy in front must terminate TLS, and the port must be reachable only from that proxy"
}
