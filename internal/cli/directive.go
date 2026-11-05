package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// Directive is the sub-command `cairn directive`: the binding rules that apply to a project, for a
// person or an agent with only a shell (issue #87). Its verb is list, which reads them through
// directive_list (CONTRACT §5). It takes --json, which writes the result as one line of JSON: the
// object the MCP tool returns, with the same field names.
//
// The text form is a summary. It prints each directive's code, scope, title and rule, then the
// expired and omitted counts when they are not zero. It leaves out a directive's applies-when,
// rationale, reviewer check, example, ratification and review date, which --json keeps.
//
// Exit codes: 0 when the answer was printed, 1 when the open or the call failed, 2 for a usage
// error.
func Directive(open Opener, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn directive: name what to do: cairn directive list")
		return 2
	}
	verb, rest := args[0], args[1:]
	if verb == "list" {
		return directiveList(open, rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn directive: unknown verb %q: use cairn directive list\n", verb)
	return 2
}

// directiveList is `cairn directive list [--agent role] [--json]`: the directives that apply to the
// project, ranked agent, project, then global, at most 12 (CONTRACT §5, directive_list). --agent
// narrows the agent-scoped directives to one role. It is not read from the CAIRN_AGENT variable: an
// empty agent is the explicit listing of every role, and a variable that narrowed it would make that
// listing impossible while the variable is set.
func directiveList(open Opener, args []string, stdout, stderr io.Writer) int {
	const verb = "cairn directive list"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", "a role name, such as reviewer; narrows the agent-scoped directives to it; leave it out to list every role")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q\n", verb, pos)
		return 2
	}
	if given(fl, "agent") {
		if strings.TrimSpace(*agent) == "" {
			fmt.Fprintf(stderr, "%s: --agent must name a role; leave it out to list every role\n", verb)
			return 2
		}
		if err := core.CheckCaller(core.Caller{Agent: *agent}); err != nil {
			fmt.Fprintf(stderr, "%s: the name from --agent is refused: %s\n", verb, Reason(err))
			return 2
		}
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	list, err := s.Svc.DirectiveList(s.Ctx, core.DirectiveListRequest{Agent: *agent})
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, list)
	}
	writeDirectiveList(stdout, list)
	return 0
}

// writeDirectiveList prints the summary of a directive list: each directive's code, scope and title,
// then its rule on the next line, then the expired and omitted counts when they are not zero. The title
// and the rule go through Plain, so each is capped to one line, as recall's lesson, mistake and task lines
// are. With no directive it prints the empty-list line (emptyLine), and the counts still follow.
func writeDirectiveList(w io.Writer, list *core.DirectiveList) {
	if len(list.Directives) == 0 {
		fmt.Fprintln(w, emptyLine("directives"))
	}
	for _, d := range list.Directives {
		fmt.Fprintf(w, "  %s  %s  %s\n", FoldLine(d.Code), scopeLabel(d), Plain(d.Title))
		if rule := Plain(d.Rule); rule != "" {
			fmt.Fprintf(w, "          %s\n", rule)
		}
	}
	if list.Expired > 0 {
		fmt.Fprintf(w, "expired     %d past review_by, left out\n", list.Expired)
	}
	if list.Omitted > 0 {
		fmt.Fprintf(w, "omitted     %d beyond the cap of %d\n", list.Omitted, core.DirectiveCap)
	}
}
