package core

import (
	"context"
	"regexp"
)

// Role is what a caller may do. Adapters set it: the MCP adapter always uses
// RoleAgent; the server derives it from the API key's scope, never from
// anything the client says.
type Role string

const (
	RoleAgent    Role = "agent"
	RoleOperator Role = "operator"
)

// Caller identifies who makes a call (DESIGN.md §2.1). It travels in the
// context so that request types carry only the call's own fields.
type Caller struct {
	Project string // required for every Agent call
	Agent   string // required for every Agent write: "main", "reviewer", ...
	Session string // optional, when the client supplies one
	Client  string // optional: "claude-code", "opencode", ...
	Role    Role
}

type callerKey struct{}

// WithCaller returns ctx carrying c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the caller in ctx, if any.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

var projectName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidProjectName reports whether name is a valid project name: lowercase
// letters, digits and single hyphens, at most 64 characters.
func ValidProjectName(name string) bool {
	return len(name) <= 64 && projectName.MatchString(name)
}
