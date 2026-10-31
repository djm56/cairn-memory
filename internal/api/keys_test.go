package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// recorder is a Service that answers three calls and records the caller of each. It
// reads the role a request carried, which is what the key resolver sets.
type recorder struct {
	core.Unimplemented
	callers []core.Caller
}

func (r *recorder) note(ctx context.Context) {
	c, _ := core.CallerFrom(ctx)
	r.callers = append(r.callers, c)
}

// TaskCurrent is a read: any key may call it.
func (r *recorder) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	r.note(ctx)
	return &core.TaskCurrent{}, nil
}

// TaskStart is a write.
func (r *recorder) TaskStart(ctx context.Context, _ core.TaskStartRequest) (*core.TaskStartResult, error) {
	r.note(ctx)
	return &core.TaskStartResult{}, nil
}

// DirectiveRetire is an operator write.
func (r *recorder) DirectiveRetire(ctx context.Context, _ core.DirectiveRetireRequest) (*core.Directive, error) {
	r.note(ctx)
	return &core.Directive{}, nil
}

// openKeyStore opens a fresh database in the test's directory.
func openKeyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// addKey adds a key named name with scope to st, and returns its plaintext. The
// store keeps only the hash, so the plaintext is known to the test alone, as it is
// to an operator who has just made a key.
func addKey(t *testing.T, st *store.Store, name string, scope auth.Scope) string {
	t.Helper()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		_, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
			Name: name, Hash: auth.HashKey(key), Scope: string(scope), CreatedAt: time.Now(),
		})
		return err
	}); err != nil {
		t.Fatalf("add key %s: %v", name, err)
	}
	return key
}

// keyedHandler serves svc behind the key resolver over st, as cairn serve will.
func keyedHandler(st *store.Store, svc core.Service) http.Handler {
	return api.Handler(svc, api.KeyCaller(auth.NewResolver(api.NewStoreLookup(st), nil)), nil)
}

// bearer returns the headers of a call with key, from a project and an agent.
func bearer(key string) map[string]string {
	return map[string]string{
		api.HeaderProject: "acme-api",
		api.HeaderAgent:   "main",
		"Authorization":   "Bearer " + key,
	}
}

// TestKeyScopeSetsTheRole checks that the role of each call comes from the key's
// scope, and that X-Cairn-Role is ignored: an agent key that claims the operator
// role in the header is still an agent.
func TestKeyScopeSetsTheRole(t *testing.T) {
	st := openKeyStore(t)
	rec := &recorder{}
	h := keyedHandler(st, rec)
	agent := addKey(t, st, "ci", auth.ScopeAgent)
	operator := addKey(t, st, "ops", auth.ScopeOperator)
	viewer := addKey(t, st, "dash", auth.ScopeViewer)

	cases := []struct {
		what   string
		key    string
		header map[string]string
		want   core.Role
	}{
		{"an agent key", agent, nil, core.RoleAgent},
		{"an operator key", operator, nil, core.RoleOperator},
		{"a viewer key", viewer, nil, core.RoleViewer},
		{"an agent key claiming the operator role", agent, map[string]string{api.HeaderRole: string(core.RoleOperator)}, core.RoleAgent},
	}
	for _, c := range cases {
		rec.callers = nil
		hdr := bearer(c.key)
		for k, v := range c.header {
			hdr[k] = v
		}
		got := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", hdr)
		if got.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200: %s", c.what, got.Code, got.Body.String())
		}
		if len(rec.callers) != 1 || rec.callers[0].Role != c.want {
			t.Errorf("%s: caller = %+v, want role %s", c.what, rec.callers, c.want)
			continue
		}
		if rec.callers[0].Project != "acme-api" || rec.callers[0].Agent != "main" {
			t.Errorf("%s: caller = %+v, want the project and agent from the headers", c.what, rec.callers[0])
		}
	}
}

// TestViewerIsRefusedOnEveryWrite checks one read and one write per scope, as the
// step plan asks: a viewer key passes the read and is refused the write, with 403
// and the kind read_only, before the service is reached. The operator write is
// refused too, since it writes.
func TestViewerIsRefusedOnEveryWrite(t *testing.T) {
	st := openKeyStore(t)
	rec := &recorder{}
	h := keyedHandler(st, rec)
	viewer := addKey(t, st, "dash", auth.ScopeViewer)
	agent := addKey(t, st, "ci", auth.ScopeAgent)

	got := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", bearer(viewer))
	if got.Code != http.StatusOK || len(rec.callers) != 1 {
		t.Fatalf("viewer read: status %d, %d calls; want 200 and one call", got.Code, len(rec.callers))
	}

	for _, route := range []string{api.RouteTaskStart, api.RouteDirectiveRetire} {
		rec.callers = nil
		got = serve(h, http.MethodPost, api.Prefix+route, "", bearer(viewer))
		if got.Code != http.StatusForbidden {
			t.Errorf("viewer write %s: status %d, want 403: %s", route, got.Code, got.Body.String())
			continue
		}
		if e := errorBody(t, got); e.Kind != api.KindReadOnly {
			t.Errorf("viewer write %s: kind %s, want read_only", route, e.Kind)
		}
		if len(rec.callers) != 0 {
			t.Errorf("viewer write %s reached the service: %d calls", route, len(rec.callers))
		}
	}

	// An agent write passes the gate: the same route answers for an agent.
	rec.callers = nil
	got = serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, "", bearer(agent))
	if got.Code != http.StatusOK || len(rec.callers) != 1 {
		t.Errorf("agent write: status %d, %d calls; want 200 and one call", got.Code, len(rec.callers))
	}
}

// TestAgentKeyIsRefusedOnOperatorRoute is the security property issue #110 exists
// for. It runs over the real core, so the refusal is core's own check: an agent key
// that sends X-Cairn-Role: operator is still refused with operator_only, and the
// same route answers an operator key.
func TestAgentKeyIsRefusedOnOperatorRoute(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, core.NewLocal(st))
	agent := addKey(t, st, "ci", auth.ScopeAgent)
	operator := addKey(t, st, "ops", auth.ScopeOperator)
	path := api.Prefix + api.RouteDirectiveRetire

	forged := bearer(agent)
	forged[api.HeaderRole] = string(core.RoleOperator)
	got := serve(h, http.MethodPost, path, `{"code":"D-0001"}`, forged)
	if got.Code != http.StatusForbidden {
		t.Fatalf("agent key with X-Cairn-Role: operator: status %d, want 403: %s", got.Code, got.Body.String())
	}
	if e := errorBody(t, got); e.Kind != core.KindOperatorOnly {
		t.Errorf("agent key with X-Cairn-Role: operator: kind %s, want operator_only", e.Kind)
	}

	got = serve(h, http.MethodPost, path, `{"code":"D-0001"}`, bearer(agent))
	if got.Code != http.StatusForbidden || errorBody(t, got).Kind != core.KindOperatorOnly {
		t.Errorf("agent key: status %d, want 403 operator_only: %s", got.Code, got.Body.String())
	}

	got = serve(h, http.MethodPost, path, `{"code":"D-0001"}`, bearer(operator))
	if got.Code == http.StatusForbidden {
		t.Errorf("operator key was refused on an operator route: %s", got.Body.String())
	}
}

// TestMissingOrBadKeyIsUnauthenticated checks each header that names no active key:
// absent, malformed, unknown, revoked, and an unknown scheme. Each is 401 with the
// kind unauthenticated, and none reaches the service or echoes the value.
func TestMissingOrBadKeyIsUnauthenticated(t *testing.T) {
	st := openKeyStore(t)
	rec := &recorder{}
	h := keyedHandler(st, rec)
	revoked := addKey(t, st, "old", auth.ScopeAgent)
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		return store.RevokeAPIKey(context.Background(), q, "old", time.Now())
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	unknown, _ := auth.NewKey()

	cases := []struct {
		what   string
		header string
	}{
		{"no header", ""},
		{"an empty value", "Bearer "},
		{"the scheme alone", "Bearer"},
		{"another scheme", "Basic " + unknown},
		{"a malformed key", "Bearer cairn_short"},
		{"an unknown key", "Bearer " + unknown},
		{"a revoked key", "Bearer " + revoked},
	}
	for _, c := range cases {
		rec.callers = nil
		hdr := map[string]string{api.HeaderProject: "acme-api", api.HeaderAgent: "main"}
		if c.header != "" {
			hdr["Authorization"] = c.header
		}
		got := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", hdr)
		if got.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401: %s", c.what, got.Code, got.Body.String())
			continue
		}
		if e := errorBody(t, got); e.Kind != api.KindUnauthenticated {
			t.Errorf("%s: kind %s, want unauthenticated", c.what, e.Kind)
		}
		if len(rec.callers) != 0 {
			t.Errorf("%s reached the service", c.what)
		}
		// The credential is the part after the scheme. The message names the scheme
		// itself, so the whole header is not searched for: only the value is.
		if value := credential(c.header); len(value) >= 8 && strings.Contains(got.Body.String(), value) {
			t.Errorf("%s: the answer echoes the credential", c.what)
		}
	}
}

// credential returns the part of an Authorization value after its scheme, or "" when
// there is no scheme separator.
func credential(header string) string {
	if i := strings.IndexByte(header, ' '); i >= 0 {
		return header[i+1:]
	}
	return ""
}

// TestHostileAuthorizationHeaders sends the headers an attacker would: one of a
// mebibyte, and the malformed forms. Each is 401, with no panic (a panic would be
// 500) and no echo of the value, and the answer stays small.
func TestHostileAuthorizationHeaders(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, &recorder{})
	huge := "Bearer " + strings.Repeat("A", 1<<20)
	for _, c := range []struct {
		what   string
		header string
	}{
		{"a 1 MiB bearer", huge},
		{"the scheme alone", "Bearer"},
		{"a basic scheme", "Basic x"},
		{"an empty value", ""},
	} {
		req := httptest.NewRequest(http.MethodPost, api.Prefix+api.RouteTaskCurrent, strings.NewReader(""))
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", c.what, rec.Code)
			continue
		}
		if e := errorBody(t, rec); e.Kind != api.KindUnauthenticated {
			t.Errorf("%s: kind %s, want unauthenticated", c.what, e.Kind)
		}
		if rec.Body.Len() > 1024 || strings.Contains(rec.Body.String(), strings.Repeat("A", 64)) {
			t.Errorf("%s: the answer is %d bytes and may echo the value", c.what, rec.Body.Len())
		}
	}
}
