package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestServeUsageRefusals checks the flags of cairn serve. Each refusal is a usage error
// (exit 2) that names the fix. The last case is a bind the policy forbids (exit 1): it is
// refused before anything listens, so the test binds nothing.
func TestServeUsageRefusals(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	cases := []struct {
		what string
		args []string
		code int
		want string
	}{
		{"no --db", []string{"serve"}, 2, "needs --db"},
		{"a certificate without a key", []string{"serve", "--db", db, "--tls-cert", "c.pem"}, 2, "go together"},
		{"a key without a certificate", []string{"serve", "--db", db, "--tls-key", "k.pem"}, 2, "go together"},
		{"a positional argument", []string{"serve", "--db", db, "extra"}, 2, "takes flags only"},
		{"an unknown flag", []string{"serve", "--db", db, "--nope"}, 2, "run cairn serve --help"},
		{"all interfaces without TLS or a proxy", []string{"serve", "--db", db, "--addr", "0.0.0.0:0"}, 1, "needs TLS"},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		code := run(c.args, &out, &errb)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d (stderr %q)", c.what, code, c.code, errb.String())
		}
		if !strings.Contains(errb.String(), c.want) {
			t.Errorf("%s: stderr %q does not name %q", c.what, errb.String(), c.want)
		}
	}
}

// TestServeHelpAndListing checks that cairn serve is listed and that its help names the
// flags, printed to stdout.
func TestServeHelpAndListing(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"serve", "--help"}, &out, &errb); code != 0 {
		t.Fatalf("serve --help: exit %d, stderr %q", code, errb.String())
	}
	for _, flag := range []string{"-addr", "-db", "-tls-cert", "-tls-key", "-behind-proxy"} {
		if !strings.Contains(out.String(), flag) {
			t.Errorf("serve --help does not name %s: %q", flag, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "serve") {
		t.Errorf("help does not list serve: exit %d, %q", code, out.String())
	}
}
