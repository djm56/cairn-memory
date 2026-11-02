package core

import "encoding/json"

// Request and result types, one pair per call. docs/CONTRACT.md documents
// each field's rules; validation lives in the Service implementation.
//
// Paged calls take Limit (0 means the call's default; the maximum is
// MaxLimit) and Cursor (empty for the first page; afterwards the Next value
// of the previous result). An empty Next means there is no further page.

const MaxLimit = 100

// StepRef names a step by its ID or by its displayed Number. Exactly one is
// set. The fields are pointers so that "not given" (nil) and a given zero
// differ: {} is invalid everywhere, while {"number": 0} is allowed only as
// StepAddRequest.After, meaning "before the first step" (CONTRACT §1.4, §3).
// Build a ref with IDRef or NumberRef.
type StepRef struct {
	ID     *int64 `json:"id,omitempty"`
	Number *int   `json:"number,omitempty"`
}

// IDRef returns a ref to the step with the given id.
func IDRef(id int64) StepRef { return StepRef{ID: &id} }

// NumberRef returns a ref to the step shown as n. NumberRef(0) is only valid
// as StepAddRequest.After.
func NumberRef(n int) StepRef { return StepRef{Number: &n} }

// StepInput is one step of a plan.
type StepInput struct {
	Title string `json:"title"`
	Agent string `json:"agent,omitempty"`
}

// ---- task_current ----

// TaskCurrent is the active task, or Task nil when none is active.
type TaskCurrent struct {
	Task         *Task       `json:"task"`
	Criteria     []Criterion `json:"criteria,omitempty"`
	Steps        []Step      `json:"steps,omitempty"`
	RecentEvents []Event     `json:"recent_events,omitempty"` // newest first, at most 5
	Directives   []Directive `json:"directives,omitempty"`    // that apply to the project, ranked, at most DirectiveCap
	Expired      int         `json:"expired"`                 // applicable directives past review_by, left out
	Omitted      int         `json:"omitted"`                 // applicable directives beyond the cap, left out
	// OtherActive counts further active tasks when the project allows
	// parallel tasks. Task is then the one started most recently.
	OtherActive int `json:"other_active,omitempty"`
}

// ---- task_start ----

type TaskStartRequest struct {
	Title       string      `json:"title"`
	Goal        string      `json:"goal"`
	Context     string      `json:"context,omitempty"`
	Constraints string      `json:"constraints,omitempty"`
	Priority    Priority    `json:"priority,omitempty"` // default normal
	Criteria    []string    `json:"criteria,omitempty"`
	KeyFiles    []TaskFile  `json:"key_files,omitempty"`
	Steps       []StepInput `json:"steps,omitempty"`
	// Queue creates the task open instead of active.
	Queue bool `json:"queue,omitempty"`
}

type TaskStartResult struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
	Steps  []Step     `json:"steps,omitempty"`
}

// ---- task_get ----

type TaskSection string

const (
	SectionAll    TaskSection = ""
	SectionBrief  TaskSection = "brief"
	SectionSteps  TaskSection = "steps"
	SectionEvents TaskSection = "events"
	SectionClose  TaskSection = "close"
)

// TaskGetRequest asks for one task, or one section of it. Limit, Cursor and
// Step page and filter the events section only. With any other section they
// are invalid (CONTRACT §2).
type TaskGetRequest struct {
	ID      string      `json:"id"`
	Section TaskSection `json:"section,omitempty"`
	Limit   int         `json:"limit,omitempty"`  // section events: default 20
	Cursor  string      `json:"cursor,omitempty"` // section events
	Step    *StepRef    `json:"step,omitempty"`   // section events: one step's events
}

// TaskDetail is one task. Task is always set; the other fields are filled
// for the requested section only (all of them for SectionAll).
type TaskDetail struct {
	Task            Task        `json:"task"`
	Criteria        []Criterion `json:"criteria,omitempty"`  // brief, close
	KeyFiles        []TaskFile  `json:"key_files,omitempty"` // brief
	Steps           []Step      `json:"steps,omitempty"`     // steps
	Events          []Event     `json:"events,omitempty"`    // events: newest first; 20 unless paged
	Next            string      `json:"next,omitempty"`      // events: cursor of the next page
	Lessons         []Lesson    `json:"lessons,omitempty"`   // close
	NoLessonsReason string      `json:"no_lessons_reason,omitempty"`
	Mistakes        []Mistake   `json:"mistakes,omitempty"`     // close
	Deliverables    []TaskFile  `json:"deliverables,omitempty"` // close
}

// MarshalJSON writes a TaskDetail with the tags above, except for events. The
// events key is written whenever the call read the events section, even as [],
// because omitempty would drop an empty list. The other sections keep omitempty,
// so a brief has no events key. A nil Events is not read, and is left out.
func (d TaskDetail) MarshalJSON() ([]byte, error) {
	type plain TaskDetail // the same fields, without this method
	var events *[]Event
	if d.Events != nil {
		events = &d.Events
	}
	return json.Marshal(struct {
		plain
		Events *[]Event `json:"events,omitempty"`
	}{plain: plain(d), Events: events})
}

// ---- task_list ----

type TaskListRequest struct {
	Status []TaskStatus `json:"status,omitempty"` // empty means all
	Limit  int          `json:"limit,omitempty"`  // default 20
	Cursor string       `json:"cursor,omitempty"`
}

type TaskList struct {
	Tasks []TaskSummary `json:"tasks"` // most recently updated first
	Next  string        `json:"next,omitempty"`
}

// ---- task_update ----

// TaskUpdateRequest changes the fields that are set. Title, Goal, Context,
// Constraints, Criteria and KeyFiles are the brief and may change only
// until the first step starts.
type TaskUpdateRequest struct {
	ID         string      `json:"id"`
	Status     *TaskStatus `json:"status,omitempty"` // open, active, blocked or abandoned
	Reason     string      `json:"reason,omitempty"` // required for blocked and abandoned
	Priority   *Priority   `json:"priority,omitempty"`
	Owner      *string     `json:"owner,omitempty"`
	NextAction *string     `json:"next_action,omitempty"`
	Blockers   *string     `json:"blockers,omitempty"`

	Title       *string     `json:"title,omitempty"`
	Goal        *string     `json:"goal,omitempty"`
	Context     *string     `json:"context,omitempty"`
	Constraints *string     `json:"constraints,omitempty"`
	Criteria    *[]string   `json:"criteria,omitempty"`
	KeyFiles    *[]TaskFile `json:"key_files,omitempty"`
}

// ---- task_close ----

type CriterionOutcome struct {
	Ord      int             `json:"ord"`
	Result   CriterionResult `json:"result"`
	Evidence string          `json:"evidence,omitempty"`
}

// TaskCloseRequest closes a task. Lessons and Mistakes distinguish absent
// (nil: not supplied) from empty (supplied, nothing to record); the close
// policy treats them differently, so encoders must not drop empty lists.
type TaskCloseRequest struct {
	ID              string             `json:"id"`
	Outcome         TaskStatus         `json:"outcome"` // done or failed
	Summary         string             `json:"summary"`
	Criteria        []CriterionOutcome `json:"criteria"`
	Lessons         []Lesson           `json:"lessons"`
	NoLessonsReason string             `json:"no_lessons_reason,omitempty"`
	Mistakes        []Mistake          `json:"mistakes"`
	Deliverables    []TaskFile         `json:"deliverables,omitempty"`
}

// The keys a refused task_close lists in Error.Missing.
const (
	MissingOutcome     = "outcome"
	MissingSummary     = "summary"
	MissingCriteria    = "criteria"
	MissingLessons     = "lessons"
	MissingMistakes    = "mistakes"
	MissingActiveSteps = "active_steps"
)

// ---- step_plan, step_add, step_update ----

type StepPlanRequest struct {
	TaskID string      `json:"task_id"`
	Steps  []StepInput `json:"steps"`
}

type StepList struct {
	Steps []Step `json:"steps"`
}

type StepAddRequest struct {
	TaskID string   `json:"task_id"`
	After  *StepRef `json:"after,omitempty"` // nil appends at the end
	Title  string   `json:"title"`
	Agent  string   `json:"agent,omitempty"`
}

type StepAction string

const (
	StepStart  StepAction = "start"
	StepFinish StepAction = "finish"
	StepFail   StepAction = "fail"
	StepSkip   StepAction = "skip"
)

type StepUpdateRequest struct {
	TaskID  string     `json:"task_id"`
	Step    StepRef    `json:"step"`
	Action  StepAction `json:"action"`
	Summary string     `json:"summary,omitempty"` // required for finish, fail and skip
	Agent   *string    `json:"agent,omitempty"`   // reassign the step
}

// ---- event_log ----

type EventLogRequest struct {
	TaskID string          `json:"task_id"`
	Step   *StepRef        `json:"step,omitempty"`
	Kind   EventKind       `json:"kind"`
	Text   string          `json:"text"`
	Data   json.RawMessage `json:"data,omitempty"` // a JSON object, if set
}

// ---- learning_add, learning_list ----

type LearningAddRequest struct {
	TaskID    string       `json:"task_id,omitempty"`
	Kind      LearningKind `json:"kind"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	Target    string       `json:"target,omitempty"`
	Rationale string       `json:"rationale,omitempty"`
	Example   string       `json:"example,omitempty"`
}

type LearningListRequest struct {
	Status []LearningStatus `json:"status,omitempty"` // empty means all
	Kind   []LearningKind   `json:"kind,omitempty"`   // empty means all
	// AllProjects lists every project's learnings; otherwise only the
	// caller's.
	AllProjects bool   `json:"all_projects,omitempty"`
	Target      string `json:"target,omitempty"`
	Limit       int    `json:"limit,omitempty"` // default 20
	Cursor      string `json:"cursor,omitempty"`
}

type LearningList struct {
	Learnings []Learning `json:"learnings"` // newest first
	Next      string     `json:"next,omitempty"`
}

// ---- directive_list ----

type DirectiveListRequest struct {
	// Agent narrows the result to directives for this role, plus the
	// global and project ones. Empty returns global and project directives
	// and every agent-scoped one.
	Agent string `json:"agent,omitempty"`
}

type DirectiveList struct {
	Directives []Directive `json:"directives"` // ranked: agent, project, global; newest first; at most DirectiveCap
	Expired    int         `json:"expired"`    // applicable directives past review_by, left out
	Omitted    int         `json:"omitted"`    // applicable directives beyond the cap, left out
}

// ---- recall ----

type RecallRequest struct {
	Goal  string `json:"goal"`
	Agent string `json:"agent,omitempty"` // narrows directives as in directive_list
	Limit int    `json:"limit,omitempty"` // per list; default 5
}

type LessonHit struct {
	TaskID  string `json:"task_id"`
	Project string `json:"project"`
	Lesson
}

type MistakeHit struct {
	TaskID  string `json:"task_id"`
	Project string `json:"project"`
	Mistake
}

// RecallResult answers "what should I know before this task". Directives
// are those that apply to the caller's project; lessons, mistakes and tasks
// come from every project, best match first.
type RecallResult struct {
	Directives []Directive   `json:"directives"` // ranked and capped as directive_list
	Expired    int           `json:"expired"`    // applicable directives past review_by, left out
	Omitted    int           `json:"omitted"`    // applicable directives beyond the cap, left out
	Lessons    []LessonHit   `json:"lessons"`
	Mistakes   []MistakeHit  `json:"mistakes"`
	Tasks      []TaskSummary `json:"tasks"`
}

// ---- search ----

type HitKind string

const (
	HitTask      HitKind = "task"
	HitEvent     HitKind = "event"
	HitLearning  HitKind = "learning"
	HitDirective HitKind = "directive"
	HitNote      HitKind = "note"
	HitLesson    HitKind = "lesson"
	HitMistake   HitKind = "mistake"
)

type SearchRequest struct {
	Query       string    `json:"query"`
	Kinds       []HitKind `json:"kinds,omitempty"` // empty means all
	AllProjects bool      `json:"all_projects,omitempty"`
	Limit       int       `json:"limit,omitempty"` // default 10
	Cursor      string    `json:"cursor,omitempty"`
}

type SearchHit struct {
	Kind    HitKind `json:"kind"`
	ID      string  `json:"id"` // the task id, the code of a directive, or a row id
	Project string  `json:"project,omitempty"`
	TaskID  string  `json:"task_id,omitempty"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
}

type SearchResult struct {
	Hits []SearchHit `json:"hits"` // best match first
	Next string      `json:"next,omitempty"`
}

// ---- operator calls ----

type LearningDecideRequest struct {
	ID           int64          `json:"id"`
	Status       LearningStatus `json:"status"`                  // anything but proposed
	SupersededBy *int64         `json:"superseded_by,omitempty"` // required for superseded
	Note         string         `json:"note,omitempty"`
}

// DirectiveRatifyRequest creates an active directive. When FromLearning is
// set, that learning must be accepted, and becomes applied.
type DirectiveRatifyRequest struct {
	FromLearning  *int64         `json:"from_learning,omitempty"`
	Scope         DirectiveScope `json:"scope"`
	Project       string         `json:"project,omitempty"` // required for scope project
	Agent         string         `json:"agent,omitempty"`   // required for scope agent
	Title         string         `json:"title"`
	AppliesWhen   string         `json:"applies_when,omitempty"`
	Rule          string         `json:"rule"`
	Rationale     string         `json:"rationale,omitempty"`
	ReviewerCheck string         `json:"reviewer_check,omitempty"`
	Example       string         `json:"example,omitempty"`
	// ReviewBy is a calendar day, YYYY-MM-DD in UTC, after which the directive is
	// due for review. It must be after today. Empty means 90 days from ratification.
	ReviewBy string `json:"review_by,omitempty"`
}

type DirectiveRetireRequest struct {
	Code string `json:"code"`
}

// ProjectSettingsRequest replaces a project's settings. The call replaces the
// whole object, so Settings must give every switch: a nil Settings, or one with
// any switch left nil, is invalid rather than read as off (CONTRACT §8, §9 item 23).
type ProjectSettingsRequest struct {
	Project     string         `json:"project"`
	DisplayName *string        `json:"display_name,omitempty"`
	Settings    *SettingsInput `json:"settings,omitempty"`
}

// SettingsInput is a project's settings as a call gives them. It mirrors
// ProjectSettings with a pointer on every switch, so a switch the call leaves
// out is nil and can be told from one set to false.
type SettingsInput struct {
	ParallelTasks *bool             `json:"parallel_tasks"`
	ClosePolicy   *ClosePolicyInput `json:"close_policy"`
}

// ClosePolicyInput mirrors ClosePolicy with a pointer on every switch.
type ClosePolicyInput struct {
	RequireSummary         *bool `json:"require_summary"`
	RequireCriteriaResults *bool `json:"require_criteria_results"`
	RequireLessons         *bool `json:"require_lessons"`
	RequireMistakes        *bool `json:"require_mistakes"`
	RequireNoActiveSteps   *bool `json:"require_no_active_steps"`
}

// AsInput gives s as a complete SettingsInput, with every switch set. Callers
// use it to send a whole object, such as DefaultSettings with one switch changed.
func (s ProjectSettings) AsInput() *SettingsInput {
	cp := s.ClosePolicy
	return &SettingsInput{
		ParallelTasks: &s.ParallelTasks,
		ClosePolicy: &ClosePolicyInput{
			RequireSummary:         &cp.RequireSummary,
			RequireCriteriaResults: &cp.RequireCriteriaResults,
			RequireLessons:         &cp.RequireLessons,
			RequireMistakes:        &cp.RequireMistakes,
			RequireNoActiveSteps:   &cp.RequireNoActiveSteps,
		},
	}
}

// complete returns in as ProjectSettings when every switch is given. Otherwise
// it returns the JSON names of the switches that are missing, and the settings
// returned must not be used. A nil in has every switch missing.
func (in *SettingsInput) complete() (ProjectSettings, []string) {
	var s ProjectSettings
	if in == nil {
		return s, []string{"parallel_tasks", "close_policy"}
	}
	var missing []string
	if in.ParallelTasks == nil {
		missing = append(missing, "parallel_tasks")
	} else {
		s.ParallelTasks = *in.ParallelTasks
	}
	if in.ClosePolicy == nil {
		return s, append(missing, "close_policy")
	}
	cp := in.ClosePolicy
	switches := []struct {
		name string
		in   *bool
		out  *bool
	}{
		{"close_policy.require_summary", cp.RequireSummary, &s.ClosePolicy.RequireSummary},
		{"close_policy.require_criteria_results", cp.RequireCriteriaResults, &s.ClosePolicy.RequireCriteriaResults},
		{"close_policy.require_lessons", cp.RequireLessons, &s.ClosePolicy.RequireLessons},
		{"close_policy.require_mistakes", cp.RequireMistakes, &s.ClosePolicy.RequireMistakes},
		{"close_policy.require_no_active_steps", cp.RequireNoActiveSteps, &s.ClosePolicy.RequireNoActiveSteps},
	}
	for _, sw := range switches {
		if sw.in == nil {
			missing = append(missing, sw.name)
			continue
		}
		*sw.out = *sw.in
	}
	return s, missing
}
