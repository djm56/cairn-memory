package core

import (
	"errors"
	"fmt"
)

// Kind classifies an Error. Adapters map kinds to their own form: an MCP
// tool error, an HTTP status.
type Kind string

const (
	// KindNotFound: the task, step, learning, directive or note does not exist
	// in the caller's project.
	KindNotFound Kind = "not_found"
	// KindInvalid: the request is malformed: a required field is empty, an
	// enum value is unknown, or the caller has no project or agent.
	KindInvalid Kind = "invalid"
	// KindRefused: the request is well formed but a rule forbids it now: the
	// close policy, a status transition, a second active task.
	KindRefused Kind = "refused"
	// KindOperatorOnly: the call is an operator decision and the caller is
	// not an operator.
	KindOperatorOnly Kind = "operator_only"
)

// Error is the error every Service call returns for a failure the caller
// can act on. Any other error is an internal failure.
type Error struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
	// Missing lists what a refused task_close lacks, as the stable keys
	// documented in docs/CONTRACT.md ("summary", "lessons", ...).
	Missing []string `json:"missing,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// KindOf returns the kind of err, or "" when err is not an *Error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}

// MissingOf returns the Missing list of a refused call, or nil.
func MissingOf(err error) []string {
	var e *Error
	if errors.As(err, &e) {
		return e.Missing
	}
	return nil
}

func NotFound(format string, args ...any) *Error {
	return &Error{Kind: KindNotFound, Message: fmt.Sprintf(format, args...)}
}

func Invalid(format string, args ...any) *Error {
	return &Error{Kind: KindInvalid, Message: fmt.Sprintf(format, args...)}
}

func Refused(format string, args ...any) *Error {
	return &Error{Kind: KindRefused, Message: fmt.Sprintf(format, args...)}
}

func OperatorOnly(format string, args ...any) *Error {
	return &Error{Kind: KindOperatorOnly, Message: fmt.Sprintf(format, args...)}
}

// ErrNoProject is returned for every agent call whose caller has no project.
// The message names the fix; there is no default project (DESIGN.md §2.1).
var ErrNoProject = Invalid("no project: run `cairn init` in the project, or set CAIRN_PROJECT")
