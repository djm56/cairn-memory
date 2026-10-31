package core

import (
	"encoding/json"
	"time"
)

// The records the Service returns (DESIGN.md §3). JSON names are what an
// MCP client and the HTTP API see. Times are UTC and set by the server.

type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskActive    TaskStatus = "active"
	TaskBlocked   TaskStatus = "blocked"
	TaskDone      TaskStatus = "done"
	TaskFailed    TaskStatus = "failed"
	TaskAbandoned TaskStatus = "abandoned"
)

// Terminal reports whether no further change is allowed.
func (s TaskStatus) Terminal() bool {
	return s == TaskDone || s == TaskFailed || s == TaskAbandoned
}

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

type Task struct {
	ID             string     `json:"id"`
	Project        string     `json:"project"`
	Title          string     `json:"title"`
	Goal           string     `json:"goal"`
	Context        string     `json:"context,omitempty"`
	Constraints    string     `json:"constraints,omitempty"`
	Status         TaskStatus `json:"status"`
	Priority       Priority   `json:"priority"`
	Owner          string     `json:"owner,omitempty"`
	NextAction     string     `json:"next_action,omitempty"`
	Blockers       string     `json:"blockers,omitempty"`
	OutcomeSummary string     `json:"outcome_summary,omitempty"`
	OpenedAt       time.Time  `json:"opened_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TaskSummary is one line of a task list.
type TaskSummary struct {
	ID        string     `json:"id"`
	Project   string     `json:"project"`
	Title     string     `json:"title"`
	Status    TaskStatus `json:"status"`
	Priority  Priority   `json:"priority"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CriterionResult string

const (
	CriterionMet     CriterionResult = "met"
	CriterionPartial CriterionResult = "partial"
	CriterionUnmet   CriterionResult = "unmet"
	CriterionUnclear CriterionResult = "unclear"
)

// Criterion is one acceptance criterion. Result is empty until close.
type Criterion struct {
	Ord       int             `json:"ord"`
	Criterion string          `json:"criterion"`
	Result    CriterionResult `json:"result,omitempty"`
	Evidence  string          `json:"evidence,omitempty"`
}

type FileRole string

const (
	FileKey         FileRole = "key"
	FileDeliverable FileRole = "deliverable"
)

type TaskFile struct {
	Path string   `json:"path"`
	Role FileRole `json:"role,omitempty"` // set by the server from where the file was given
	Note string   `json:"note,omitempty"`
}

type LessonKind string

const (
	LessonWrongAssumption LessonKind = "wrong-assumption"
	LessonReuse           LessonKind = "reuse"
	LessonSimilarTask     LessonKind = "similar-task"
)

type Lesson struct {
	Kind       LessonKind `json:"kind"`
	Text       string     `json:"text"`
	Why        string     `json:"why,omitempty"`
	Correction string     `json:"correction,omitempty"`
}

type Impact string

const (
	ImpactLow  Impact = "low"
	ImpactMed  Impact = "med"
	ImpactHigh Impact = "high"
)

type Mistake struct {
	Agent      string `json:"agent"`
	Mistake    string `json:"mistake"`
	Impact     Impact `json:"impact"`
	Correction string `json:"correction,omitempty"`
	Prevention string `json:"prevention,omitempty"`
}

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepActive  StepStatus = "active"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// Step is one planned piece of a task. Number is the displayed position,
// computed from the order each time; ID never changes (DESIGN.md §3.2).
type Step struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	TaskID    string     `json:"task_id"`
	Title     string     `json:"title"`
	Agent     string     `json:"agent,omitempty"`
	Status    StepStatus `json:"status"`
	Attempts  int        `json:"attempts"`
	Summary   string     `json:"summary,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type EventKind string

const (
	EventNote      EventKind = "note"
	EventStarted   EventKind = "started"
	EventFinished  EventKind = "finished"
	EventBlocked   EventKind = "blocked"
	EventUnblocked EventKind = "unblocked"
	EventHandoff   EventKind = "handoff"
	EventDecision  EventKind = "decision"
	EventReview    EventKind = "review"
	EventClose     EventKind = "close"
	EventCustom    EventKind = "custom"
)

// AgentLoggable reports whether an agent may log this kind with event_log.
// started, finished and close are written by the calls they record. A review
// is logged by an agent with its verdict and counts in data (CONTRACT §4).
func (k EventKind) AgentLoggable() bool {
	switch k {
	case EventNote, EventBlocked, EventUnblocked, EventHandoff, EventDecision, EventReview, EventCustom:
		return true
	}
	return false
}

type Event struct {
	ID         int64           `json:"id"`
	TaskID     string          `json:"task_id"`
	Project    string          `json:"project"`
	StepID     *int64          `json:"step_id,omitempty"`
	Agent      string          `json:"agent"`
	SessionID  string          `json:"session_id,omitempty"`
	Kind       EventKind       `json:"kind"`
	Text       string          `json:"text"`
	Data       json.RawMessage `json:"data,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type Verdict string

const (
	VerdictApproved    Verdict = "approved"
	VerdictNotApproved Verdict = "not-approved"
)

type LearningKind string

const (
	LearningFinding   LearningKind = "finding"
	LearningPattern   LearningKind = "pattern"
	LearningAgentNote LearningKind = "agent-note"
)

type LearningStatus string

const (
	LearningProposed   LearningStatus = "proposed"
	LearningAccepted   LearningStatus = "accepted"
	LearningRejected   LearningStatus = "rejected"
	LearningApplied    LearningStatus = "applied"
	LearningRetired    LearningStatus = "retired"
	LearningSuperseded LearningStatus = "superseded"
)

type Learning struct {
	ID           int64          `json:"id"`
	Project      string         `json:"project"`
	TaskID       string         `json:"task_id,omitempty"`
	Agent        string         `json:"agent"`
	Kind         LearningKind   `json:"kind"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Target       string         `json:"target,omitempty"`
	Rationale    string         `json:"rationale,omitempty"`
	Example      string         `json:"example,omitempty"`
	Status       LearningStatus `json:"status"`
	SupersededBy *int64         `json:"superseded_by,omitempty"`
	DecidedBy    string         `json:"decided_by,omitempty"`
	DecidedAt    *time.Time     `json:"decided_at,omitempty"`
	DecisionNote string         `json:"decision_note,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type DirectiveScope string

const (
	ScopeGlobal  DirectiveScope = "global"
	ScopeProject DirectiveScope = "project"
	ScopeAgent   DirectiveScope = "agent"
)

type DirectiveStatus string

const (
	DirectiveActive  DirectiveStatus = "active"
	DirectiveRetired DirectiveStatus = "retired"
)

type Directive struct {
	ID             int64           `json:"id"`
	Code           string          `json:"code"` // D-0001
	Scope          DirectiveScope  `json:"scope"`
	Project        string          `json:"project,omitempty"` // set only for scope project
	Agent          string          `json:"agent,omitempty"`   // set only for scope agent: a role name
	Title          string          `json:"title"`
	AppliesWhen    string          `json:"applies_when,omitempty"`
	Rule           string          `json:"rule"`
	Rationale      string          `json:"rationale,omitempty"`
	ReviewerCheck  string          `json:"reviewer_check,omitempty"`
	Example        string          `json:"example,omitempty"`
	Status         DirectiveStatus `json:"status"`
	SourceLearning *int64          `json:"source_learning,omitempty"`
	RatifiedBy     string          `json:"ratified_by"`
	RatifiedAt     time.Time       `json:"ratified_at"`
	RetiredAt      *time.Time      `json:"retired_at,omitempty"`
}

// ClosePolicy is what task_close requires (DESIGN.md §3.7). Each field turns
// one requirement on; DefaultSettings turns them all on.
type ClosePolicy struct {
	RequireSummary         bool `json:"require_summary"`
	RequireCriteriaResults bool `json:"require_criteria_results"`
	RequireLessons         bool `json:"require_lessons"`
	RequireMistakes        bool `json:"require_mistakes"`
	RequireNoActiveSteps   bool `json:"require_no_active_steps"`
}

type ProjectSettings struct {
	ParallelTasks bool        `json:"parallel_tasks"`
	ClosePolicy   ClosePolicy `json:"close_policy"`
}

// DefaultSettings are a new project's settings.
func DefaultSettings() ProjectSettings {
	return ProjectSettings{
		ClosePolicy: ClosePolicy{
			RequireSummary:         true,
			RequireCriteriaResults: true,
			RequireLessons:         true,
			RequireMistakes:        true,
			RequireNoActiveSteps:   true,
		},
	}
}

// Project is created on its first agent call; nothing registers it first.
type Project struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name,omitempty"`
	Settings    ProjectSettings `json:"settings"`
	CreatedAt   time.Time       `json:"created_at"`
	LastSeenAt  time.Time       `json:"last_seen_at"`
}
