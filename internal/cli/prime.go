package cli

import (
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// primeResult is the --json object of prime. Current is the answer of task current, which prime reads
// with the same call, so it is written as task current writes it.
type primeResult struct {
	Project string            `json:"project"`
	Role    string            `json:"role"`
	Current *core.TaskCurrent `json:"current"`
}

// Prime is `cairn prime [--agent role] [--json]`: the session-start view for an agent that has only a
// shell and no hook. It makes the one call task current makes. It writes the session-start block, the
// same lines the hook writes, closed by a pointer to the brief. When a task is active it then writes a
// blank line and the summary task current writes, without the directive counts and expired and omitted
// lines the block has already given. With no active task the block is the whole answer.
//
// --agent names the caller's role, and CAIRN_AGENT names it when the flag is left out, as task current
// reads it. A name the wire form refuses is a usage error, exit 2, before the project is opened. Prime
// takes no positional argument. --json writes one line: the project, the role ("" when none), and the
// task current result.
//
// Exit codes: 0 when the answer was printed, 1 when the open or the call failed, or the session names no
// project, 2 for a usage error.
func Prime(open Opener, args []string, stdout, stderr io.Writer) int {
	const verb = "cairn prime"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", "the calling agent's role, such as main or reviewer; names its agent-scoped directives; left out, "+identity.EnvAgent+" is used")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q\n", verb, pos)
		return 2
	}
	name, ok := agentName(fl, *agent, false, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	// The project names the block and the JSON. A session with none is a fault in the opener, not a
	// usage error, and it is refused before the call so no answer is printed for an unnamed project.
	if s.Project == "" {
		fmt.Fprintf(stderr, "%s: an internal fault: the session names no project\n", verb)
		return 1
	}
	ctx := s.Ctx
	if name != "" {
		ctx = withAgent(ctx, name)
	}
	cur, err := s.Svc.TaskCurrent(ctx)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, primeResult{Project: s.Project, Role: name, Current: cur})
	}
	SessionStart(stdout, s.Project, cur, name, false, PrimePointer, PrimeOmittedCall)
	if cur.Task == nil {
		return 0
	}
	fmt.Fprintln(stdout)
	writeCurrentSummary(stdout, cur, false)
	return 0
}
