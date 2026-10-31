package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNewKeyShape checks the form NewKey gives: the prefix, 32 bytes of entropy in
// unpadded base64url, and a fresh key each time.
func TestNewKeyShape(t *testing.T) {
	a, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	b, _ := NewKey()
	if a == b {
		t.Fatal("two keys are equal")
	}
	if !strings.HasPrefix(a, KeyPrefix) || len(a) != keyLen {
		t.Fatalf("key %q: want prefix %q and length %d", a, KeyPrefix, keyLen)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(a, KeyPrefix))
	if err != nil || len(raw) != keyBytes {
		t.Fatalf("payload decodes to %d bytes, err %v; want %d", len(raw), err, keyBytes)
	}
	if !wellFormed(a) {
		t.Errorf("NewKey gave a key that wellFormed refuses")
	}
}

// TestHashKeyIsSHA256Hex checks HashKey against the SHA-256 test vector for "abc".
func TestHashKeyIsSHA256Hex(t *testing.T) {
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := HashKey("abc"); got != want {
		t.Errorf("HashKey(abc) = %s, want %s", got, want)
	}
}

// TestBearerKeyAcceptsOnlyTheForm checks the header grammar. Each refused header
// is one the resolver must answer with ErrUnauthenticated, never with a key.
func TestBearerKeyAcceptsOnlyTheForm(t *testing.T) {
	good := "cairn_" + strings.Repeat("A", 43)
	cases := []struct {
		what   string
		header string
		ok     bool
	}{
		{"the form", "Bearer " + good, true},
		{"a lower-case scheme", "bearer " + good, true},
		{"no header", "", false},
		{"the scheme alone", "Bearer", false},
		{"the scheme and a space", "Bearer ", false},
		{"another scheme", "Basic " + good, false},
		{"a double space", "Bearer  " + good, false},
		{"a trailing space", "Bearer " + good + " ", false},
		{"a short key", "Bearer cairn_abc", false},
		{"a long key", "Bearer " + good + "A", false},
		{"no prefix", "Bearer " + strings.Repeat("A", 49), false},
		{"padding", "Bearer cairn_" + strings.Repeat("A", 42) + "=", false},
		{"the standard alphabet", "Bearer cairn_" + strings.Repeat("+", 43), false},
		{"non-ASCII", "Bearer cairn_" + strings.Repeat("é", 21) + "A", false},
	}
	for _, c := range cases {
		_, ok := bearerKey(c.header)
		if ok != c.ok {
			t.Errorf("%s: bearerKey ok = %v, want %v", c.what, ok, c.ok)
		}
	}
}

// fakeLookup is a Lookup over a map of hash to key, and it counts its calls.
type fakeLookup struct {
	keys    map[string]Key
	findErr error
	touches int
	touchOK error
	lastID  int64
}

func (f *fakeLookup) FindActive(_ context.Context, hash string) (Key, error) {
	if f.findErr != nil {
		return Key{}, f.findErr
	}
	k, ok := f.keys[hash]
	if !ok {
		return Key{}, ErrUnknownKey
	}
	return k, nil
}

func (f *fakeLookup) Touch(_ context.Context, id int64, _ time.Time) error {
	f.touches++
	f.lastID = id
	return f.touchOK
}

// newTestResolver returns a resolver over f with its clock under the test's control.
func newTestResolver(f *fakeLookup, clock *time.Time, hook func(error)) *Resolver {
	r := NewResolver(f, hook)
	r.now = func() time.Time { return *clock }
	return r
}

// TestResolveRefusesWithoutDetail checks that a header naming no active key gets
// ErrUnauthenticated, and that the lookup of an unknown hash is the same answer.
func TestResolveRefusesWithoutDetail(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLookup{keys: map[string]Key{}}
	clock := time.Now()
	r := newTestResolver(f, &clock, nil)
	for _, h := range []string{"", "Bearer", "Basic " + key, "Bearer " + key} {
		if _, err := r.Resolve(context.Background(), h); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("header %q: err = %v, want ErrUnauthenticated", h[:min(len(h), 12)], err)
		}
	}
	if f.touches != 0 {
		t.Errorf("a refused key was stamped %d times", f.touches)
	}
}

// TestResolveReturnsTheScope checks that a known active key comes back with its
// id, name and scope, and that its last_used_at is written.
func TestResolveReturnsTheScope(t *testing.T) {
	key, _ := NewKey()
	f := &fakeLookup{keys: map[string]Key{HashKey(key): {ID: 7, Name: "ci", Scope: ScopeViewer}}}
	clock := time.Now()
	r := newTestResolver(f, &clock, nil)
	k, err := r.Resolve(context.Background(), "Bearer "+key)
	if err != nil || k.Scope != ScopeViewer || k.ID != 7 || k.Name != "ci" {
		t.Fatalf("Resolve = %+v, %v; want ci, viewer, id 7", k, err)
	}
	if f.touches != 1 || f.lastID != 7 {
		t.Errorf("touches = %d (id %d), want one for id 7", f.touches, f.lastID)
	}
}

// TestResolveTouchesOncePerMinute checks the throttle: repeated calls within a
// minute write last_used_at once, and a call a minute later writes it again.
func TestResolveTouchesOncePerMinute(t *testing.T) {
	key, _ := NewKey()
	f := &fakeLookup{keys: map[string]Key{HashKey(key): {ID: 1, Name: "ci", Scope: ScopeAgent}}}
	clock := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	r := newTestResolver(f, &clock, nil)
	for i := 0; i < 5; i++ {
		clock = clock.Add(5 * time.Second)
		if _, err := r.Resolve(context.Background(), "Bearer "+key); err != nil {
			t.Fatal(err)
		}
	}
	if f.touches != 1 {
		t.Fatalf("touches within a minute = %d, want 1", f.touches)
	}
	clock = clock.Add(time.Minute)
	if _, err := r.Resolve(context.Background(), "Bearer "+key); err != nil {
		t.Fatal(err)
	}
	if f.touches != 2 {
		t.Errorf("touches after a minute = %d, want 2", f.touches)
	}
}

// TestResolveSurvivesATouchFailure checks that a failed last_used_at write does not
// refuse the call: the failure goes to the hook, and the key is returned.
func TestResolveSurvivesATouchFailure(t *testing.T) {
	key, _ := NewKey()
	f := &fakeLookup{
		keys:    map[string]Key{HashKey(key): {ID: 1, Name: "ci", Scope: ScopeAgent}},
		touchOK: errors.New("disk full"),
	}
	clock := time.Now()
	var hooked []error
	r := newTestResolver(f, &clock, func(err error) { hooked = append(hooked, err) })
	k, err := r.Resolve(context.Background(), "Bearer "+key)
	if err != nil || k.Name != "ci" {
		t.Fatalf("Resolve = %+v, %v; want the key, no error", k, err)
	}
	if len(hooked) != 1 {
		t.Errorf("hook calls = %d, want 1", len(hooked))
	}
}

// TestResolveLookupFailureIsNotUnauthenticated checks that a failing lookup is not
// reported as a refused key: it is a server fault, and the caller answers it as one.
func TestResolveLookupFailureIsNotUnauthenticated(t *testing.T) {
	key, _ := NewKey()
	f := &fakeLookup{findErr: errors.New("database is locked")}
	clock := time.Now()
	r := newTestResolver(f, &clock, nil)
	_, err := r.Resolve(context.Background(), "Bearer "+key)
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want a lookup failure that is not ErrUnauthenticated", err)
	}
}

// TestResolveRefusesAnUnknownScope checks that a row whose scope is none of the
// three is an error, not an agent: an unknown scope must not grant anything.
func TestResolveRefusesAnUnknownScope(t *testing.T) {
	key, _ := NewKey()
	f := &fakeLookup{keys: map[string]Key{HashKey(key): {ID: 1, Name: "odd", Scope: "admin"}}}
	clock := time.Now()
	r := newTestResolver(f, &clock, nil)
	_, err := r.Resolve(context.Background(), "Bearer "+key)
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want a failure for scope admin", err)
	}
}
