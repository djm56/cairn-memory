package core

import (
	"context"
	"regexp"
	"strings"
	"unicode"
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

// CheckCaller refuses an agent or client name that the wire form cannot carry
// unchanged (step 8 review W3). The name must hold no control character and no
// space at either end. An HTTP header value cannot carry a control character, and
// loses its edge spaces on the way, so a name that passed here would reach the
// server as a different name. A blank name is no name, and the write checks
// refuse it (CONTRACT §1.1). CheckCaller writes nothing. The client runs it before
// a call is sent, so the same refusal comes back over HTTP.
func CheckCaller(c Caller) error {
	if err := checkName("agent name", c.Agent); err != nil {
		return err
	}
	return checkName("client name", c.Client)
}

// checkName is CheckCaller for one name. The message names the field and the fix.
func checkName(field, name string) error {
	if blank(name) || (strings.IndexFunc(name, unicode.IsControl) < 0 && strings.TrimSpace(name) == name) {
		return nil
	}
	return Invalid("the %s %q has a control character or a space at its edge: give a name of printable text with no space at either end", field, name)
}
