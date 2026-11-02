package store

import "time"

// The row types below mirror the tables in docs/DESIGN.md §3, one per table.
//
// They are not core's types. store cannot import core: core implements its
// rules over the store, so an import the other way is a cycle. The rows also
// hold storage forms that core's types do not: times are time.Time here but
// fixed-width text in the database; a nullable text column is "" in Go; a
// step's Number is computed by the query and ignored on write. Core converts
// between the two. The conversion is a field copy, with no rule in it.
//
// Conventions shared by every row:
//   - an empty string in a nullable text column is SQL NULL, both ways;
//   - a nullable time or id is a pointer, nil for NULL;
//   - enum columns are plain strings. The schema's CHECK constraints refuse an
//     unknown value, and core validates before it writes.

// ProjectRow is one row of projects (DESIGN §3.6). Settings is the JSON the
// caller passes in; the store does not read it.
type ProjectRow struct {
	Name        string
	DisplayName string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	Settings    string
}

// SessionRow is one row of sessions (DESIGN §3.6).
type SessionRow struct {
	ID         string
	Project    string
	Client     string
	Agent      string
	StartedAt  time.Time
	LastSeenAt time.Time
}

// TaskRow is one row of tasks (DESIGN §3.1).
type TaskRow struct {
	ID              string
	Project         string
	Title           string
	Goal            string
	Context         string
	Constraints     string
	Status          string
	Priority        string
	Owner           string
	NextAction      string
	Blockers        string
	OutcomeSummary  string
	NoLessonsReason string
	OpenedAt        time.Time
	StartedAt       *time.Time
	ClosedAt        *time.Time
	UpdatedAt       time.Time
}

// CriterionRow is one row of task_criteria. Result is "" until close.
type CriterionRow struct {
	TaskID    string
	Ord       int
	Criterion string
	Result    string
	Evidence  string
}

// FileRow is one row of task_files. Role is "key" or "deliverable"; the two
// roles share the table and are replaced separately.
type FileRow struct {
	TaskID string
	Role   string
	Ord    int
	Path   string
	Note   string
}

// LessonRow is one row of task_lessons, written at close.
type LessonRow struct {
	TaskID     string
	Ord        int
	Kind       string
	Text       string
	Why        string
	Correction string
}

// MistakeRow is one row of task_mistakes, written at close.
type MistakeRow struct {
	TaskID     string
	Ord        int
	Agent      string
	Mistake    string
	Impact     string
	Correction string
	Prevention string
}

// StepRow is one row of steps (DESIGN §3.2). Number is the displayed position,
// computed by the query that returns the row. It is zero on a row being
// written and is never stored.
type StepRow struct {
	ID        int64
	TaskID    string
	Number    int
	Position  float64
	Title     string
	Agent     string
	Status    string
	Attempts  int
	Summary   string
	StartedAt *time.Time
	EndedAt   *time.Time
}

// EventRow is one row of events (DESIGN §3.3). Data is the JSON text the
// caller passes in, or "" for none.
type EventRow struct {
	ID         int64
	TaskID     string
	Project    string
	StepID     *int64
	Agent      string
	SessionID  string
	Kind       string
	Text       string
	Data       string
	OccurredAt time.Time
}

// ReviewRow is one row of reviews (DESIGN §3.3).
type ReviewRow struct {
	ID        int64
	TaskID    string
	StepID    int64
	Reviewer  string
	Verdict   string
	Critical  int
	Warnings  int
	Summary   string
	CreatedAt time.Time
}

// LearningRow is one row of learnings (DESIGN §3.4).
type LearningRow struct {
	ID           int64
	Project      string
	TaskID       string
	Agent        string
	Kind         string
	Title        string
	Body         string
	Target       string
	Rationale    string
	Example      string
	Status       string
	SupersededBy *int64
	DecidedBy    string
	DecidedAt    *time.Time
	DecisionNote string
	CreatedAt    time.Time
}

// DirectiveRow is one row of directives (DESIGN §3.4). Project is "" unless the
// scope is project; Agent is "" unless the scope is agent.
type DirectiveRow struct {
	ID             int64
	Code           string
	Scope          string
	Project        string
	Agent          string
	Title          string
	AppliesWhen    string
	Rule           string
	Rationale      string
	ReviewerCheck  string
	Example        string
	Status         string
	SourceLearning *int64
	RatifiedBy     string
	RatifiedAt     *time.Time
	RetiredAt      *time.Time
	ReviewBy       *time.Time // nil only on a retired row from before schema v5
}

// NoteRow is one row of notes (DESIGN §3.5). TaskID is "" for a note that is
// not attached to a task.
type NoteRow struct {
	ID        int64
	TaskID    string
	Project   string
	Title     string
	Body      string
	CreatedBy string
	UpdatedAt time.Time
}

// SearchRow is one hit from search_index. Ref is the source key as text: a row
// id, a directive code's row id, or "task-id/ord" for a lesson or a mistake.
// Snippet is an excerpt with the matched words in square brackets.
type SearchRow struct {
	Kind    string
	Ref     string
	Project string
	TaskID  string
	Title   string
	Snippet string
}

// APIKeyNew is an API key to add (DESIGN §3.6, §8). Hash is the SHA-256 hex of
// the key's plaintext, from auth.HashKey: the plaintext itself never reaches the
// store. Scope is one of agent, operator or viewer, and the schema refuses any
// other value.
type APIKeyNew struct {
	Name      string
	Hash      string
	Scope     string
	CreatedAt time.Time
}

// APIKeyInfo is one row of api_keys as the store returns it. It carries no hash:
// nothing outside the store reads one back (DESIGN §8). LastUsedAt and RevokedAt
// are nil for NULL.
type APIKeyInfo struct {
	ID         int64
	Name       string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}
