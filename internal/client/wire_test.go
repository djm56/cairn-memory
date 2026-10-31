package client_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestUnknownRouteIsNoRoute checks that a client whose base URL has a path the
// server does not serve gets the kind no_route, and a message that names its own
// base URL and tells the caller to check it. A missing entity is not_found, and is
// tested in the api package.
func TestUnknownRouteIsNoRoute(t *testing.T) {
	srv := httptest.NewServer(api.Handler(newLocal(t), api.UnauthenticatedHeaderCaller, nil))
	t.Cleanup(srv.Close)
	base := srv.URL + "/cairn"
	c, err := client.New(base, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.TaskCurrent(agentCtx())
	if got := core.KindOf(err); got != api.KindNoRoute {
		t.Fatalf("kind %q (err %v), want no_route", got, err)
	}
	if !strings.Contains(err.Error(), "check the server base URL") || !strings.Contains(err.Error(), base) {
		t.Errorf("error %q does not name the base URL %s and tell the caller to check it", err, base)
	}
	if want := base + api.Prefix + api.RouteTaskCurrent; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the path the client sent, %s", err, want)
	}
}

// TestCallSendsJSONContentType checks that a call sends its body as
// application/json, the type the server requires (DESIGN §5.4).
func TestCallSendsJSONContentType(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.TaskCurrent(agentCtx()); err != nil {
		t.Fatalf("TaskCurrent: %v", err)
	}
	if got := <-seen; got != "application/json" {
		t.Errorf("Content-Type %q, want application/json", got)
	}
}

// TestClientIgnoresExtraResponseFields checks the rule that a server may add a
// response field without a new prefix (DESIGN §5.4): an answer with fields the
// client does not know, at the top level and inside a known object, decodes into
// the result as if they were not there.
func TestClientIgnoresExtraResponseFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task":{"id":"task-20261031-01","added_later":"x"},"added_later":[1,2],"added_object":{"a":true}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.TaskCurrent(agentCtx())
	if err != nil {
		t.Fatalf("an answer with extra fields failed: %v", err)
	}
	if res.Task == nil || res.Task.ID != "task-20261031-01" {
		t.Errorf("result %+v, want the known task id", res)
	}
}
