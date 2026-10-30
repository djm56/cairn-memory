package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// newLocal returns a Service over a new database, for a test server to wrap.
func newLocal(t *testing.T) *core.Local {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return core.NewLocal(st)
}

// loopback serves svc through the api handlers on an httptest server, and
// returns a Client for it. The server binds 127.0.0.1 for the life of the test
// (D19); the test fails if it does not.
func loopback(t *testing.T, svc core.Service) *client.Client {
	t.Helper()
	srv := httptest.NewServer(api.Handler(svc, api.HeaderCaller))
	t.Cleanup(srv.Close)
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("test server at %s is not on loopback", srv.URL)
	}
	c, err := client.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c
}

// TestConformance runs the whole suite over the client, through an httptest
// server that wraps the api handlers over a real core. Every case must pass the
// same as it does against core directly (DESIGN.md §5.1).
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) core.Service {
		return loopback(t, newLocal(t))
	})
}

// failing is a Service whose TaskStart fails with err. Every other call is
// unimplemented.
type failing struct {
	core.Unimplemented
	err error
}

func (f failing) TaskStart(context.Context, core.TaskStartRequest) (*core.TaskStartResult, error) {
	return nil, f.err
}

// agentCtx is the context of an agent call in project acme-api.
func agentCtx() context.Context {
	return core.WithCaller(context.Background(), core.Caller{Project: "acme-api", Agent: "main", Role: core.RoleAgent})
}

// TestErrorsComeBackAsTheCoreError checks that each core kind, with its message
// and its missing list, reaches the client as the same core.Error.
func TestErrorsComeBackAsTheCoreError(t *testing.T) {
	cases := []*core.Error{
		{Kind: core.KindInvalid, Message: "fix the request"},
		{Kind: core.KindNotFound, Message: "task task-20261030-01 not found"},
		{Kind: core.KindRefused, Message: "close policy not met: summary, lessons", Missing: []string{"summary", "lessons"}},
		{Kind: core.KindOperatorOnly, Message: "an operator call"},
	}
	for _, want := range cases {
		c := loopback(t, failing{err: want})
		_, err := c.TaskStart(agentCtx(), core.TaskStartRequest{Title: "t", Goal: "g"})
		if got := core.KindOf(err); got != want.Kind {
			t.Errorf("%s: kind %q (err %v), want %q", want.Kind, got, err, want.Kind)
			continue
		}
		var ce *core.Error
		if !errors.As(err, &ce) || ce.Message != want.Message {
			t.Errorf("%s: error %v, want message %q", want.Kind, err, want.Message)
		}
		if got := core.MissingOf(err); !slices.Equal(got, want.Missing) {
			t.Errorf("%s: missing %v, want %v", want.Kind, got, want.Missing)
		}
	}
}

// TestInternalFailureIsAPlainError checks that a failure which is not a
// core.Error reaches the client as a plain error: it is not a core kind, it names
// the route and the status, and the cause does not travel.
func TestInternalFailureIsAPlainError(t *testing.T) {
	c := loopback(t, failing{err: errors.New("disk on fire")})
	_, err := c.TaskStart(agentCtx(), core.TaskStartRequest{Title: "t", Goal: "g"})
	if err == nil {
		t.Fatal("no error from an internal failure")
	}
	if kind := core.KindOf(err); kind != "" {
		t.Errorf("internal failure read as kind %q, want none", kind)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error %q does not name the status", err)
	}
	if strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("the cause reached the client: %q", err)
	}
}

// TestCallWithNoCallerIsInvalid checks that a context with no caller sends no
// Cairn headers, and so the server finds no project. The message names the fix.
func TestCallWithNoCallerIsInvalid(t *testing.T) {
	c := loopback(t, newLocal(t))
	_, err := c.TaskCurrent(context.Background())
	if core.KindOf(err) != core.KindInvalid || !strings.Contains(err.Error(), "cairn init") {
		t.Errorf("TaskCurrent with no caller = %v, want invalid and a message that names cairn init", err)
	}
}

// TestMalformedAnswerIsAnError checks that a success that is not the call's
// result is an error, and not a zero result that reads as success.
func TestMalformedAnswerIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.TaskCurrent(agentCtx())
	if err == nil || res != nil {
		t.Errorf("answer %q gave result %v and error %v, want an error", "not json", res, err)
	}
	if core.KindOf(err) != "" {
		t.Errorf("a malformed answer read as kind %q", core.KindOf(err))
	}
}

// TestNewRejectsBadBase checks that New refuses a base that is not an absolute
// http or https URL with a host, or that carries a query or fragment.
func TestNewRejectsBadBase(t *testing.T) {
	for _, base := range []string{
		"", "127.0.0.1:8787", "ftp://host", "http://", "http://host/?x=1", "http://host/#frag",
	} {
		if _, err := client.New(base, nil); err == nil {
			t.Errorf("New(%q) accepted a base that is not an http URL with a host", base)
		}
	}
	if _, err := client.New("http://127.0.0.1:8787/", nil); err != nil {
		t.Errorf("New with a trailing slash: %v", err)
	}
}
