package ui

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// The action routes of DESIGN §5.5: the five POSTs that change state, and the ratify form. A
// POST is wrapped in form, which checks, in order, the session, the origin, the role, the body and
// the CSRF value, before the action here runs. An action calls core with the signed-in key's name as
// its operator, since the browser supplies no name (DESIGN §5.5, Operator name). A success redirects
// to the page that shows the result. A failure re-renders its form with the message and the typed
// values, at the status DESIGN §5.5 gives the kind (Errors).

// The fixed messages of the actions. None carries a stored value, and none repeats a typed one.
const (
	msgNoteNeeded    = "A decision needs a note: say why the learning is decided this way."
	msgSupersededBy  = "superseded_by is the id of the learning that replaces this one: a whole number."
	msgLearningID    = "from_learning is the id of an accepted learning: a whole number."
	msgConfirmNeeded = "Retiring a directive is final. Tick the box to confirm. To keep the rule, reaffirm the directive instead."
	msgSwitchNeeded  = "Each switch is on or off."
)

// failed is what a page shows when an action's form fails: the status, the message, the row the
// form sat on (a learning id or a directive code), and the typed values by field name. The zero
// value is no failure. A page re-rendered with a failure keeps the operator's typed values
// (DESIGN §5.5).
type failed struct {
	Status  int
	Message string
	Target  string
	Draft   map[string]string
}

// active reports whether f is a failure, so the page re-renders with its status and message.
func (f failed) active() bool {
	return f.Status != 0
}

// ratifyKeys are the fields of the ratify form, in the order the form shows them (CONTRACT §8).
var ratifyKeys = []string{
	"scope", "project", "agent", "title", "rule", "applies_when", "rationale",
	"reviewer_check", "example", "review_by", "from_learning",
}

// ratifyPage is the ratify form. FromLearning is the learning the directive comes from, when the
// form was reached from one (W7 in DESIGN §5.5, Ratify).
type ratifyPage struct {
	FromLearning                                string
	ScopeOptions                                []option
	Project, Agent, Title, Rule, AppliesWhen    string
	Rationale, ReviewerCheck, Example, ReviewBy string
}

// boolPtr returns a pointer to v, for the optional switches of a settings call.
func boolPtr(v bool) *bool {
	return &v
}

// parseID reads a row id from a path or a field. It accepts a whole number above zero.
func parseID(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil && v > 0
}

// operatorPage wraps a GET page that only an operator may see: the ratify form. A viewer gets
// 403 read_only, answered before the page reads anything (DESIGN §5.5, Viewer mode).
func (u *ui) operatorPage(h func(http.ResponseWriter, *http.Request, signedIn)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := u.session(w, r)
		if !ok {
			return
		}
		if s.Key.Scope != auth.ScopeOperator {
			u.pageFail(w, readerCtx(r, s, ""), s, http.StatusForbidden, msgReadOnly)
			return
		}
		h(w, r, s)
	}
}

// operatorCtx returns the context of an operator call from the signed-in key. The name is the
// key's name, from api_keys, and never a value the browser sent (DESIGN §5.5). The role follows
// the key's scope, as readerCtx gives it, so core refuses a viewer's call too, if the form's role
// check were ever bypassed.
func operatorCtx(r *http.Request, s signedIn) context.Context {
	role := core.RoleViewer
	if s.Key.Scope == auth.ScopeOperator {
		role = core.RoleOperator
	}
	return core.WithCaller(r.Context(), core.Caller{Agent: s.Key.Name, Role: role})
}

// failedAction answers a core error of an action. An invalid or refused call goes to again, which
// re-renders the form, at 400 or 409 (DESIGN §5.5, Errors). Any other kind is answered by pageError:
// not_found is 404, operator_only is 403, and anything else is internal.
func (u *ui) failedAction(w http.ResponseWriter, r *http.Request, s signedIn, err error, again func(int, string)) {
	switch core.KindOf(err) {
	case core.KindInvalid:
		again(http.StatusBadRequest, coreMessage(err))
	case core.KindRefused:
		again(http.StatusConflict, coreMessage(err))
	default:
		u.pageError(w, r, readerCtx(r, s, ""), s, err)
	}
}

// coreMessage returns the message of a core error, which core writes for the caller to read. It
// is "" for any other error.
func coreMessage(err error) string {
	var ce *core.Error
	if errors.As(err, &ce) {
		return ce.Message
	}
	return ""
}

// ratifyForm shows the ratify form to an operator. from_learning, when given, fills the hidden
// field and names the learning on the form. The form does not check that the learning is accepted:
// core refuses a ratify from one that is not (CONTRACT §8), and the refusal re-renders the form.
func (u *ui) ratifyForm(w http.ResponseWriter, r *http.Request, s signedIn) {
	fl := r.URL.Query().Get("from_learning")
	if fl != "" {
		if _, ok := parseID(fl); !ok {
			u.renderRatify(w, r, s, failed{Status: http.StatusBadRequest, Message: msgLearningID})
			return
		}
	}
	u.renderRatify(w, r, s, failed{Draft: map[string]string{"from_learning": fl}})
}

// renderRatify answers the ratify form. Its values are the draft's: the typed ones after a failed
// POST, or the from_learning of the query.
func (u *ui) renderRatify(w http.ResponseWriter, r *http.Request, s signedIn, f failed) {
	d := f.Draft
	scope := d["scope"]
	if scope == "" {
		scope = string(core.ScopeGlobal)
	}
	out := ratifyPage{
		FromLearning: d["from_learning"],
		ScopeOptions: optionsOf(scope, "", "global", "project", "agent"),
		Project:      d["project"], Agent: d["agent"], Title: d["title"], Rule: d["rule"],
		AppliesWhen: d["applies_when"], Rationale: d["rationale"], ReviewerCheck: d["reviewer_check"],
		Example: d["example"], ReviewBy: d["review_by"],
	}
	code := http.StatusOK
	if f.active() {
		code = f.Status
	}
	u.render(w, code, "ratify", view{
		Title: "Ratify a directive", Nav: u.navFor(readerCtx(r, s, ""), s), Notice: f.Message, Page: out,
	})
}

// directiveRatify ratifies a directive from the form (CONTRACT §8). A project directive names a
// project that must already have a row: a name with none is refused here, so a misspelt name
// creates no project (DESIGN §5.5). Success redirects to the directives page.
func (u *ui) directiveRatify(w http.ResponseWriter, r *http.Request, s signedIn) {
	f := r.PostForm
	draft := map[string]string{}
	for _, k := range ratifyKeys {
		draft[k] = f.Get(k)
	}
	again := func(status int, msg string) {
		u.renderRatify(w, r, s, failed{Status: status, Message: msg, Draft: draft})
	}
	req := core.DirectiveRatifyRequest{
		Scope: core.DirectiveScope(f.Get("scope")), Project: f.Get("project"), Agent: f.Get("agent"),
		Title: f.Get("title"), Rule: f.Get("rule"), AppliesWhen: f.Get("applies_when"),
		Rationale: f.Get("rationale"), ReviewerCheck: f.Get("reviewer_check"), Example: f.Get("example"),
		ReviewBy: f.Get("review_by"),
	}
	if fl := f.Get("from_learning"); fl != "" {
		id, ok := parseID(fl)
		if !ok {
			again(http.StatusBadRequest, msgLearningID)
			return
		}
		req.FromLearning = &id
	}
	ctx := readerCtx(r, s, "")
	if req.Scope == core.ScopeProject && core.ValidProjectName(req.Project) {
		if _, err := u.project(ctx, req.Project); err != nil {
			if core.KindOf(err) != core.KindNotFound {
				u.pageError(w, r, ctx, s, err)
				return
			}
			again(http.StatusBadRequest, "There is no project named "+req.Project+". A project is created by its first agent call: check the name.")
			return
		}
	}
	if _, err := u.cfg.Service.DirectiveRatify(operatorCtx(r, s), req); err != nil {
		u.failedAction(w, r, s, err, again)
		return
	}
	http.Redirect(w, r, pathDirectives, http.StatusSeeOther)
}

// learningDecide records the operator's decision on a learning (CONTRACT §8). The page requires a
// note for all three decisions (DESIGN §5.5, Pages). Success redirects to the learning's project
// page. A failure re-renders the page the form sat on, which is the project's when the form was on
// a project page, and the all-projects list otherwise.
func (u *ui) learningDecide(w http.ResponseWriter, r *http.Request, s signedIn) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		u.pageFail(w, readerCtx(r, s, ""), s, http.StatusNotFound, msgNotFound)
		return
	}
	f := r.PostForm
	project := f.Get("project")
	if project != "" && !core.ValidProjectName(project) {
		project = ""
	}
	draft := map[string]string{"status": f.Get("status"), "superseded_by": f.Get("superseded_by"), "note": f.Get("note")}
	again := func(status int, msg string) {
		u.renderLearnings(w, r, s, project, failed{
			Status: status, Message: msg, Target: strconv.FormatInt(id, 10), Draft: draft,
		})
	}
	if strings.TrimSpace(f.Get("note")) == "" {
		again(http.StatusBadRequest, msgNoteNeeded)
		return
	}
	req := core.LearningDecideRequest{ID: id, Status: core.LearningStatus(f.Get("status")), Note: f.Get("note")}
	if sb := strings.TrimSpace(f.Get("superseded_by")); sb != "" {
		v, ok := parseID(sb)
		if !ok {
			again(http.StatusBadRequest, msgSupersededBy)
			return
		}
		req.SupersededBy = &v
	}
	l, err := u.cfg.Service.LearningDecide(operatorCtx(r, s), req)
	if err != nil {
		u.failedAction(w, r, s, err, again)
		return
	}
	dest := projectLearningsHref(l.Project)
	if dest == "" {
		dest = pathLearnings
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// directiveRetire retires a directive (CONTRACT §8). The form needs the confirm box, since
// retiring is final (DESIGN §5.5, Pages): without it the directives page is re-rendered with the
// message and nothing is retired. Success redirects to the directives page.
func (u *ui) directiveRetire(w http.ResponseWriter, r *http.Request, s signedIn) {
	code := r.PathValue("code")
	if !core.ValidDirectiveCode(code) {
		u.pageFail(w, readerCtx(r, s, ""), s, http.StatusNotFound, msgNotFound)
		return
	}
	again := func(status int, msg string) {
		u.renderDirectives(w, r, s, failed{Status: status, Message: msg, Target: code})
	}
	if r.PostForm.Get("confirm") != "yes" {
		again(http.StatusBadRequest, msgConfirmNeeded)
		return
	}
	if _, err := u.cfg.Service.DirectiveRetire(operatorCtx(r, s), core.DirectiveRetireRequest{Code: code}); err != nil {
		u.failedAction(w, r, s, err, again)
		return
	}
	http.Redirect(w, r, pathDirectives, http.StatusSeeOther)
}

// directiveReaffirm moves a directive's review_by (CONTRACT §8, D26). An empty review_by means 90
// days from now. Success redirects to the directives page. A failure re-renders the page with the
// typed day in the directive's own form.
func (u *ui) directiveReaffirm(w http.ResponseWriter, r *http.Request, s signedIn) {
	code := r.PathValue("code")
	if !core.ValidDirectiveCode(code) {
		u.pageFail(w, readerCtx(r, s, ""), s, http.StatusNotFound, msgNotFound)
		return
	}
	reviewBy := r.PostForm.Get("review_by")
	again := func(status int, msg string) {
		u.renderDirectives(w, r, s, failed{
			Status: status, Message: msg, Target: code, Draft: map[string]string{"review_by": reviewBy},
		})
	}
	req := core.DirectiveReaffirmRequest{Code: code, ReviewBy: reviewBy}
	if _, err := u.cfg.Service.DirectiveReaffirm(operatorCtx(r, s), req); err != nil {
		u.failedAction(w, r, s, err, again)
		return
	}
	http.Redirect(w, r, pathDirectives, http.StatusSeeOther)
}

// projectSettingsSet replaces a project's settings and display name (CONTRACT §8). The settings
// call creates a row for a name that has none, so the page checks the project first: a name with no
// row is 404, and nothing is written (DESIGN §5.5). Every switch must be sent on or off, since the
// call replaces the whole object. Success redirects to the project's settings page.
func (u *ui) projectSettingsSet(w http.ResponseWriter, r *http.Request, s signedIn) {
	name := r.PathValue("project")
	ctx := readerCtx(r, s, "")
	if !core.ValidProjectName(name) {
		u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
		return
	}
	if _, err := u.project(ctx, name); err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	f := r.PostForm
	draft := map[string]string{"display_name": f.Get("display_name")}
	for _, k := range settingsKeys {
		draft[k] = f.Get(k)
	}
	again := func(status int, msg string) {
		u.renderSettings(w, r, s, name, failed{Status: status, Message: msg, Draft: draft})
	}
	vals := map[string]bool{}
	for _, k := range settingsKeys {
		switch f.Get(k) {
		case "on":
			vals[k] = true
		case "off":
			vals[k] = false
		default:
			again(http.StatusBadRequest, msgSwitchNeeded)
			return
		}
	}
	display := f.Get("display_name")
	in := &core.SettingsInput{
		ParallelTasks: boolPtr(vals["parallel_tasks"]),
		ClosePolicy: &core.ClosePolicyInput{
			RequireSummary:         boolPtr(vals["require_summary"]),
			RequireCriteriaResults: boolPtr(vals["require_criteria_results"]),
			RequireLessons:         boolPtr(vals["require_lessons"]),
			RequireMistakes:        boolPtr(vals["require_mistakes"]),
			RequireNoActiveSteps:   boolPtr(vals["require_no_active_steps"]),
		},
	}
	req := core.ProjectSettingsRequest{Project: name, DisplayName: &display, Settings: in}
	if _, err := u.cfg.Service.ProjectSettingsSet(operatorCtx(r, s), req); err != nil {
		u.failedAction(w, r, s, err, again)
		return
	}
	http.Redirect(w, r, projectSettingsHref(name), http.StatusSeeOther)
}
