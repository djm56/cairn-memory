package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
)

// testTimeout bounds one call in these tests, so a stuck server ends the test rather than
// the run.
const testTimeout = 10 * time.Second

// recorder is a server that counts every request it receives, and keeps the Authorization
// header of each one that carries it. It answers 200 with an empty object, so a request that
// reaches it looks like a success.
type recorder struct {
	mu    sync.Mutex
	calls int
	auth  []string
}

// ServeHTTP records the request and answers it.
func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.calls++
	if a := req.Header.Get("Authorization"); a != "" {
		r.auth = append(r.auth, a)
	}
	r.mu.Unlock()
	_, _ = w.Write([]byte("{}"))
}

// counts returns the number of requests the recorder received, and how many carried an
// Authorization header.
func (r *recorder) counts() (calls, withKey int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, len(r.auth)
}

// reset forgets the requests counted so far.
func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls, r.auth = 0, nil
}

// redirector answers every request with status and a Location of target, as a server that has
// moved would.
func redirector(status int, target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target)
		w.WriteHeader(status)
	})
}

// plainOrigin starts an http server for h, stopped at the end of the test.
func plainOrigin(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

// tlsOrigin starts an https server for h, stopped at the end of the test.
func tlsOrigin(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	return s
}

// TestTheKeyIsNeverSentAfterARedirect is 8-W3. A server that answers a call with 307 or 308,
// to another host, to another port on the same host, or to another scheme, must not get the
// key from the client. The client refuses the redirect and answers it as a plain error that
// does not print the Location, and the target receives no request at all. The same-host,
// other-port case is the one net/http would follow with the header, so it is the case that
// shows the refusal is what holds.
func TestTheKeyIsNeverSentAfterARedirect(t *testing.T) {
	const key = "cairn_KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK"
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		target := &recorder{}
		tgt := httptest.NewServer(target)
		t.Cleanup(tgt.Close)
		otherHost := strings.Replace(tgt.URL, "127.0.0.1", "localhost", 1)

		cases := []struct {
			what   string
			origin *httptest.Server
			hc     *http.Client
		}{
			{"another host", plainOrigin(t, redirector(status, otherHost)), &http.Client{Timeout: testTimeout}},
			{"the same host, another port", plainOrigin(t, redirector(status, tgt.URL)), &http.Client{Timeout: testTimeout}},
			{"another scheme, the same host", tlsOrigin(t, redirector(status, tgt.URL)), nil},
		}
		for _, c := range cases {
			hc := c.hc
			if hc == nil {
				// The TLS origin's own client trusts its certificate, and is given the timeout.
				hc = c.origin.Client()
				hc.Timeout = testTimeout
			}
			cl, err := client.NewWithKey(c.origin.URL, key, hc)
			if err != nil {
				t.Fatalf("%s, %d: NewWithKey: %v", c.what, status, err)
			}
			_, err = cl.TaskCurrent(agentCtx())
			switch {
			case err == nil:
				t.Errorf("%s, %d: the redirect answered a call with no error", c.what, status)
			case !strings.Contains(err.Error(), "redirect"):
				t.Errorf("%s, %d: err = %v; want it to say the redirect was not followed", c.what, status, err)
			case strings.Contains(err.Error(), tgt.URL), strings.Contains(err.Error(), "localhost"):
				t.Errorf("%s, %d: err = %v; the refusal prints the Location", c.what, status, err)
			case strings.Contains(err.Error(), key):
				t.Errorf("%s, %d: the refusal prints the key", c.what, status)
			}
			if calls, withKey := target.counts(); calls != 0 || withKey != 0 {
				t.Errorf("%s, %d: the target received %d requests, %d with an Authorization header; want none",
					c.what, status, calls, withKey)
			}
			target.reset()
		}
	}
}

// TestARedirectIsNotFollowedWithNoKey checks that the refusal does not depend on a key: a
// client built with New, which sends none, is answered the redirect as an error and does not
// follow it to the target.
func TestARedirectIsNotFollowedWithNoKey(t *testing.T) {
	target := &recorder{}
	tgt := httptest.NewServer(target)
	t.Cleanup(tgt.Close)
	origin := plainOrigin(t, redirector(http.StatusTemporaryRedirect, tgt.URL))
	cl, err := client.New(origin.URL, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := cl.TaskCurrent(context.Background()); err == nil {
		t.Errorf("a redirect answered a call with no error")
	}
	if calls, _ := target.counts(); calls != 0 {
		t.Errorf("the target received %d requests from a client with no key; want none", calls)
	}
}
