package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// The page tests run the read pages over a listener, the real handler, the real core and a real
// store, so a stored value is written through core and read back by the page. They sit here, not
// in internal/ui, because internal/ui keeps to no store.

// pageCSP is the content security policy of every answer under /ui/ (DESIGN §5.5, Headers).
const pageCSP = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'"

// checkPageHeaders fails the test unless resp carries the security headers of DESIGN §5.5.
func checkPageHeaders(t *testing.T, label string, resp *http.Response) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": pageCSP,
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
		"Cache-Control":           "no-store",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s: %s = %q, want %q", label, k, got, v)
		}
	}
}

// readPage sends a GET with cookie, and returns the answer and its body as text.
func readPage(t *testing.T, c *http.Client, target string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// signInCookie signs key in over the listener at base, and returns the session cookie.
func signInCookie(t *testing.T, base, key string) *http.Cookie {
	t.Helper()
	resp, _ := postForm(t, noRedirect(), base+"/ui/signin", url.Values{"key": {key}},
		map[string]string{"Origin": base})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in: status %d, want 303", resp.StatusCode)
	}
	c := uiCookie(resp, "cairn_ui")
	if c == nil {
		t.Fatal("sign-in set no session cookie")
	}
	return c
}

// pageText is the text a fixture stores. The plain set is used by most tests; the markup set is
// used by the escaping test.
type pageText struct {
	display, title, goal, body, rule string
}

// plainText is the text of the ordinary fixture.
var plainText = pageText{
	display: "Acme API", title: "Ship the reads", goal: "Show the reads",
	body: "A learning body", rule: "A directive rule",
}

// markupText is the text of the escaping fixture: markup, a quote, and a javascript: URL.
var markupText = pageText{
	display: `<script>alert(1)</script> "quoted" javascript:alert(1)`,
	title:   `<script>alert(1)</script> "quoted" javascript:alert(1)`,
	goal:    `<script>alert(1)</script> "quoted" javascript:alert(1)`,
	body:    `<script>alert(1)</script> "quoted" javascript:alert(1)`,
	rule:    `<script>alert(1)</script> "quoted" javascript:alert(1)`,
}

// pageFixture is a store seeded through core: two projects, a closed task with a close record,
// an active task with a review in its journal, a proposed learning, and an active directive.
type pageFixture struct {
	closedID string
	activeID string
}

// seedPages writes the fixture into st through core, with the text tx.
func seedPages(t *testing.T, st *store.Store, tx pageText) pageFixture {
	t.Helper()
	svc := core.NewLocal(st)
	ctx := context.Background()
	agentCtx := func(project string) context.Context {
		return core.WithCaller(ctx, core.Caller{Project: project, Agent: "main", Role: core.RoleAgent})
	}
	opCtx := core.WithCaller(ctx, core.Caller{Agent: "ops", Role: core.RoleOperator})

	closed, err := svc.TaskStart(agentCtx("acme-api"), core.TaskStartRequest{
		Title: tx.title, Goal: tx.goal, Context: "The context of the brief", Constraints: "The constraints",
		Criteria: []string{"the reads answer"}, Steps: []core.StepInput{{Title: "build it"}},
	})
	if err != nil {
		t.Fatalf("task_start: %v", err)
	}
	if _, err := svc.TaskClose(agentCtx("acme-api"), core.TaskCloseRequest{
		ID: closed.ID, Outcome: core.TaskDone, Summary: "Done, with the close record.",
		Criteria: []core.CriterionOutcome{{Ord: 1, Result: core.CriterionMet, Evidence: "the run passed"}},
		Lessons:  []core.Lesson{{Kind: core.LessonReuse, Text: tx.body, Why: "it holds"}},
		Mistakes: []core.Mistake{{Agent: "main", Mistake: "a slip", Impact: core.ImpactHigh}},
	}); err != nil {
		t.Fatalf("task_close: %v", err)
	}

	active, err := svc.TaskStart(agentCtx("acme-api"), core.TaskStartRequest{Title: tx.title, Goal: tx.goal})
	if err != nil {
		t.Fatalf("task_start active: %v", err)
	}
	if _, err := svc.EventLog(agentCtx("acme-api"), core.EventLogRequest{
		TaskID: active.ID, Kind: core.EventReview, Text: "The plan was read.",
		Data: []byte(`{"verdict":"approved","critical":0,"warnings":1}`),
	}); err != nil {
		t.Fatalf("event_log review: %v", err)
	}

	if _, err := svc.TaskStart(agentCtx("acme-web"), core.TaskStartRequest{Title: "A web task", Goal: "g", Queue: true}); err != nil {
		t.Fatalf("task_start web: %v", err)
	}
	if _, err := svc.LearningAdd(agentCtx("acme-api"), core.LearningAddRequest{
		TaskID: closed.ID, Kind: core.LearningFinding, Title: tx.title, Body: tx.body,
		Target: "the target", Rationale: "the rationale", Example: "the example",
	}); err != nil {
		t.Fatalf("learning_add: %v", err)
	}
	if _, err := svc.DirectiveRatify(opCtx, core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: tx.title, Rule: tx.rule, Rationale: "the why",
		ReviewBy: "2028-01-01",
	}); err != nil {
		t.Fatalf("directive_ratify: %v", err)
	}
	display := tx.display
	if _, err := svc.ProjectSettingsSet(opCtx, core.ProjectSettingsRequest{
		Project: "acme-api", DisplayName: &display, Settings: core.DefaultSettings().AsInput(),
	}); err != nil {
		t.Fatalf("project_settings_set: %v", err)
	}
	return pageFixture{closedID: closed.ID, activeID: active.ID}
}

// pagePaths is every read page of the fixture, with a query where a page has one.
func pagePaths(f pageFixture) []string {
	return []string{
		"/ui/",
		"/ui/tasks",
		"/ui/tasks?status=done",
		"/ui/p/acme-api/tasks/" + f.closedID,
		"/ui/p/acme-api/tasks/" + f.activeID,
		"/ui/p/acme-api/learnings",
		"/ui/p/acme-api/learnings?status=all",
		"/ui/learnings",
		"/ui/learnings?status=all",
		"/ui/directives",
		"/ui/directives?state=retired",
		"/ui/projects",
		"/ui/projects/acme-api/settings",
	}
}

// pageSessions starts a listener over a store with an operator key and a viewer key, and returns
// the base URL with the two session cookies.
type pageSessions struct {
	base         string
	ops, viewers *http.Cookie
}

// startPages starts the handler over st and signs in one operator and one viewer.
func startPages(t *testing.T, st *store.Store) pageSessions {
	t.Helper()
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	return pageSessions{
		base:    base,
		ops:     signInCookie(t, base, addKey(t, st, "ops", auth.ScopeOperator)),
		viewers: signInCookie(t, base, addKey(t, st, "watch", auth.ScopeViewer)),
	}
}

// TestEveryReadPageRendersForOperatorAndViewer checks that each read page answers 200 with the
// signed-in key's name and role in its header, and with the security headers. A viewer's page
// has no form that posts, and neither does an operator's, since the actions arrive in a later
// step (DESIGN §5.5, Viewer mode).
func TestEveryReadPageRendersForOperatorAndViewer(t *testing.T) {
	st := openStore(t)
	f := seedPages(t, st, plainText)
	s := startPages(t, st)
	c := noRedirect()
	for _, path := range pagePaths(f) {
		for _, who := range []struct {
			name, role string
			cookie     *http.Cookie
		}{{"ops", "operator", s.ops}, {"watch", "viewer", s.viewers}} {
			resp, body := readPage(t, c, s.base+path, who.cookie)
			label := path + " as " + who.role
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: status %d, want 200", label, resp.StatusCode)
				continue
			}
			if !strings.Contains(body, who.name+" ("+who.role+")") {
				t.Errorf("%s: the header does not name the key and its role", label)
			}
			if strings.Contains(strings.ToLower(body), `method="post"`) {
				t.Errorf("%s: the page renders a form that posts", label)
			}
			checkPageHeaders(t, label, resp)
		}
	}
}

// TestMissingProjectIs404AndCreatesNoRow checks the project check of DESIGN §5.5 (Choosing a
// project): a project page for a name with no row is 404 for the operator and for the viewer, a
// path part that is not a valid name is 404 too, a task of another project is 404, and no
// request creates a project row.
func TestMissingProjectIs404AndCreatesNoRow(t *testing.T) {
	st := openStore(t)
	f := seedPages(t, st, plainText)
	s := startPages(t, st)
	c := noRedirect()
	paths := []string{
		"/ui/p/nope/learnings",
		"/ui/p/nope/tasks/" + f.closedID,
		"/ui/projects/nope/settings",
		"/ui/tasks?project=nope",
		"/ui/p/Not_Valid/learnings",
		"/ui/projects/Not_Valid/settings",
		"/ui/p/acme-web/tasks/" + f.closedID,
	}
	for _, path := range paths {
		for who, cookie := range map[string]*http.Cookie{"operator": s.ops, "viewer": s.viewers} {
			resp, _ := readPage(t, c, s.base+path, cookie)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s as %s: status %d, want 404", path, who, resp.StatusCode)
			}
			checkPageHeaders(t, path+" as "+who, resp)
		}
	}
	if _, err := store.GetProject(context.Background(), st, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a GET of a missing project created its row: %v", err)
	}
}

// TestStoredTextIsEscapedOnEveryPage checks escaping: text with markup, a quote and a javascript:
// URL, stored as a task brief, a learning, a directive and a project's display name, comes back as
// text on every page that shows it. No raw "<script" reaches a page, and no href carries the
// javascript: scheme. A project name cannot carry markup (core.ValidProjectName), so the display
// name stands for the project.
func TestStoredTextIsEscapedOnEveryPage(t *testing.T) {
	st := openStore(t)
	f := seedPages(t, st, markupText)
	s := startPages(t, st)
	c := noRedirect()
	const escaped = "&lt;script&gt;alert(1)&lt;/script&gt;"
	for _, path := range pagePaths(f) {
		if strings.Contains(path, "retired") {
			continue // the retired list holds no fixture row
		}
		for who, cookie := range map[string]*http.Cookie{"operator": s.ops, "viewer": s.viewers} {
			_, body := readPage(t, c, s.base+path, cookie)
			label := path + " as " + who
			if strings.Contains(body, "<script") {
				t.Errorf("%s: a raw <script reached the page", label)
			}
			if strings.Contains(body, `href="javascript:`) || strings.Contains(body, `src="javascript:`) {
				t.Errorf("%s: a javascript: URL reached an attribute", label)
			}
			if !strings.Contains(body, escaped) {
				t.Errorf("%s: the stored markup is not shown escaped", label)
			}
			if !strings.Contains(body, "&#34;quoted&#34;") {
				t.Errorf("%s: the stored quote is not shown escaped", label)
			}
		}
	}
}

// TestPagingAndInvalidCursor checks the paging of the lists: the first page holds 20 tasks and a
// next link that carries the cursor, the next link leads to the rest, and a cursor that does not
// decode, or a status that is not one, is 400 on every paged page.
func TestPagingAndInvalidCursor(t *testing.T) {
	st := openStore(t)
	f := seedPages(t, st, plainText)
	svc := core.NewLocal(st)
	web := core.WithCaller(context.Background(), core.Caller{Project: "acme-web", Agent: "main", Role: core.RoleAgent})
	for i := 0; i < 21; i++ {
		if _, err := svc.TaskStart(web, core.TaskStartRequest{
			Title: fmt.Sprintf("bulk %02d", i), Goal: "g", Queue: true,
		}); err != nil {
			t.Fatalf("task_start bulk: %v", err)
		}
	}
	s := startPages(t, st)
	c := noRedirect()

	// 24 tasks in all: the fixture's three and the 21 above. The first page holds 20 of them.
	first, body := readPage(t, c, s.base+"/ui/tasks", s.ops)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first page: status %d", first.StatusCode)
	}
	next := regexp.MustCompile(`href="(/ui/tasks\?cursor=[^"]+)"`).FindStringSubmatch(body)
	if next == nil {
		t.Fatal("the first page has no next link")
	}
	if got := strings.Count(body, "<tr><td>"); got != 20 {
		t.Errorf("first page rows = %d, want 20", got)
	}
	second, body2 := readPage(t, c, s.base+next[1], s.ops)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("next page: status %d", second.StatusCode)
	}
	if got := strings.Count(body2, "<tr><td>"); got != 4 {
		t.Errorf("next page rows = %d, want the last 4", got)
	}
	if strings.Contains(body2, "Next page") {
		t.Error("the last page has a next link")
	}

	for _, path := range []string{
		"/ui/tasks?cursor=%25%25",
		"/ui/learnings?cursor=zz",
		"/ui/p/acme-api/learnings?cursor=zz",
		"/ui/p/acme-api/tasks/" + f.closedID + "?cursor=zz",
		"/ui/directives?cursor=zz",
		"/ui/projects?cursor=zz",
		"/ui/tasks?status=pending",
		"/ui/directives?state=pending",
	} {
		resp, _ := readPage(t, c, s.base+path, s.ops)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", path, resp.StatusCode)
		}
		checkPageHeaders(t, path, resp)
	}
}

// TestDashboardFiguresLinkToTheirLists checks that each waiting figure links to the list that
// holds its items (DESIGN §5.5, Pages), and that a rate with no base is left out.
func TestDashboardFiguresLinkToTheirLists(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	_, body := readPage(t, noRedirect(), s.base+"/ui/", s.ops)
	for _, want := range []string{
		`href="/ui/tasks?status=active"`, `href="/ui/learnings"`,
		`href="/ui/tasks?status=blocked"`, `href="/ui/directives"`,
		"Proposed learnings: 1", "Running tasks: 1", "Expired directives: 0",
		"Criteria met: 1 of 1 (100%)", "done 1 (100%)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
}

// TestDirectivePageShowsTheReviewDay checks that the directives page shows the directive's code,
// its scope, its rule and its review day.
func TestDirectivePageShowsTheReviewDay(t *testing.T) {
	st := openStore(t)
	seedPages(t, st, plainText)
	s := startPages(t, st)
	_, body := readPage(t, noRedirect(), s.base+"/ui/directives", s.viewers)
	for _, want := range []string{"D-0001", "A directive rule", "review by 2028-01-01", "global"} {
		if !strings.Contains(body, want) {
			t.Errorf("directives page lacks %q", want)
		}
	}
}
