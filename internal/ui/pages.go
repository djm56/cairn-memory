package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// The pages of DESIGN §5.5: the reads of the web UI, each called in process through
// core.Service. A page checks its path parts first, and a project page checks its project
// before any other read (DESIGN §5.5, Choosing a project). No page here writes, and no
// page renders an action form: the forms arrive with the actions.

// The fixed messages of a page's error. None carries a stored value or core's internal text.
const (
	msgNotFound     = "Nothing is recorded at this address."
	msgOperatorOnly = "This page is for an operator or a viewer key."
)

// pageSize is how many rows a list page asks core for: its default, so the page and the
// call agree on the size of a page (CONTRACT §1.3).
const pageSize = 20

// taskIDPattern is the form of a task id, task-YYYYMMDD-NN (CONTRACT §1.3). A path part
// that does not match is a missing task, and is not sent to core.
var taskIDPattern = regexp.MustCompile(`^task-[0-9]{8}-[0-9]{2,}$`)

// readerCtx returns the context of a read the signed-in key makes: the key's name, its role,
// and the project when the read is of one project. The role follows the key's scope, which the
// session check read on this request. Core checks the role again on every read.
func readerCtx(r *http.Request, s signedIn, project string) context.Context {
	role := core.RoleViewer
	if s.Key.Scope == auth.ScopeOperator {
		role = core.RoleOperator
	}
	return core.WithCaller(r.Context(), core.Caller{Project: project, Agent: s.Key.Name, Role: role})
}

// navView is the header of a signed-in page: the key's name and role, and the project
// switcher, which lists every project name from project_list (CONTRACT §8.1).
type navView struct {
	Name, Role string
	Projects   []string
}

// navFor returns the header of a signed-in page. The switcher is read best effort: a failed
// read leaves the list empty and is logged, so the page itself still answers.
func (u *ui) navFor(ctx context.Context, s signedIn) *navView {
	nav := &navView{Name: s.Key.Name, Role: string(s.Key.Scope)}
	names, err := u.projectNames(ctx)
	if err != nil {
		u.log.Error("project switcher failed", "cause", u.redacted(err))
		return nav
	}
	nav.Projects = names
	return nav
}

// projectNames returns the name of every project, in name order, read a page of MaxLimit at a
// time. It creates no project row.
func (u *ui) projectNames(ctx context.Context) ([]string, error) {
	var names []string
	cursor := ""
	for {
		page, err := u.cfg.Service.ProjectList(ctx, core.ProjectListRequest{Limit: core.MaxLimit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, p := range page.Projects {
			names = append(names, p.Name)
		}
		if page.Next == "" {
			return names, nil
		}
		cursor = page.Next
	}
}

// project returns the project named name, or core's not_found when it has no row. It is the
// check each project page makes first, and it creates no row (DESIGN §5.5, Choosing a project).
func (u *ui) project(ctx context.Context, name string) (core.Project, error) {
	list, err := u.cfg.Service.ProjectList(ctx, core.ProjectListRequest{Project: name})
	if err != nil {
		return core.Project{}, err
	}
	if len(list.Projects) != 1 {
		return core.Project{}, core.NotFound("project %s not found", name)
	}
	return list.Projects[0], nil
}

// pageFail answers a signed-in page with an error page, with the header. It is for the
// failures the page itself finds: a bad path part, a missing project or task.
func (u *ui) pageFail(w http.ResponseWriter, ctx context.Context, s signedIn, status int, msg string) {
	u.render(w, status, "error", view{Title: http.StatusText(status), Message: msg, Nav: u.navFor(ctx, s)})
}

// pageError answers a failed core read on a signed-in page, with the status DESIGN §5.5 gives
// the kind: 404 for not_found, and 400 for invalid, which shows core's own message, since core
// writes that for the caller. Any other kind is internal: the page says so, and the cause goes
// to the log, redacted.
func (u *ui) pageError(w http.ResponseWriter, r *http.Request, ctx context.Context, s signedIn, err error) {
	var ce *core.Error
	if !errors.As(err, &ce) {
		u.internal(w, r, err)
		return
	}
	switch ce.Kind {
	case core.KindNotFound:
		u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
	case core.KindInvalid:
		u.pageFail(w, ctx, s, http.StatusBadRequest, ce.Message)
	case core.KindRefused:
		u.pageFail(w, ctx, s, http.StatusConflict, ce.Message)
	case core.KindOperatorOnly:
		u.pageFail(w, ctx, s, http.StatusForbidden, msgOperatorOnly)
	default:
		u.internal(w, r, err)
	}
}

// pageHref returns the link to the next page of the list at path: the same query, with the
// cursor set to next. It is "" when there is no next page.
func pageHref(r *http.Request, path, next string) string {
	if next == "" {
		return ""
	}
	q := r.URL.Query()
	q.Set("cursor", next)
	return path + "?" + q.Encode()
}

// option is one choice of a filter's select. Selected marks the one the page is showing.
type option struct {
	Value, Label string
	Selected     bool
}

// optionsOf returns one option per value, with current selected. The empty value's label is
// label, so a filter can say what "no filter" means.
func optionsOf(current, label string, values ...string) []option {
	out := make([]option, 0, len(values))
	for _, v := range values {
		l := v
		if v == "" {
			l = label
		}
		out = append(out, option{Value: v, Label: l, Selected: v == current})
	}
	return out
}

// stamp formats a time as the UI shows it: UTC, to the minute.
func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// stampPtr is stamp for an optional time, "" when there is none.
func stampPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return stamp(*t)
}

// percent returns part as a percentage of whole, rounded to the nearest whole number. It is ""
// when whole is zero, so a page leaves the rate out (DESIGN §5.5, Pages).
func percent(part, whole int) string {
	if whole == 0 {
		return ""
	}
	return strconv.Itoa((part*100+whole/2)/whole) + "%"
}

// onOff is the word a switch shows.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// taskHref is the page of one task, or "" when its project or id is not a valid path part.
func taskHref(project, id string) string {
	if !core.ValidProjectName(project) || !taskIDPattern.MatchString(id) {
		return ""
	}
	return "/ui/p/" + project + "/tasks/" + id
}

// projectLearningsHref is the learnings page of one project, or "" when the name is not valid.
func projectLearningsHref(project string) string {
	if !core.ValidProjectName(project) {
		return ""
	}
	return "/ui/p/" + project + "/learnings"
}

// projectSettingsHref is the settings page of one project, or "" when the name is not valid.
func projectSettingsHref(project string) string {
	if !core.ValidProjectName(project) {
		return ""
	}
	return "/ui/projects/" + project + "/settings"
}

// ---- the dashboard ----

// dashboardPage holds the figures of the dashboard. Each waiting figure carries the link to
// the list that holds its items (DESIGN §5.5, Pages).
type dashboardPage struct {
	RunningCount   int
	Running        []taskRow
	RunningHref    string
	Proposed       int
	ProposedHref   string
	Blocked        int
	BlockedHref    string
	Expired        int
	ExpiredHref    string
	Closed         int
	Done           int
	DonePct        string
	CriteriaMet    int
	CriteriaTotal  int
	CriteriaPct    string
	HighImpact     int
	Monthly        []monthRow
	Active         int
	OldestReview   string
	OldestRatified string
}

// monthRow is one UTC month of the dashboard's history.
type monthRow struct {
	Month      string
	Closed     int
	Done       int
	DonePct    string
	HighImpact int
	Ratified   int
}

// dashboard shows the figures of CONTRACT §8.1 from one snapshot.
func (u *ui) dashboard(w http.ResponseWriter, r *http.Request, s signedIn) {
	ctx := readerCtx(r, s, "")
	d, err := u.cfg.Service.Dashboard(ctx)
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	u.render(w, http.StatusOK, "dashboard", view{Title: "Dashboard", Nav: u.navFor(ctx, s), Page: dashboardOf(d)})
}

// dashboardOf converts the dashboard figures to the page.
func dashboardOf(d *core.Dashboard) dashboardPage {
	p := dashboardPage{
		RunningCount: d.Running.Count, RunningHref: "/ui/tasks?status=active",
		Proposed: d.Waiting.ProposedLearnings, ProposedHref: "/ui/learnings",
		Blocked: d.Waiting.BlockedTasks, BlockedHref: "/ui/tasks?status=blocked",
		Expired: d.Waiting.ExpiredDirectives, ExpiredHref: "/ui/directives",
		Closed: d.Quality.Closed, Done: d.Quality.Done, DonePct: percent(d.Quality.Done, d.Quality.Closed),
		CriteriaMet: d.Quality.CriteriaMet, CriteriaTotal: d.Quality.CriteriaTotal,
		CriteriaPct: percent(d.Quality.CriteriaMet, d.Quality.CriteriaTotal),
		HighImpact:  d.Quality.HighImpactMistakes,
		Active:      d.Directives.Active,
	}
	// The earliest review_by and the age of the directive ratified earliest are two
	// different directives, so each is shown with its own label (CONTRACT §8.1).
	p.OldestReview = d.Directives.OldestReviewBy
	if d.Directives.OldestAgeDays != nil {
		p.OldestRatified = fmt.Sprintf("%d days ago", *d.Directives.OldestAgeDays)
	}
	for _, t := range d.Running.Tasks {
		p.Running = append(p.Running, taskRowOf(t))
	}
	for _, m := range d.Monthly {
		p.Monthly = append(p.Monthly, monthRow{
			Month: m.Month, Closed: m.Closed, Done: m.Done, DonePct: percent(m.Done, m.Closed),
			HighImpact: m.HighImpactMistakes, Ratified: m.DirectivesRatified,
		})
	}
	return p
}

// ---- the tasks list ----

// taskRow is one line of a task list.
type taskRow struct {
	ID, Project, Title, Status, Priority, UpdatedAt string
	Href                                            string
}

// taskRowOf converts a task summary to a row.
func taskRowOf(t core.TaskSummary) taskRow {
	return taskRow{
		ID: t.ID, Project: t.Project, Title: t.Title, Status: string(t.Status),
		Priority: string(t.Priority), UpdatedAt: stamp(t.UpdatedAt),
		Href: taskHref(t.Project, t.ID),
	}
}

// tasksPage is the tasks list across projects, with its filters.
type tasksPage struct {
	Project       string
	StatusOptions []option
	Tasks         []taskRow
	Next          string
}

// tasks lists the tasks of every project, or of the project named in the query, filtered by
// status. The project filter is checked first, as a project page's is.
func (u *ui) tasks(w http.ResponseWriter, r *http.Request, s signedIn) {
	q := r.URL.Query()
	ctx := readerCtx(r, s, "")
	req := core.TaskBrowseRequest{Cursor: q.Get("cursor"), Limit: pageSize}
	project := q.Get("project")
	if project != "" {
		if !core.ValidProjectName(project) {
			u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
			return
		}
		if _, err := u.project(ctx, project); err != nil {
			u.pageError(w, r, ctx, s, err)
			return
		}
		req.Project = project
	}
	status := q.Get("status")
	if status != "" {
		req.Status = []core.TaskStatus{core.TaskStatus(status)}
	}
	page, err := u.cfg.Service.TaskBrowse(ctx, req)
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	out := tasksPage{
		Project: project,
		StatusOptions: optionsOf(status, "any status", "", "open", "active", "blocked",
			"done", "failed", "abandoned"),
		Next: pageHref(r, "/ui/tasks", page.Next),
	}
	for _, t := range page.Tasks {
		out.Tasks = append(out.Tasks, taskRowOf(t))
	}
	u.render(w, http.StatusOK, "tasks", view{Title: "Tasks", Nav: u.navFor(ctx, s), Page: out})
}

// ---- one task ----

// stepRow is one step of a task's plan.
type stepRow struct {
	Number   int
	Title    string
	Agent    string
	Status   string
	Attempts int
	Summary  string
}

// eventRow is one entry of a task's journal. A review entry carries its verdict and counts,
// or the words that none was recorded (CONTRACT §4, §9 item 39).
type eventRow struct {
	At, Kind, Agent, Step, Text string
	IsReview                    bool
	Verdict, Counts             string
}

// criterionRow is one acceptance criterion, with its result once the task is closed.
type criterionRow struct {
	Ord                         int
	Criterion, Result, Evidence string
}

// fileRow is a key file or a deliverable of a task.
type fileRow struct {
	Path, Note string
}

// lessonRow is one lesson of a close record.
type lessonRow struct {
	Kind, Text, Why, Correction string
}

// mistakeRow is one mistake of a close record.
type mistakeRow struct {
	Agent, Mistake, Impact, Correction, Prevention string
}

// taskPage is one task: the header, the brief, the steps, the journal and the close record.
type taskPage struct {
	Project, ID, Title, Status, Priority, Owner string
	NextAction, Blockers, OutcomeSummary        string
	Goal, Context, Constraints                  string
	OpenedAt, StartedAt, ClosedAt, UpdatedAt    string
	Criteria                                    []criterionRow
	KeyFiles                                    []fileRow
	Steps                                       []stepRow
	Events                                      []eventRow
	EventsNext                                  string
	HasClose                                    bool
	NoLessonsReason                             string
	Lessons                                     []lessonRow
	Mistakes                                    []mistakeRow
	Deliverables                                []fileRow
	LearningsHref                               string
}

// task shows one task of the project named in the path. The project and the id are checked
// first, then the project, then the task.
func (u *ui) task(w http.ResponseWriter, r *http.Request, s signedIn) {
	project, id := r.PathValue("project"), r.PathValue("task")
	ctx := readerCtx(r, s, "")
	if !core.ValidProjectName(project) || !taskIDPattern.MatchString(id) {
		u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
		return
	}
	pctx := readerCtx(r, s, project)
	if _, err := u.project(pctx, project); err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	detail, err := u.cfg.Service.TaskGet(pctx, core.TaskGetRequest{ID: id})
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	journal, err := u.cfg.Service.TaskGet(pctx, core.TaskGetRequest{
		ID: id, Section: core.SectionEvents, Limit: pageSize, Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	out := taskPageOf(detail, journal)
	out.EventsNext = pageHref(r, r.URL.Path, journal.Next)
	out.LearningsHref = projectLearningsHref(project)
	u.render(w, http.StatusOK, "task", view{Title: id, Nav: u.navFor(pctx, s), Page: out})
}

// taskPageOf converts a task's detail, and its journal page, to the page.
func taskPageOf(d, journal *core.TaskDetail) taskPage {
	t := d.Task
	p := taskPage{
		Project: t.Project, ID: t.ID, Title: t.Title, Status: string(t.Status),
		Priority: string(t.Priority), Owner: t.Owner, NextAction: t.NextAction,
		Blockers: t.Blockers, OutcomeSummary: t.OutcomeSummary,
		Goal: t.Goal, Context: t.Context, Constraints: t.Constraints,
		OpenedAt: stamp(t.OpenedAt), StartedAt: stampPtr(t.StartedAt),
		ClosedAt: stampPtr(t.ClosedAt), UpdatedAt: stamp(t.UpdatedAt),
		HasClose:        t.Status == core.TaskDone || t.Status == core.TaskFailed,
		NoLessonsReason: d.NoLessonsReason,
	}
	for _, c := range d.Criteria {
		p.Criteria = append(p.Criteria, criterionRow{
			Ord: c.Ord, Criterion: c.Criterion, Result: string(c.Result), Evidence: c.Evidence,
		})
	}
	for _, f := range d.KeyFiles {
		p.KeyFiles = append(p.KeyFiles, fileRow{Path: f.Path, Note: f.Note})
	}
	numbers := map[int64]int{}
	for _, s := range d.Steps {
		numbers[s.ID] = s.Number
		p.Steps = append(p.Steps, stepRow{
			Number: s.Number, Title: s.Title, Agent: s.Agent, Status: string(s.Status),
			Attempts: s.Attempts, Summary: s.Summary,
		})
	}
	for _, e := range journal.Events {
		row := eventRow{At: stamp(e.OccurredAt), Kind: string(e.Kind), Agent: e.Agent, Text: e.Text}
		if e.StepID != nil {
			if n, ok := numbers[*e.StepID]; ok {
				row.Step = "step " + strconv.Itoa(n)
			}
		}
		if e.Kind == core.EventReview {
			row.IsReview = true
			row.Verdict, row.Counts = reviewOf(e.Data)
		}
		p.Events = append(p.Events, row)
	}
	for _, l := range d.Lessons {
		p.Lessons = append(p.Lessons, lessonRow{Kind: string(l.Kind), Text: l.Text, Why: l.Why, Correction: l.Correction})
	}
	for _, m := range d.Mistakes {
		p.Mistakes = append(p.Mistakes, mistakeRow{
			Agent: m.Agent, Mistake: m.Mistake, Impact: string(m.Impact),
			Correction: m.Correction, Prevention: m.Prevention,
		})
	}
	for _, f := range d.Deliverables {
		p.Deliverables = append(p.Deliverables, fileRow{Path: f.Path, Note: f.Note})
	}
	return p
}

// reviewOf reads a review entry's data: its verdict and counts. An entry with no data was written
// before the verdict was recorded, and reads as no verdict.
func reviewOf(data json.RawMessage) (verdict, counts string) {
	if len(data) == 0 {
		return "no verdict recorded", ""
	}
	var v struct {
		Verdict  string `json:"verdict"`
		Critical int    `json:"critical"`
		Warnings int    `json:"warnings"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "no verdict recorded", ""
	}
	return v.Verdict, fmt.Sprintf("%d critical, %d warnings", v.Critical, v.Warnings)
}

// ---- learnings ----

// learningRow is one learning. Project and its links are set on the cross-project list; the
// task link is set when the learning has a task of a valid project and id.
type learningRow struct {
	ID                                            int64
	Project, ProjectHref                          string
	Kind, Title, Body, Target, Rationale, Example string
	Status, DecidedBy, DecidedAt, DecisionNote    string
	TaskID, TaskHref                              string
}

// learningsPage is a list of learnings, one project's or every project's, with its filters.
type learningsPage struct {
	Project       string
	AllProjects   bool
	StatusOptions []option
	KindOptions   []option
	Learnings     []learningRow
	Next          string
	FormAction    string
}

// projectLearnings lists one project's learnings. The project is checked first.
func (u *ui) projectLearnings(w http.ResponseWriter, r *http.Request, s signedIn) {
	project := r.PathValue("project")
	ctx := readerCtx(r, s, "")
	if !core.ValidProjectName(project) {
		u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
		return
	}
	pctx := readerCtx(r, s, project)
	if _, err := u.project(pctx, project); err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	u.learningsList(w, r, s, pctx, false, project)
}

// learnings lists the learnings of every project, through learning_list with all_projects.
func (u *ui) learnings(w http.ResponseWriter, r *http.Request, s signedIn) {
	u.learningsList(w, r, s, readerCtx(r, s, ""), true, "")
}

// learningsList answers a learnings page. The status defaults to proposed, and "all" lifts the
// status filter. The kind is one of the three kinds, or every kind.
func (u *ui) learningsList(w http.ResponseWriter, r *http.Request, s signedIn, ctx context.Context, all bool, project string) {
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = string(core.LearningProposed)
	}
	req := core.LearningListRequest{AllProjects: all, Cursor: q.Get("cursor"), Limit: pageSize}
	if status != "all" {
		req.Status = []core.LearningStatus{core.LearningStatus(status)}
	}
	kind := q.Get("kind")
	if kind != "" {
		req.Kind = []core.LearningKind{core.LearningKind(kind)}
	}
	list, err := u.cfg.Service.LearningList(ctx, req)
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	base := "/ui/learnings"
	if !all {
		base = projectLearningsHref(project)
	}
	out := learningsPage{
		Project: project, AllProjects: all,
		StatusOptions: optionsOf(status, "", "proposed", "accepted", "rejected", "applied",
			"retired", "superseded", "all"),
		KindOptions: optionsOf(kind, "any kind", "", "finding", "pattern", "agent-note"),
		Next:        pageHref(r, base, list.Next),
		FormAction:  base,
	}
	for _, l := range list.Learnings {
		out.Learnings = append(out.Learnings, learningRowOf(l))
	}
	u.render(w, http.StatusOK, "learnings", view{Title: "Learnings", Nav: u.navFor(ctx, s), Page: out})
}

// learningRowOf converts a learning to a row.
func learningRowOf(l core.Learning) learningRow {
	return learningRow{
		ID: l.ID, Project: l.Project, ProjectHref: projectLearningsHref(l.Project),
		Kind: string(l.Kind), Title: l.Title, Body: l.Body, Target: l.Target,
		Rationale: l.Rationale, Example: l.Example, Status: string(l.Status),
		DecidedBy: l.DecidedBy, DecidedAt: stampPtr(l.DecidedAt), DecisionNote: l.DecisionNote,
		TaskID: l.TaskID, TaskHref: taskHref(l.Project, l.TaskID),
	}
}

// ---- directives ----

// directiveRow is one directive. State is active, expired or retired: an active directive
// past its review_by is expired, and that is the list's flag, not a second list.
type directiveRow struct {
	Code, Scope, State, Title                   string
	AppliesWhen, Rule, Rationale, ReviewerCheck string
	Example, ReviewBy, RatifiedBy, RatifiedAt   string
	RetiredAt, SourceLearning                   string
}

// directivesPage is the directives across projects, of one state, with the head figures.
type directivesPage struct {
	State          string
	StateOptions   []option
	Active         int
	Expired        int
	OldestReview   string
	OldestRatified string
	Directives     []directiveRow
	Next           string
}

// directives lists the directives of the state in the query, active by default, with the head
// figures of the dashboard: the active count, the expired count and the oldest review_by.
func (u *ui) directives(w http.ResponseWriter, r *http.Request, s signedIn) {
	q := r.URL.Query()
	ctx := readerCtx(r, s, "")
	state := q.Get("state")
	if state == "" {
		state = string(core.DirectiveActive)
	}
	d, err := u.cfg.Service.Dashboard(ctx)
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	browse, err := u.cfg.Service.DirectiveBrowse(ctx, core.DirectiveBrowseRequest{
		State: core.DirectiveStatus(state), Cursor: q.Get("cursor"), Limit: pageSize,
	})
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	out := directivesPage{
		State:        state,
		StateOptions: optionsOf(state, "", "active", "retired"),
		Active:       d.Directives.Active,
		Expired:      d.Directives.Expired,
		OldestReview: d.Directives.OldestReviewBy,
		Next:         pageHref(r, "/ui/directives", browse.Next),
	}
	if d.Directives.OldestAgeDays != nil {
		out.OldestRatified = fmt.Sprintf("%d days ago", *d.Directives.OldestAgeDays)
	}
	for _, b := range browse.Directives {
		out.Directives = append(out.Directives, directiveRowOf(b))
	}
	u.render(w, http.StatusOK, "directives", view{Title: "Directives", Nav: u.navFor(ctx, s), Page: out})
}

// directiveRowOf converts a browsed directive to a row.
func directiveRowOf(b core.BrowsedDirective) directiveRow {
	d := b.Directive
	row := directiveRow{
		Code: d.Code, Scope: string(d.Scope), Title: d.Title,
		AppliesWhen: d.AppliesWhen, Rule: d.Rule, Rationale: d.Rationale,
		ReviewerCheck: d.ReviewerCheck, Example: d.Example,
		RatifiedBy: d.RatifiedBy, RatifiedAt: stamp(d.RatifiedAt), RetiredAt: stampPtr(d.RetiredAt),
	}
	switch d.Scope {
	case core.ScopeProject:
		row.Scope = "project " + d.Project
	case core.ScopeAgent:
		row.Scope = "agent " + d.Agent
	}
	if d.ReviewBy != nil {
		row.ReviewBy = d.ReviewBy.UTC().Format("2006-01-02")
	}
	if d.SourceLearning != nil {
		row.SourceLearning = strconv.FormatInt(*d.SourceLearning, 10)
	}
	switch {
	case d.Status == core.DirectiveRetired:
		row.State = "retired"
	case b.Expired:
		row.State = "expired"
	default:
		row.State = "active"
	}
	return row
}

// ---- projects ----

// projectRow is one line of the project list.
type projectRow struct {
	Name, Href, DisplayName, LastSeenAt string
	Parallel, CloseOn                   string
}

// projectsPage is the project list, in name order.
type projectsPage struct {
	Projects []projectRow
	Next     string
}

// projects lists the projects with their settings: parallel tasks, and how many of the five
// close-policy switches are on.
func (u *ui) projects(w http.ResponseWriter, r *http.Request, s signedIn) {
	ctx := readerCtx(r, s, "")
	list, err := u.cfg.Service.ProjectList(ctx, core.ProjectListRequest{
		Cursor: r.URL.Query().Get("cursor"), Limit: pageSize,
	})
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	out := projectsPage{Next: pageHref(r, "/ui/projects", list.Next)}
	for _, p := range list.Projects {
		out.Projects = append(out.Projects, projectRow{
			Name: p.Name, Href: projectSettingsHref(p.Name), DisplayName: p.DisplayName,
			LastSeenAt: stamp(p.LastSeenAt), Parallel: onOff(p.Settings.ParallelTasks),
			CloseOn: fmt.Sprintf("%d of 5", closeSwitchesOn(p.Settings.ClosePolicy)),
		})
	}
	u.render(w, http.StatusOK, "projects", view{Title: "Projects", Nav: u.navFor(ctx, s), Page: out})
}

// closeSwitchesOn counts the close-policy switches that are on.
func closeSwitchesOn(c core.ClosePolicy) int {
	n := 0
	for _, on := range []bool{c.RequireSummary, c.RequireCriteriaResults, c.RequireLessons,
		c.RequireMistakes, c.RequireNoActiveSteps} {
		if on {
			n++
		}
	}
	return n
}

// switchRow is one close-policy switch of a project's settings.
type switchRow struct {
	Label, State string
}

// projectPage is one project's settings, shown as text for an operator and a viewer alike.
type projectPage struct {
	Name, DisplayName, CreatedAt, LastSeenAt string
	Parallel                                 string
	Switches                                 []switchRow
	LearningsHref                            string
}

// projectSettings shows one project's settings. The project is checked first.
func (u *ui) projectSettings(w http.ResponseWriter, r *http.Request, s signedIn) {
	name := r.PathValue("project")
	ctx := readerCtx(r, s, "")
	if !core.ValidProjectName(name) {
		u.pageFail(w, ctx, s, http.StatusNotFound, msgNotFound)
		return
	}
	p, err := u.project(ctx, name)
	if err != nil {
		u.pageError(w, r, ctx, s, err)
		return
	}
	c := p.Settings.ClosePolicy
	out := projectPage{
		Name: p.Name, DisplayName: p.DisplayName,
		CreatedAt: stamp(p.CreatedAt), LastSeenAt: stamp(p.LastSeenAt),
		Parallel: onOff(p.Settings.ParallelTasks),
		Switches: []switchRow{
			{"Summary", onOff(c.RequireSummary)},
			{"Criteria results", onOff(c.RequireCriteriaResults)},
			{"Lessons, or a reason for none", onOff(c.RequireLessons)},
			{"Mistakes", onOff(c.RequireMistakes)},
			{"No active steps", onOff(c.RequireNoActiveSteps)},
		},
		LearningsHref: projectLearningsHref(p.Name),
	}
	u.render(w, http.StatusOK, "project", view{Title: p.Name, Nav: u.navFor(ctx, s), Page: out})
}
