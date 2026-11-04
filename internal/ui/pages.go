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

// msgNotFound is the fixed message of a page's 404. It carries no stored value or core's internal
// text.
const msgNotFound = "Nothing is recorded at this address."

// pageSize is how many rows a list page asks core for: its default, so the page and the
// call agree on the size of a page (CONTRACT §1.3).
const pageSize = 20

// switcherSize is how many project names the header's switcher lists. The switcher is read on
// every signed-in page, so its read is bounded to this many; the full list is the projects page,
// which the switcher links to (#156).
const switcherSize = 10

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
// switcher, which lists the first switcherSize project names from project_list (CONTRACT §8.1).
type navView struct {
	Name, Role string
	Projects   []string
	// More is set when the switcher is short of the projects there are. The header then links to
	// the full projects page.
	More bool
	// Operator is set for an operator's key. Only then does a page render an action form.
	Operator bool
	// CSRF is the value each action form carries in its hidden csrf field (DESIGN §5.5).
	CSRF string
}

// navFor returns the header of a signed-in page. The switcher is read best effort: a failed
// read leaves the list empty and is logged, so the page itself still answers.
func (u *ui) navFor(ctx context.Context, s signedIn) *navView {
	nav := &navView{
		Name: s.Key.Name, Role: string(s.Key.Scope),
		Operator: s.Key.Scope == auth.ScopeOperator, CSRF: csrfToken(s.token),
	}
	names, more, err := u.switcher(ctx)
	if err != nil {
		u.log.Error("project switcher failed", "cause", u.redacted(err))
		return nav
	}
	nav.Projects, nav.More = names, more
	return nav
}

// switcher returns the names of the first switcherSize projects, in name order, and whether
// more projects follow. It is one bounded read, so a page costs the same however many projects
// there are. It creates no project row.
func (u *ui) switcher(ctx context.Context) ([]string, bool, error) {
	page, err := u.cfg.Service.ProjectList(ctx, core.ProjectListRequest{Limit: switcherSize})
	if err != nil {
		return nil, false, err
	}
	names := make([]string, 0, len(page.Projects))
	for _, p := range page.Projects {
		names = append(names, p.Name)
	}
	return names, page.Next != "", nil
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

// coreError returns the core error that err wraps, or nil when it wraps none. Every answer of the
// UI that shows a core message reads it through here, so the unwrap is written once (#162).
func coreError(err error) *core.Error {
	var ce *core.Error
	if errors.As(err, &ce) {
		return ce
	}
	return nil
}

// pageFail answers a signed-in page with an error page, with the header. It is for the
// failures the page itself finds: a bad path part, a missing project or task.
func (u *ui) pageFail(ctx context.Context, w http.ResponseWriter, s signedIn, status int, msg string) {
	u.render(w, status, "error", view{Title: http.StatusText(status), Message: msg, Nav: u.navFor(ctx, s)})
}

// pageError answers a failed core read on a signed-in page, with the status DESIGN §5.5 gives
// the kind: 404 for not_found, and 400 for invalid, which shows core's own message, since core
// writes that for the caller. A refused kind is 409, with core's message too. Any other kind,
// operator_only included, is internal: the page says so, and the cause goes to the log, redacted.
// operator_only has no page of its own. Both the form and the operator page check the key's scope
// before core is called, and reads are not operator calls, so core never refuses a call of these
// pages for its role.
func (u *ui) pageError(ctx context.Context, w http.ResponseWriter, r *http.Request, s signedIn, err error) {
	ce := coreError(err)
	if ce == nil {
		u.internal(w, r, err)
		return
	}
	switch ce.Kind {
	case core.KindNotFound:
		u.pageFail(ctx, w, s, http.StatusNotFound, msgNotFound)
	case core.KindInvalid:
		u.pageFail(ctx, w, s, http.StatusBadRequest, ce.Message)
	case core.KindRefused:
		u.pageFail(ctx, w, s, http.StatusConflict, ce.Message)
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
		u.pageError(ctx, w, r, s, err)
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
			u.pageFail(ctx, w, s, http.StatusNotFound, msgNotFound)
			return
		}
		if _, err := u.project(ctx, project); err != nil {
			u.pageError(ctx, w, r, s, err)
			return
		}
		req.Project = project
	}
	// status repeats (DESIGN §5.5): every value is a filter. The select shows the first one,
	// since it is the only one the form can set; the others come back only from a typed URL.
	var statuses []core.TaskStatus
	for _, v := range q["status"] {
		if v != "" {
			statuses = append(statuses, core.TaskStatus(v))
		}
	}
	req.Status = statuses
	status := ""
	if len(statuses) > 0 {
		status = string(statuses[0])
	}
	page, err := u.cfg.Service.TaskBrowse(ctx, req)
	if err != nil {
		u.pageError(ctx, w, r, s, err)
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
		u.pageFail(ctx, w, s, http.StatusNotFound, msgNotFound)
		return
	}
	pctx := readerCtx(r, s, project)
	if _, err := u.project(pctx, project); err != nil {
		u.pageError(ctx, w, r, s, err)
		return
	}
	detail, err := u.cfg.Service.TaskGet(pctx, core.TaskGetRequest{ID: id})
	if err != nil {
		u.pageError(ctx, w, r, s, err)
		return
	}
	journal, err := u.cfg.Service.TaskGet(pctx, core.TaskGetRequest{
		ID: id, Section: core.SectionEvents, Limit: pageSize, Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		u.pageError(ctx, w, r, s, err)
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
// task link is set when the learning has a task of a valid project and id. Ratifiable is set
// for an operator on an accepted learning, and Decide is set for an operator on a proposed one.
type learningRow struct {
	ID                                            int64
	Project, ProjectHref                          string
	Kind, Title, Body, Target, Rationale, Example string
	Status, DecidedBy, DecidedAt, DecisionNote    string
	TaskID, TaskHref                              string
	Ratifiable                                    bool
	Decide                                        *decideForm
}

// decideForm is the decide form of one proposed learning. Its values are the defaults, or the
// ones the operator typed when the decision failed (DESIGN §5.5). Project is the page the form
// sits on, which the decision returns to when it fails; it is "" on the all-projects list.
type decideForm struct {
	ID                         int64
	Project                    string
	Status, SupersededBy, Note string
}

// learningsPage is a list of learnings, one project's or every project's, with its filters.
type learningsPage struct {
	Project       string
	AllProjects   bool
	Operator      bool
	StatusOptions []option
	KindOptions   []option
	Learnings     []learningRow
	Next          string
	FormAction    string
}

// projectLearnings lists one project's learnings. The project is checked first.
func (u *ui) projectLearnings(w http.ResponseWriter, r *http.Request, s signedIn) {
	project := r.PathValue("project")
	if !core.ValidProjectName(project) {
		u.pageFail(readerCtx(r, s, ""), w, s, http.StatusNotFound, msgNotFound)
		return
	}
	u.renderLearnings(w, r, s, project, failed{})
}

// learnings lists the learnings of every project, through learning_list with all_projects.
func (u *ui) learnings(w http.ResponseWriter, r *http.Request, s signedIn) {
	u.renderLearnings(w, r, s, "", failed{})
}

// renderLearnings answers the learnings page of project, or of every project when project is "".
// A project's page checks the project before any other read, as the page does (DESIGN §5.5). f
// is the failed form to show, if any.
func (u *ui) renderLearnings(w http.ResponseWriter, r *http.Request, s signedIn, project string, f failed) {
	ctx := readerCtx(r, s, project)
	if project != "" {
		if _, err := u.project(ctx, project); err != nil {
			u.pageError(readerCtx(r, s, ""), w, r, s, err)
			return
		}
	}
	u.learningsList(ctx, w, r, s, project == "", project, f)
}

// learningsList answers a learnings page. The status defaults to proposed, and "all" lifts the
// status filter. The kind is one of the three kinds, or every kind.
func (u *ui) learningsList(ctx context.Context, w http.ResponseWriter, r *http.Request, s signedIn, all bool, project string, f failed) {
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
		u.pageError(readerCtx(r, s, ""), w, r, s, err)
		return
	}
	base := "/ui/learnings"
	if !all {
		base = projectLearningsHref(project)
	}
	operator := s.Key.Scope == auth.ScopeOperator
	out := learningsPage{
		Project: project, AllProjects: all, Operator: operator,
		StatusOptions: optionsOf(status, "", "proposed", "accepted", "rejected", "applied",
			"retired", "superseded", "all"),
		KindOptions: optionsOf(kind, "any kind", "", "finding", "pattern", "agent-note"),
		Next:        pageHref(r, base, list.Next),
		FormAction:  base,
	}
	for _, l := range list.Learnings {
		row := learningRowOf(l)
		if operator {
			row.Ratifiable = l.Status == core.LearningAccepted
			if l.Status == core.LearningProposed {
				row.Decide = decideFormOf(l.ID, project, f)
			}
		}
		out.Learnings = append(out.Learnings, row)
	}
	code := http.StatusOK
	if f.active() {
		code = f.Status
	}
	u.render(w, code, "learnings", view{Title: "Learnings", Nav: u.navFor(ctx, s), Notice: f.Message, Page: out})
}

// decideFormOf returns the decide form of learning id. The form shows the typed values when the
// failed decision was for this learning, and the defaults otherwise.
func decideFormOf(id int64, project string, f failed) *decideForm {
	d := &decideForm{ID: id, Project: project, Status: "accepted"}
	if f.active() && f.Target == strconv.FormatInt(id, 10) {
		if v := f.Draft["status"]; v != "" {
			d.Status = v
		}
		d.SupersededBy = f.Draft["superseded_by"]
		d.Note = f.Draft["note"]
	}
	return d
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
	// Actions is set for an operator on a directive that is not retired. ReaffirmBy is the day
	// its reaffirm form offers.
	Actions    bool
	ReaffirmBy string
}

// directivesPage is the directives across projects, of one state, with the head figures.
// Operator is set for an operator's key: an active directive then carries the reaffirm and
// retire forms, and the page links to the ratify form.
type directivesPage struct {
	State          string
	StateOptions   []option
	Operator       bool
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
	u.renderDirectives(w, r, s, failed{})
}

// renderDirectives answers the directives page. f is the failed form to show, if any; the page
// shows the state in the query, which a re-rendered page of a failed POST does not carry, so it
// is the active list.
func (u *ui) renderDirectives(w http.ResponseWriter, r *http.Request, s signedIn, f failed) {
	q := r.URL.Query()
	ctx := readerCtx(r, s, "")
	state := q.Get("state")
	if state == "" {
		state = string(core.DirectiveActive)
	}
	d, err := u.cfg.Service.Dashboard(ctx)
	if err != nil {
		u.pageError(ctx, w, r, s, err)
		return
	}
	browse, err := u.cfg.Service.DirectiveBrowse(ctx, core.DirectiveBrowseRequest{
		State: core.DirectiveStatus(state), Cursor: q.Get("cursor"), Limit: pageSize,
	})
	if err != nil {
		u.pageError(ctx, w, r, s, err)
		return
	}
	operator := s.Key.Scope == auth.ScopeOperator
	out := directivesPage{
		State:        state,
		StateOptions: optionsOf(state, "", "active", "retired"),
		Operator:     operator,
		Active:       d.Directives.Active,
		Expired:      d.Directives.Expired,
		OldestReview: d.Directives.OldestReviewBy,
		Next:         pageHref(r, "/ui/directives", browse.Next),
	}
	if d.Directives.OldestAgeDays != nil {
		out.OldestRatified = fmt.Sprintf("%d days ago", *d.Directives.OldestAgeDays)
	}
	for _, b := range browse.Directives {
		row := directiveRowOf(b)
		if operator && row.State != "retired" && core.ValidDirectiveCode(row.Code) {
			row.Actions = true
			row.ReaffirmBy = defaultReviewBy()
			if f.active() && f.Target == row.Code && f.Draft["review_by"] != "" {
				row.ReaffirmBy = f.Draft["review_by"]
			}
		}
		out.Directives = append(out.Directives, row)
	}
	code := http.StatusOK
	if f.active() {
		code = f.Status
	}
	u.render(w, code, "directives", view{Title: "Directives", Nav: u.navFor(ctx, s), Notice: f.Message, Page: out})
}

// defaultReviewBy is the day a reaffirm form offers: 90 days from today, in UTC (DESIGN §5.5).
func defaultReviewBy() string {
	return time.Now().UTC().AddDate(0, 0, core.DirectiveReviewDays).Format("2006-01-02")
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
		u.pageError(ctx, w, r, s, err)
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

// switchRow is one close-policy switch of a project's settings. State is the stored value, as
// text. On is the value the form shows: the stored one, or the one the operator typed when a save
// failed. Key is the form field.
type switchRow struct {
	Key, Label, State string
	On                bool
}

// projectPage is one project's settings. An operator gets the form, with the display name and
// the switches as selects; a viewer gets the same values as text.
type projectPage struct {
	Name, DisplayName, CreatedAt, LastSeenAt string
	Parallel                                 string
	ParallelOn                               bool
	Switches                                 []switchRow
	LearningsHref                            string
	Operator                                 bool
}

// settingsKeys are the form fields of the settings form, in the order the page shows them. The
// first is parallel_tasks; the rest are the five close-policy switches of CONTRACT §8.
var settingsKeys = []string{
	"parallel_tasks", "require_summary", "require_criteria_results", "require_lessons",
	"require_mistakes", "require_no_active_steps",
}

// projectSettings shows one project's settings. The project is checked first.
func (u *ui) projectSettings(w http.ResponseWriter, r *http.Request, s signedIn) {
	u.renderSettings(w, r, s, r.PathValue("project"), failed{})
}

// renderSettings answers the settings page of name. f is the failed save to show, if any: its
// draft holds what the operator typed, and the form shows it in place of the stored values.
func (u *ui) renderSettings(w http.ResponseWriter, r *http.Request, s signedIn, name string, f failed) {
	ctx := readerCtx(r, s, "")
	if !core.ValidProjectName(name) {
		u.pageFail(ctx, w, s, http.StatusNotFound, msgNotFound)
		return
	}
	p, err := u.project(ctx, name)
	if err != nil {
		u.pageError(ctx, w, r, s, err)
		return
	}
	c := p.Settings.ClosePolicy
	// shown returns the form's value for key: the typed one after a failed save, else the stored one.
	shown := func(key string, stored bool) bool {
		if f.active() {
			if v, ok := f.Draft[key]; ok {
				return v == "on"
			}
		}
		return stored
	}
	display := p.DisplayName
	if v, ok := f.Draft["display_name"]; ok && f.active() {
		display = v
	}
	out := projectPage{
		Name: p.Name, DisplayName: display,
		CreatedAt: stamp(p.CreatedAt), LastSeenAt: stamp(p.LastSeenAt),
		Parallel:   onOff(p.Settings.ParallelTasks),
		ParallelOn: shown("parallel_tasks", p.Settings.ParallelTasks),
		Switches: []switchRow{
			{"require_summary", "Summary", onOff(c.RequireSummary), shown("require_summary", c.RequireSummary)},
			{"require_criteria_results", "Criteria results", onOff(c.RequireCriteriaResults), shown("require_criteria_results", c.RequireCriteriaResults)},
			{"require_lessons", "Lessons, or a reason for none", onOff(c.RequireLessons), shown("require_lessons", c.RequireLessons)},
			{"require_mistakes", "Mistakes", onOff(c.RequireMistakes), shown("require_mistakes", c.RequireMistakes)},
			{"require_no_active_steps", "No active steps", onOff(c.RequireNoActiveSteps), shown("require_no_active_steps", c.RequireNoActiveSteps)},
		},
		LearningsHref: projectLearningsHref(p.Name),
		Operator:      s.Key.Scope == auth.ScopeOperator,
	}
	code := http.StatusOK
	if f.active() {
		code = f.Status
	}
	u.render(w, code, "project", view{Title: p.Name, Nav: u.navFor(ctx, s), Notice: f.Message, Page: out})
}
