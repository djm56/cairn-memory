package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// The action tests post the forms of the web UI over a listener, through the real handler, the real
// core and a real store. Keys are made by addKey, which stores only the hash of a key the auth path
// made. A form is posted as a browser posts it: the session cookie, the same-origin Origin, and the
// csrf value the page carried.

// csrfField matches the csrf input of a rendered form. The value is unpadded base64url, so it
// needs no unescaping.
var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// The fixed messages the action tests look for. They are the UI's own texts (DESIGN §5.5).
const (
	textReadOnly     = "This key is read-only."
	textNotVerified  = "The form could not be verified."
	textNotFound     = "Nothing is recorded at this address."
	textConfirm      = "Retiring a directive is final."
	textNoteNeeded   = "A decision needs a note"
	textSwitchNeeded = "Each switch is on or off."
)

// csrfOf returns the csrf value of a rendered page, and fails the test when the page has none.
func csrfOf(t *testing.T, body string) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the page carries no csrf field")
	}
	return m[1]
}

// pageCSRF reads the page at path as cookie's browser, and returns its csrf value.
func pageCSRF(t *testing.T, base, path string, cookie *http.Cookie) string {
	t.Helper()
	_, body := readPage(t, noRedirect(), base+path, cookie)
	return csrfOf(t, body)
}

// formAs posts form to path as the browser of cookie does: with the session cookie and the
// same-origin Origin. It returns the answer and its body, as text.
func formAs(t *testing.T, base, path string, cookie *http.Cookie, form url.Values) (*http.Response, string) {
	t.Helper()
	headers := map[string]string{"Origin": base}
	if cookie != nil {
		headers["Cookie"] = cookie.Name + "=" + cookie.Value
	}
	resp, body := postForm(t, noRedirect(), base+path, form, headers)
	return resp, string(body)
}

// rowCount returns the single count query.
func rowCount(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// rowText returns the single text query.
func rowText(t *testing.T, st *store.Store, query string, args ...any) string {
	t.Helper()
	var s string
	if err := st.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("read %q: %v", query, err)
	}
	return s
}

// idText renders a row id as the decimal text a path or a form carries.
func idText(n int64) string {
	return strconv.FormatInt(n, 10)
}

// addLearning records a proposed finding through the agent route, as an agent key does, and
// returns its id.
func addLearning(t *testing.T, base, agentKey, title string) int64 {
	t.Helper()
	resp, body := send(t, client(nil), http.MethodPost, base+api.Prefix+api.RouteLearningAdd,
		`{"kind":"finding","title":"`+title+`","body":"seen in the check"}`, callHeaders(agentKey))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("learning_add: status %d: %s", resp.StatusCode, body)
	}
	var l struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &l); err != nil || l.ID == 0 {
		t.Fatalf("learning_add answered no id: %s", body)
	}
	return l.ID
}

// settingsValues is a complete settings form for acme-api: every switch on, the display name
// "Acme API", and csrf. over replaces any field it names.
func settingsValues(csrf string, over url.Values) url.Values {
	v := url.Values{
		"csrf": {csrf}, "display_name": {"Acme API"}, "parallel_tasks": {"on"},
		"require_summary": {"on"}, "require_criteria_results": {"on"}, "require_lessons": {"on"},
		"require_mistakes": {"on"}, "require_no_active_steps": {"on"},
	}
	for k, val := range over {
		v[k] = val
	}
	return v
}

// ratifyValues is a complete global ratify form with csrf. over replaces any field it names.
func ratifyValues(csrf string, over url.Values) url.Values {
	v := url.Values{"csrf": {csrf}, "scope": {"global"}, "title": {"A ratified rule"}, "rule": {"Keep the rule"}}
	for k, val := range over {
		v[k] = val
	}
	return v
}

// TestM3ExitCheck is M3's exit check (PLAN, M3 row): a lead sees a proposed finding, accepts it,
// ratifies a directive from it, and the next session's task_current returns that directive. A
// viewer key sees every page, and gets 403 read_only on every action.
func TestM3ExitCheck(t *testing.T) {
	st := openStore(t)
	f := seedPages(t, st, plainText)
	agent := addKey(t, st, "main-agent", auth.ScopeAgent)
	s := startPages(t, st)
	base := s.base
	c := noRedirect()

	// 1. An agent key records a proposed learning through the agent route.
	id := addLearning(t, base, agent, "Exit finding")

	// 2. The lead signs in and sees the proposed learning.
	lead := signInCookie(t, base, addKey(t, st, "lead", auth.ScopeOperator))
	resp, body := readPage(t, c, base+"/ui/learnings", lead)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Exit finding") {
		t.Fatalf("the learnings page does not show the proposed learning: status %d", resp.StatusCode)
	}

	// 3. The lead accepts it, with a note, using the csrf value of the page.
	csrf := csrfOf(t, body)
	resp, _ = formAs(t, base, "/ui/learnings/"+idText(id)+"/decide", lead, url.Values{
		"csrf": {csrf}, "status": {"accepted"}, "note": {"Accepted in the exit check"},
		"project": {""},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/p/acme-api/learnings" {
		t.Fatalf("decide: status %d, Location %q; want 303 to the project's learnings",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := rowText(t, st, `SELECT status FROM learnings WHERE id = ?`, id); got != "accepted" {
		t.Fatalf("the learning is %s after the decision, want accepted", got)
	}

	// 4. The lead opens the ratify form from the learning, then posts it.
	_, form := readPage(t, c, base+"/ui/directives/new?from_learning="+idText(id), lead)
	if !strings.Contains(form, `name="from_learning" value="`+idText(id)+`"`) {
		t.Fatal("the ratify form does not carry from_learning")
	}
	resp, _ = formAs(t, base, "/ui/directives", lead, ratifyValues(csrfOf(t, form), url.Values{
		"title": {"Exit directive"}, "rule": {"Ratified in the exit check"},
		"from_learning": {idText(id)},
	}))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/directives" {
		t.Fatalf("ratify: status %d, Location %q; want 303 to /ui/directives", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := rowText(t, st, `SELECT status FROM learnings WHERE id = ?`, id); got != "applied" {
		t.Fatalf("the learning is %s after the ratify, want applied", got)
	}

	// 5. A new agent session calls task_current, as the session-start path does.
	resp, body2 := send(t, client(nil), http.MethodPost, base+api.Prefix+api.RouteTaskCurrent, "{}", callHeaders(agent))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task_current: status %d: %s", resp.StatusCode, body2)
	}
	if !strings.Contains(string(body2), "Exit directive") {
		t.Errorf("task_current does not return the ratified directive: %s", body2)
	}

	// 6. A viewer key sees every page, with no action form, and gets 403 read_only on every action.
	watch := s.viewers
	for _, path := range append(pagePaths(f), "/ui/p/acme-api/learnings?status=all") {
		resp, body := readPage(t, c, base+path, watch)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("viewer GET %s: status %d, want 200", path, resp.StatusCode)
			continue
		}
		checkActionForms(t, path+" as viewer", "viewer", body)
	}
	resp, body = readPage(t, c, base+"/ui/directives/new", watch)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, textReadOnly) {
		t.Errorf("viewer GET /ui/directives/new: status %d, want 403 read_only", resp.StatusCode)
	}
	actions := []struct {
		path string
		form url.Values
	}{
		{"/ui/learnings/" + idText(id) + "/decide", url.Values{"status": {"rejected"}, "note": {"viewer"}, "csrf": {"x"}}},
		{"/ui/directives", ratifyValues("x", nil)},
		{"/ui/directives/D-0001/retire", url.Values{"confirm": {"yes"}, "csrf": {"x"}}},
		{"/ui/directives/D-0001/reaffirm", url.Values{"review_by": {"2100-01-01"}, "csrf": {"x"}}},
		{"/ui/projects/acme-api/settings", settingsValues("x", nil)},
	}
	for _, a := range actions {
		resp, body := formAs(t, base, a.path, watch, a.form)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, textReadOnly) {
			t.Errorf("viewer POST %s: status %d, want 403 read_only", a.path, resp.StatusCode)
		}
	}
	if n := rowCount(t, st, `SELECT COUNT(*) FROM directives WHERE status = 'retired'`); n != 0 {
		t.Errorf("a viewer's retire changed %d directives", n)
	}
}

// TestDecideRerendersWithTheTypedValues checks each decide failure: the status and the message
// the design gives, and that the page keeps what the operator typed.
func TestDecideRerendersWithTheTypedValues(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	agent := addKey(t, st, "main-agent", auth.ScopeAgent)
	s := startPages(t, st)
	id := addLearning(t, s.base, agent, "Typed learning")
	csrf := pageCSRF(t, s.base, "/ui/learnings", s.ops)
	cases := []struct {
		name, path string
		form       url.Values
		status     int
		want, keep string
	}{
		{"note missing", "/ui/learnings/" + idText(id) + "/decide",
			url.Values{"status": {"rejected"}, "note": {"  "}}, 400, textNoteNeeded, `value="rejected" selected`},
		{"superseded with no replacement", "/ui/learnings/" + idText(id) + "/decide",
			url.Values{"status": {"superseded"}, "note": {"why"}}, 400, "superseded_by", ""},
		{"superseded_by not a number", "/ui/learnings/" + idText(id) + "/decide",
			url.Values{"status": {"superseded"}, "superseded_by": {"abc"}, "note": {"why"}}, 400, "whole number", `value="abc"`},
		{"unknown status", "/ui/learnings/" + idText(id) + "/decide",
			url.Values{"status": {"maybe"}, "note": {"why"}}, 400, "unknown learning status", ""},
		{"a learning that does not exist", "/ui/learnings/999999/decide",
			url.Values{"status": {"accepted"}, "note": {"why"}}, 404, textNotFound, ""},
		{"an id that is not a number", "/ui/learnings/abc/decide",
			url.Values{"status": {"accepted"}, "note": {"why"}}, 404, textNotFound, ""},
	}
	for _, tc := range cases {
		tc.form.Set("csrf", csrf)
		resp, body := formAs(t, s.base, tc.path, s.ops, tc.form)
		if resp.StatusCode != tc.status || !strings.Contains(body, tc.want) {
			t.Errorf("%s: status %d, want %d with %q", tc.name, resp.StatusCode, tc.status, tc.want)
		}
		if tc.keep != "" && !strings.Contains(body, tc.keep) {
			t.Errorf("%s: the page does not keep the typed value %q", tc.name, tc.keep)
		}
	}
	if got := rowText(t, st, `SELECT status FROM learnings WHERE id = ?`, id); got != "proposed" {
		t.Errorf("a refused decision changed the learning to %s", got)
	}
}

// TestRatifyRefusalsAndValidation checks each ratify failure: its status and message, the typed
// values kept, and that no directive is written and no project row is made.
func TestRatifyRefusalsAndValidation(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	csrf := pageCSRF(t, s.base, "/ui/directives/new", s.ops)
	learning := addLearningFor(t, st)
	cases := []struct {
		name string
		over url.Values
		want string
		code int
		keep string
	}{
		{"no rule", url.Values{"rule": {""}, "title": {"Typed title"}}, "needs a rule", 400, `value="Typed title"`},
		{"a project with no row", url.Values{"scope": {"project"}, "project": {"nope"}}, "There is no project named nope", 400, `value="nope"`},
		{"a project name that is not valid", url.Values{"scope": {"project"}, "project": {"Not_Valid"}}, "valid project name", 400, ""},
		{"a review_by in the past", url.Values{"review_by": {"2000-01-01"}}, "not in the future", 400, `value="2000-01-01"`},
		{"a review_by that is not a date", url.Values{"review_by": {"31/12/2099"}}, "not a date", 400, ""},
		{"from_learning that is not a number", url.Values{"from_learning": {"abc"}}, "whole number", 400, ""},
		{"from a learning that is not accepted", url.Values{"from_learning": {learning}}, "only an accepted learning", 409, ""},
		{"from a learning that does not exist", url.Values{"from_learning": {"999999"}}, textNotFound, 404, ""},
	}
	for _, tc := range cases {
		resp, body := formAs(t, s.base, "/ui/directives", s.ops, ratifyValues(csrf, tc.over))
		if resp.StatusCode != tc.code || !strings.Contains(body, tc.want) {
			t.Errorf("%s: status %d, want %d with %q", tc.name, resp.StatusCode, tc.code, tc.want)
		}
		if tc.keep != "" && !strings.Contains(body, tc.keep) {
			t.Errorf("%s: the page does not keep the typed value %q", tc.name, tc.keep)
		}
	}
	// The fixture holds one directive, D-0001. A refused ratify writes none.
	if n := rowCount(t, st, `SELECT COUNT(*) FROM directives`); n != 1 {
		t.Errorf("refused ratifies left %d directives, want the fixture's 1", n)
	}
	if n := rowCount(t, st, `SELECT COUNT(*) FROM projects WHERE name = 'nope'`); n != 0 {
		t.Errorf("a ratify of a misspelt project created %d project rows", n)
	}
	_, body := readPage(t, noRedirect(), s.base+"/ui/directives/new?from_learning=abc", s.ops)
	if !strings.Contains(body, "whole number") {
		t.Error("a GET of the ratify form with a bad from_learning does not say why")
	}
}

// addLearningFor records a proposed learning directly through core, as an agent's own call does,
// and returns its id as text. The learning stays proposed, so a ratify from it is refused.
func addLearningFor(t *testing.T, st *store.Store) string {
	t.Helper()
	svc := core.NewLocal(st)
	l, err := svc.LearningAdd(core.WithCaller(context.Background(), core.Caller{
		Project: "acme-api", Agent: "main", Role: core.RoleAgent}), core.LearningAddRequest{
		Kind: core.LearningFinding, Title: "Proposed only", Body: "not accepted",
	})
	if err != nil {
		t.Fatalf("learning_add: %v", err)
	}
	return idText(l.ID)
}

// TestRetireNeedsTheConfirmBox checks that retiring without confirm=yes is refused and retires
// nothing, that a retire with it is final, and that a second retire is refused.
func TestRetireNeedsTheConfirmBox(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	csrf := pageCSRF(t, s.base, "/ui/directives", s.ops)
	for _, form := range []url.Values{
		{"csrf": {csrf}},
		{"csrf": {csrf}, "confirm": {"no"}},
	} {
		resp, body := formAs(t, s.base, "/ui/directives/D-0001/retire", s.ops, form)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, textConfirm) {
			t.Errorf("retire without confirm=yes: status %d, want 400 with the confirm message", resp.StatusCode)
		}
	}
	if got := rowText(t, st, `SELECT status FROM directives WHERE code = 'D-0001'`); got != "active" {
		t.Fatalf("a retire without confirm=yes left D-0001 %s, want active", got)
	}
	resp, _ := formAs(t, s.base, "/ui/directives/D-0001/retire", s.ops, url.Values{"csrf": {csrf}, "confirm": {"yes"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/directives" {
		t.Fatalf("retire with confirm=yes: status %d, want 303 to /ui/directives", resp.StatusCode)
	}
	if got := rowText(t, st, `SELECT status FROM directives WHERE code = 'D-0001'`); got != "retired" {
		t.Fatalf("D-0001 is %s after the confirmed retire, want retired", got)
	}
	resp, body := formAs(t, s.base, "/ui/directives/D-0001/retire", s.ops, url.Values{"csrf": {csrf}, "confirm": {"yes"}})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "already retired") {
		t.Errorf("a second retire: status %d, want 409 already retired", resp.StatusCode)
	}
	resp, _ = formAs(t, s.base, "/ui/directives/D-9999/retire", s.ops, url.Values{"csrf": {csrf}, "confirm": {"yes"}})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("retire of an unknown code: status %d, want 404", resp.StatusCode)
	}
}

// TestReaffirmWritesOneHistoryRowAndRefusesARetiredDirective checks a reaffirm through the form:
// the day moves, the code does not, and one history row is written; a bad day is re-rendered with
// the typed day; a retired directive is refused and writes nothing.
func TestReaffirmWritesOneHistoryRowAndRefusesARetiredDirective(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	csrf := pageCSRF(t, s.base, "/ui/directives", s.ops)
	reaffirm := "/ui/directives/D-0001/reaffirm"

	resp, body := formAs(t, s.base, reaffirm, s.ops, url.Values{"csrf": {csrf}, "review_by": {"2000-01-01"}})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, `value="2000-01-01"`) {
		t.Errorf("reaffirm to a past day: status %d, want 400 keeping the typed day", resp.StatusCode)
	}
	if n := rowCount(t, st, `SELECT COUNT(*) FROM directive_reaffirmations`); n != 0 {
		t.Errorf("a refused reaffirm wrote %d history rows", n)
	}

	resp, _ = formAs(t, s.base, reaffirm, s.ops, url.Values{"csrf": {csrf}, "review_by": {"2100-06-30"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/directives" {
		t.Fatalf("reaffirm: status %d, want 303 to /ui/directives", resp.StatusCode)
	}
	if n := rowCount(t, st, `SELECT COUNT(*) FROM directive_reaffirmations`); n != 1 {
		t.Errorf("reaffirm wrote %d history rows, want 1", n)
	}
	if got := rowText(t, st, `SELECT code FROM directives WHERE code = 'D-0001'`); got != "D-0001" {
		t.Errorf("the reaffirm changed the code to %s", got)
	}
	if got := rowText(t, st, `SELECT review_by FROM directives WHERE code = 'D-0001'`); !strings.HasPrefix(got, "2100-06-30") {
		t.Errorf("review_by after the reaffirm = %s, want 2100-06-30", got)
	}

	csrf = pageCSRF(t, s.base, "/ui/directives", s.ops)
	resp, _ = formAs(t, s.base, "/ui/directives/D-0001/retire", s.ops, url.Values{"csrf": {csrf}, "confirm": {"yes"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("retire before the refused reaffirm: status %d", resp.StatusCode)
	}
	resp, _ = formAs(t, s.base, reaffirm, s.ops, url.Values{"csrf": {csrf}, "review_by": {"2100-07-01"}})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("reaffirm of a retired directive: status %d, want 409", resp.StatusCode)
	}
	if n := rowCount(t, st, `SELECT COUNT(*) FROM directive_reaffirmations`); n != 1 {
		t.Errorf("a refused reaffirm of a retired directive wrote a row: %d rows, want 1", n)
	}
}

// TestActionsRefuseAMissingOrWrongCSRF checks that each action route refuses a form with no csrf
// value, and one carrying the value of another session, with the form message. The state is the
// same afterwards.
func TestActionsRefuseAMissingOrWrongCSRF(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	agent := addKey(t, st, "main-agent", auth.ScopeAgent)
	s := startPages(t, st)
	id := addLearning(t, s.base, agent, "Guarded learning")
	other := signInCookie(t, s.base, addKey(t, st, "other", auth.ScopeOperator))
	foreign := pageCSRF(t, s.base, "/ui/directives", other)
	routes := []struct {
		path string
		form url.Values
	}{
		{"/ui/learnings/" + idText(id) + "/decide", url.Values{"status": {"accepted"}, "note": {"why"}}},
		{"/ui/directives", ratifyValues("", nil)},
		{"/ui/directives/D-0001/retire", url.Values{"confirm": {"yes"}}},
		{"/ui/directives/D-0001/reaffirm", url.Values{"review_by": {"2100-01-01"}}},
		{"/ui/projects/acme-api/settings", settingsValues("", nil)},
	}
	for _, r := range routes {
		for _, token := range []string{"", foreign} {
			form := url.Values{}
			for k, v := range r.form {
				form[k] = v
			}
			if token == "" {
				form.Del("csrf")
			} else {
				form.Set("csrf", token)
			}
			resp, body := formAs(t, s.base, r.path, s.ops, form)
			if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, textNotVerified) {
				t.Errorf("POST %s with csrf %q: status %d, want 403 with the form message", r.path, token, resp.StatusCode)
			}
		}
	}
	if got := rowText(t, st, `SELECT status FROM learnings WHERE id = ?`, id); got != "proposed" {
		t.Errorf("a refused decision changed the learning to %s", got)
	}
	if got := rowText(t, st, `SELECT status FROM directives WHERE code = 'D-0001'`); got != "active" {
		t.Errorf("a refused retire left D-0001 %s", got)
	}
	if got := rowText(t, st, `SELECT display_name FROM projects WHERE name = 'acme-api'`); got != "Acme API" {
		t.Errorf("a refused save changed the display name to %q", got)
	}
}

// TestViewerIsRefusedBeforeTheBodyIsRead checks that a viewer's POST is answered 403 read_only
// before the body is read (DESIGN §5.5, Viewer mode): an oversized body gets the role's answer, not
// the 413 a body read would give. The body is larger than the form cap and the server's own cap.
func TestViewerIsRefusedBeforeTheBodyIsRead(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	big := "csrf=x&note=" + strings.Repeat("a", 2<<20)
	for _, path := range []string{
		"/ui/learnings/1/decide", "/ui/directives", "/ui/directives/D-0001/retire",
		"/ui/directives/D-0001/reaffirm", "/ui/projects/acme-api/settings",
	} {
		req, err := http.NewRequest(http.MethodPost, s.base+path, strings.NewReader(big))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", s.base)
		req.Header.Set("Cookie", s.viewers.Name+"="+s.viewers.Value)
		got, err := noRedirect().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		answer, err := io.ReadAll(got.Body)
		got.Body.Close()
		if err != nil {
			t.Fatalf("read answer: %v", err)
		}
		if got.StatusCode != http.StatusForbidden || !strings.Contains(string(answer), textReadOnly) {
			t.Errorf("viewer POST %s with an oversized body: status %d, want 403 read_only", path, got.StatusCode)
		}
	}
}

// TestNoActionCreatesAProjectThroughAMisspeltName checks that the two actions which name a project
// make no row for a name that has none: a settings save is 404, and a directive for the project is
// refused with the message.
func TestNoActionCreatesAProjectThroughAMisspeltName(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	before := rowCount(t, st, `SELECT COUNT(*) FROM projects`)
	csrf := pageCSRF(t, s.base, "/ui/directives", s.ops)

	resp, _ := formAs(t, s.base, "/ui/projects/acme-apii/settings", s.ops, settingsValues(csrf, nil))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("settings for a misspelt project: status %d, want 404", resp.StatusCode)
	}
	resp, _ = formAs(t, s.base, "/ui/directives", s.ops, ratifyValues(csrf, url.Values{
		"scope": {"project"}, "project": {"acme-apii"},
	}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ratify for a misspelt project: status %d, want 400", resp.StatusCode)
	}
	if after := rowCount(t, st, `SELECT COUNT(*) FROM projects`); after != before {
		t.Errorf("misspelt names created %d project rows", after-before)
	}
}

// TestSettingsRoundTripThroughOneDecoder checks that a saved settings object reads back, through
// project_list, as it was saved: the decoder that the close rules use. It also checks the refusal
// of a switch that is missing or not on or off, with the typed name kept.
func TestSettingsRoundTripThroughOneDecoder(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	csrf := pageCSRF(t, s.base, "/ui/projects/acme-api/settings", s.ops)

	resp, _ := formAs(t, s.base, "/ui/projects/acme-api/settings", s.ops, settingsValues(csrf, url.Values{
		"display_name": {"Round Trip"}, "parallel_tasks": {"off"},
		"require_summary": {"off"}, "require_lessons": {"off"},
	}))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/projects/acme-api/settings" {
		t.Fatalf("settings save: status %d, want 303 to the settings page", resp.StatusCode)
	}
	list, err := core.NewLocal(st).ProjectList(core.WithCaller(context.Background(),
		core.Caller{Agent: "reader", Role: core.RoleOperator}), core.ProjectListRequest{Project: "acme-api"})
	if err != nil || len(list.Projects) != 1 {
		t.Fatalf("project_list: %v, %d projects", err, len(list.Projects))
	}
	p := list.Projects[0]
	c := p.Settings.ClosePolicy
	if p.DisplayName != "Round Trip" || p.Settings.ParallelTasks ||
		c.RequireSummary || c.RequireLessons || !c.RequireCriteriaResults ||
		!c.RequireMistakes || !c.RequireNoActiveSteps {
		t.Errorf("saved settings read back as %q %+v, not as saved", p.DisplayName, p.Settings)
	}

	csrf = pageCSRF(t, s.base, "/ui/projects/acme-api/settings", s.ops)
	resp, body := formAs(t, s.base, "/ui/projects/acme-api/settings", s.ops, settingsValues(csrf, url.Values{
		"require_mistakes": {"maybe"}, "display_name": {"Typed name"},
	}))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, textSwitchNeeded) || !strings.Contains(body, `value="Typed name"`) {
		t.Errorf("a switch that is not on or off: status %d, want 400 keeping the typed name", resp.StatusCode)
	}
}
