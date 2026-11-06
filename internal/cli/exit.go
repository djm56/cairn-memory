package cli

import (
	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
)

// Exit codes of the verbs that take --json: task, step, event, learning, directive, recall, search and
// prime. A failed call exits with the code of its error kind, so a script or an agent with only a shell
// can tell a refusal from a missing row without reading the message:
//
//	0  success
//	1  a failure with no kind: an internal failure of the server, or one raised on this machine
//	2  a usage error, refused before any call: the message is on stderr, and nothing is on stdout
//	3  invalid: the request is malformed, a value is unknown, or there is no project or agent (core.KindInvalid)
//	4  not_found: the task, step, learning or directive is not in the caller's project (core.KindNotFound)
//	5  refused: a rule forbids the call now, such as a status move or the close policy (core.KindRefused)
//	6  operator_only: an operator call made without the operator role (core.KindOperatorOnly)
//	7  no_route: the path is not a call, so the server base URL is wrong (api.KindNoRoute)
//	8  unsupported_media_type: the request body is not application/json (api.KindUnsupportedMediaType)
//
// The codes 3 to 8 are the kinds of CONTRACT §1.2 that reach the CLI with their kind, in the order the
// section lists them. The kinds unauthenticated and read_only do not: the client answers them as plain
// errors, so they exit 1 with the kind internal, as any other failure with no kind does. The other
// commands (status, doctor, export and the rest) exit 1 on a failure and do not use this table.
const (
	// ExitOK is the code of a call that succeeded.
	ExitOK = 0
	// ExitFailure is the code of a failure with no kind.
	ExitFailure = 1
	// ExitUsage is the code of a usage error, which is refused before any call.
	ExitUsage = 2
)

// exitByKind is the exit code of each kind a call can fail with (the table in the package comment).
var exitByKind = map[core.Kind]int{
	core.KindInvalid:             3,
	core.KindNotFound:            4,
	core.KindRefused:             5,
	core.KindOperatorOnly:        6,
	api.KindNoRoute:              7,
	api.KindUnsupportedMediaType: 8,
}

// ExitCodeFor returns the exit code of a failed call of kind k. A kind the table does not name, including
// the empty kind of a failure with none, is ExitFailure.
func ExitCodeFor(k core.Kind) int {
	if code, ok := exitByKind[k]; ok {
		return code
	}
	return ExitFailure
}

// ExitCodeHelp is the table of exit codes that cairn --help prints. It is the package comment's table in
// words, so the help and the code cannot drift apart while the codes are written in one place each.
const ExitCodeHelp = `Exit codes for task, step, event, learning, directive, recall, search and prime:
  0  success
  1  a failure with no kind (an internal failure, or one raised on this machine)
  2  a usage error, refused before any call: the message is on stderr
  3  invalid           4  not_found      5  refused
  6  operator_only     7  no_route       8  unsupported_media_type
With --json, a failed call writes {"error": {"kind": ..., "message": ..., "missing": [...]}} to stdout,
and nothing to stderr. "missing" is listed only for a refused close. Other commands exit 1 on a failure.`
