package server

import (
	"context"
	"net/netip"
	"testing"
)

// addrs parses each address of a test table.
func addrs(t *testing.T, s ...string) []netip.Addr {
	t.Helper()
	var out []netip.Addr
	for _, v := range s {
		a, err := netip.ParseAddr(v)
		if err != nil {
			t.Fatalf("parse %q: %v", v, err)
		}
		out = append(out, a)
	}
	return out
}

// TestLoopback checks which addresses count as loopback. Only loopback needs no TLS and
// no key, so a wrong yes here is the failure the policy exists to stop.
func TestLoopback(t *testing.T) {
	cases := []struct {
		what string
		ips  []netip.Addr
		want bool
	}{
		{"IPv4 127.0.0.1", addrs(t, "127.0.0.1"), true},
		{"another address of 127/8", addrs(t, "127.5.6.7"), true},
		{"IPv6 ::1", addrs(t, "::1"), true},
		{"the IPv4-mapped form of 127.0.0.1", addrs(t, "::ffff:127.0.0.1"), true},
		{"localhost, both loopback addresses", addrs(t, "127.0.0.1", "::1"), true},
		{"0.0.0.0, every IPv4 interface", addrs(t, "0.0.0.0"), false},
		{":: , every IPv6 interface", addrs(t, "::"), false},
		{"a LAN address", addrs(t, "192.168.1.5"), false},
		{"a public address", addrs(t, "203.0.113.7"), false},
		{"a mapped LAN address", addrs(t, "::ffff:10.0.0.1"), false},
		{"localhost plus a LAN address", addrs(t, "127.0.0.1", "10.0.0.1"), false},
		{"a host with no address", nil, false},
	}
	for _, c := range cases {
		if got := Loopback(c.ips); got != c.want {
			t.Errorf("Loopback(%s) = %v, want %v", c.what, got, c.want)
		}
	}
}

// TestResolveHost checks the host forms the bind rule reads: the empty host, an IP
// literal, and a name, which is looked up.
func TestResolveHost(t *testing.T) {
	ctx := context.Background()
	if ips, err := ResolveHost(ctx, ""); err != nil || ips != nil {
		t.Errorf("ResolveHost(empty) = %v, %v; want nothing, so the bind is refused", ips, err)
	}
	if ips, err := ResolveHost(ctx, "127.0.0.1"); err != nil || len(ips) != 1 || ips[0].String() != "127.0.0.1" {
		t.Errorf("ResolveHost(127.0.0.1) = %v, %v", ips, err)
	}
	ips, err := ResolveHost(ctx, "localhost")
	if err != nil {
		t.Fatalf("ResolveHost(localhost): %v", err)
	}
	if !Loopback(ips) {
		t.Errorf("localhost resolved to %v, which is not loopback only", ips)
	}
}

// TestRequireTransport checks the TLS rule of D20: a loopback bind needs no TLS, and any
// other bind needs TLS files or --behind-proxy.
func TestRequireTransport(t *testing.T) {
	cases := []struct {
		what                     string
		loopback, tls, proxy, ok bool
	}{
		{"loopback, plain", true, false, false, true},
		{"loopback, with TLS", true, true, false, true},
		{"non-loopback, plain", false, false, false, false},
		{"non-loopback, with TLS", false, true, false, true},
		{"non-loopback, behind a proxy", false, false, true, true},
		{"non-loopback, TLS and a proxy", false, true, true, true},
	}
	for _, c := range cases {
		err := RequireTransport(c.loopback, c.tls, c.proxy)
		if (err == nil) != c.ok {
			t.Errorf("RequireTransport(%s) = %v, want ok=%v", c.what, err, c.ok)
		}
	}
}

// TestRequireKeys checks the key rule of DESIGN §8: a non-loopback bind needs at least one
// active key, and a loopback bind with none may start.
func TestRequireKeys(t *testing.T) {
	cases := []struct {
		what     string
		loopback bool
		keys     int
		ok       bool
	}{
		{"loopback, no keys", true, 0, true},
		{"loopback, one key", true, 1, true},
		{"non-loopback, no keys", false, 0, false},
		{"non-loopback, one key", false, 1, true},
		{"non-loopback, many keys", false, 5, true},
	}
	for _, c := range cases {
		err := RequireKeys(c.loopback, c.keys)
		if (err == nil) != c.ok {
			t.Errorf("RequireKeys(%s) = %v, want ok=%v", c.what, err, c.ok)
		}
	}
}
