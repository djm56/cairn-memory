package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/docscheck"
)

// cliReferencePage is the wiki page that lists the CLI verbs, relative to this package.
const cliReferencePage = "../../wiki/CLI-Reference.md"

// TestCLIReferencePageMatchesDispatchTable is the docs-drift gate for the CLI verbs (CONTRIBUTING.md,
// Keeping docs current). The names in the dispatch table, commands, must be the names in the marked
// table of wiki/CLI-Reference.md, help and version included. A verb added, renamed or removed on
// either side fails the test, and the failure names each verb that differs.
func TestCLIReferencePageMatchesDispatchTable(t *testing.T) {
	registered := make([]string, 0, len(commands))
	for name := range commands {
		registered = append(registered, name)
	}
	sort.Strings(registered)

	onPage, err := docscheck.Names(cliReferencePage, "cairn-cli-verbs")
	if err != nil {
		t.Fatalf("read the verb table: %v", err)
	}

	missing, extra := docscheck.Diff(registered, onPage)
	if len(missing) > 0 {
		t.Errorf("wiki/CLI-Reference.md has no row for these dispatched verbs: %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("wiki/CLI-Reference.md has rows for verbs the binary does not dispatch: %s", strings.Join(extra, ", "))
	}
}
