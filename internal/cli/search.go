package cli

import (
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/core"
)

// Search is `cairn search`: a full-text search of the rows a project keeps, for a person or an agent
// with only a shell (issue #87). It is the search call of CONTRACT §6 and takes no verb. --query is the
// text to find, as plain words that no operator reads as syntax, and --query - reads it from standard
// input. --kind narrows the search to one kind and may be given more than once; left out, every kind
// is searched. --all-projects searches every project and not only this one. --limit and --cursor page the
// hits, as the server counts them. It takes --json, which writes the result as one line of JSON: the
// object the MCP tool returns, with the same field names.
//
// A --kind that is not one of the seven kinds is a usage error, exit 2, before the call, as the server
// would refuse it. A --query with no word to match is not an error: the search has no hit.
//
// The text form is a summary. It prints one line for each hit with its kind, id, project and title, and
// the hit's snippet on the line under it. A search with no hit prints the empty-list line. In a snippet,
// the words in square brackets are the words the search matched. The brackets are markers the server
// adds, not text of the row. --json keeps the snippet as the server gives it, and the task id of each hit,
// which the summary leaves out.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the open or the call failed
// (ExitCodeFor), 2 for a usage error.
func Search(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn search"
	fl, asJSON := newFlags(verb)
	query := fl.String("query", "", "the text to find: plain words, which no operator reads as syntax; - reads standard input")
	kinds := listFlag[core.HitKind]{name: "kind"}
	fl.Var(&kinds, "kind", "a kind to search: task, event, learning, directive, note, lesson or mistake; repeat for more; leave it out to search all")
	allProjects := fl.Bool("all-projects", false, "search every project, not only this one")
	limit := fl.Int("limit", 0, "hits per page: 10 by default, 100 at most; leave it out for the default")
	cursor := fl.String("cursor", "", "the next value from the last page")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if limitUsage(fl, verb, *limit, stderr) {
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q: give the text with --query\n", verb, pos)
		return 2
	}
	if !given(fl, "query") {
		fmt.Fprintf(stderr, "%s: --query is required: cairn search --query \"plain words\"\n", verb)
		return 2
	}
	for _, k := range kinds.vals {
		if !validSearchKind(k) {
			fmt.Fprintf(stderr, "%s: --kind must be task, event, learning, directive, note, lesson or mistake, not %q: cairn search --query text --kind lesson\n", verb, k)
			return 2
		}
	}
	in := &inputs{stdin: stdin}
	text, err := in.text("query", *query)
	if err != nil {
		return usageErr(stderr, verb, err)
	}
	s, err := open()
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	res, err := s.Svc.Search(s.Ctx, core.SearchRequest{
		Query: text, Kinds: kinds.vals, AllProjects: *allProjects, Limit: *limit, Cursor: *cursor,
	})
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, res)
	}
	writeSearch(stdout, res)
	return 0
}

// searchKinds are the kinds a search may name (CONTRACT §6). The check is made here, before the project
// is resolved, so a kind outside them is a usage error and not a refused call.
var searchKinds = []core.HitKind{
	core.HitTask, core.HitEvent, core.HitLearning, core.HitDirective, core.HitNote, core.HitLesson, core.HitMistake,
}

// validSearchKind reports whether kind is one a search may name.
func validSearchKind(kind core.HitKind) bool {
	for _, k := range searchKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// writeSearch prints the summary of a search: one line for each hit, with its kind, id, project and
// title, the snippet on the line under it, and the cursor of the next page when there is one. A search
// with no hit prints the empty-list line.
func writeSearch(w io.Writer, res *core.SearchResult) {
	if len(res.Hits) == 0 {
		fmt.Fprintln(w, emptyLine("hits"))
	}
	for _, h := range res.Hits {
		line := fmt.Sprintf("%s  %s", Plain(string(h.Kind)), Plain(h.ID))
		if p := Plain(h.Project); p != "" {
			line += "  " + p
		}
		if t := Plain(h.Title); t != "" {
			line += "  " + t
		}
		fmt.Fprintln(w, line)
		if s := Plain(h.Snippet); s != "" {
			fmt.Fprintf(w, "          %s\n", s)
		}
	}
	if res.Next != "" {
		fmt.Fprintf(w, "more hits: pass --cursor %s to read the next page\n", FoldLine(res.Next))
	}
}
