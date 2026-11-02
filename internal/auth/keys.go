// Package auth makes and checks the API keys of a Cairn server (docs/DESIGN.md
// §8). A key is random, is shown once, and is stored only as its hash. The
// resolver turns an Authorization header into the key's scope, and that scope,
// not anything the client sends, decides the caller's role (CONTRACT §1.1).
//
// The package imports no store. It reaches the key table through the Lookup
// interface, so the reach guard (internal/core) sees no database in it.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// KeyPrefix starts every key, so a key is recognisable in a config file or a
// log line, and a secret scanner can find it.
const KeyPrefix = "cairn_"

// keyBytes is the entropy of a key: 256 bits, which makes a fast hash enough
// (see HashKey).
const keyBytes = 32

// keyLen is the length of a well-formed key: the prefix, then the 32 bytes in
// unpadded base64url, which is 43 characters.
const keyLen = len(KeyPrefix) + 43

// Scope is what a key may do. One scope per key (DESIGN §3.6).
type Scope string

const (
	// ScopeAgent is the MCP tools: the agent calls, read and write.
	ScopeAgent Scope = "agent"
	// ScopeOperator is the decisions and ratification (CONTRACT §8), and every
	// agent call.
	ScopeOperator Scope = "operator"
	// ScopeViewer is read-only: the agent reads, and no write.
	ScopeViewer Scope = "viewer"
)

// ValidScope reports whether s is one of the three scopes.
func ValidScope(s Scope) bool {
	switch s {
	case ScopeAgent, ScopeOperator, ScopeViewer:
		return true
	}
	return false
}

// NewKey returns a new key: KeyPrefix and 32 bytes from crypto/rand, in unpadded
// base64url. The plaintext is returned once. The caller stores HashKey of it and
// shows the plaintext to the operator, and then nothing keeps it.
func NewKey() (string, error) {
	var b [keyBytes]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", fmt.Errorf("auth: read entropy for a key: %w", err)
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashKey returns the SHA-256 of key, in lower-case hex. A password KDF is not
// used, on purpose: a KDF slows a guess of a low-entropy secret, and a Cairn key
// has 256 random bits, which no guess reaches. A fast hash is enough here, and
// the lookup by hash stays a plain index read.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// wellFormed reports whether key has the shape NewKey gives: the prefix, then 43
// characters of the base64url alphabet. It is checked before any lookup, so a
// hostile header is refused without a database read.
func wellFormed(key string) bool {
	if len(key) != keyLen || key[:len(KeyPrefix)] != KeyPrefix {
		return false
	}
	for i := len(KeyPrefix); i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// bearerScheme is the auth scheme the header must carry, with its one space.
const bearerScheme = "Bearer "

// bearerKey returns the key in an Authorization header of the form
// "Bearer <key>", and reports whether the header is that and holds a well-formed
// key. The scheme is matched without regard to case (RFC 7235). The header is
// not trimmed: a space anywhere else makes it malformed.
func bearerKey(header string) (string, bool) {
	if len(header) < len(bearerScheme) || !equalFoldASCII(header[:len(bearerScheme)], bearerScheme) {
		return "", false
	}
	key := header[len(bearerScheme):]
	if !wellFormed(key) {
		return "", false
	}
	return key, true
}

// equalFoldASCII compares two strings of ASCII letters and spaces without regard
// to case. It takes a byte string, so a header with bytes past ASCII is simply
// unequal, and no rune decoding can fail on it.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
