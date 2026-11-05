package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
)

// narrowEntry is what the table holds for a tool whose narrowClause names arguments: the clause's
// exact text, and the arguments it names. The text is pinned in full, not matched by a word, so an
// edit to the clause fails until this entry is edited with it, and a dropped argument phrase fails
// as well.
type narrowEntry struct {
	clause string
	args   []string
}

// narrowArgs is the table of tools whose narrowClause names arguments. A tool with a clause that
// is not in this table fails the test, so a new clause is written here with the arguments it names.
var narrowArgs = map[string]narrowEntry{
	"task_get": {
		clause: "narrow the read with the section argument (brief, steps, events or close); for events, page with the limit and cursor arguments. A brief over the limit cannot be narrowed further, so its text must be shortened",
		args:   []string{"section", "limit", "cursor"},
	},
	"task_list": {
		clause: "narrow the list with the status argument, or page with the limit and cursor arguments",
		args:   []string{"status", "limit", "cursor"},
	},
	"learning_list": {
		clause: "narrow the list with the status, kind or target arguments, or page with the limit and cursor arguments",
		args:   []string{"status", "kind", "target", "limit", "cursor"},
	},
	"search": {
		clause: "narrow the search with the kinds argument, or page with the limit and cursor arguments",
		args:   []string{"kinds", "limit", "cursor"},
	},
	"directive_list": {
		clause: "narrow the list with the agent argument",
		args:   []string{"agent"},
	},
	"recall": {
		clause: "ask for fewer items with the limit argument",
		args:   []string{"limit"},
	},
}

// TestNarrowClauseNamesOnlyArgumentsTheSchemaHas checks #111 for every tool. A tool whose clause is
// not the generic one has an entry in the table, and its clause is the entry's text. Each argument
// the entry lists is in the tool's input schema. Each schema property the clause names as a whole
// word is one the entry lists, so a clause that names an argument the table omits fails. A tool with
// a paging argument, limit or cursor, must have a clause, not the generic one. A renamed argument, an
// edited clause, or a tool added with paging and no clause, fails here.
func TestNarrowClauseNamesOnlyArgumentsTheSchemaHas(t *testing.T) {
	schemas := toolSchemas(t)
	if len(schemas) != 15 {
		t.Fatalf("the server lists %d tools, want the fifteen of DESIGN.md §4", len(schemas))
	}
	generic := narrowClause("no-such-tool")
	for name := range narrowArgs {
		if _, ok := schemas[name]; !ok {
			t.Errorf("the table names %s, which the server does not list", name)
		}
	}
	for name, props := range schemas {
		clause := narrowClause(name)
		entry, hasEntry := narrowArgs[name]
		if clause == generic {
			if props["limit"] || props["cursor"] {
				t.Errorf("%s pages its result with limit or cursor, but its narrowClause is the generic one", name)
			}
			if hasEntry {
				t.Errorf("%s has an entry in narrowArgs but the generic narrowClause", name)
			}
			continue
		}
		if !hasEntry {
			t.Errorf("%s has a narrowClause but no entry in narrowArgs", name)
			continue
		}
		if clause != entry.clause {
			t.Errorf("%s: its narrowClause is not the text the table holds; edit both together:\n got  %q\n want %q", name, clause, entry.clause)
		}
		for _, arg := range entry.args {
			if !props[arg] {
				t.Errorf("%s: the table names %q, which its input schema lacks", name, arg)
			}
		}
		for prop := range props {
			if wordIn(clause, prop) && !slices.Contains(entry.args, prop) {
				t.Errorf("%s: its narrowClause names %q, which the table does not list", name, prop)
			}
		}
	}
}

// wordIn reports whether word occurs in text as a whole word: not inside a longer run of letters,
// digits or underscores.
func wordIn(text, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

// toolSchemas lists the tools of a server over an in-memory transport, and returns the names of
// the input properties of each, by tool name.
func toolSchemas(t *testing.T) map[string]map[string]bool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := New(core.Unimplemented{}, Project{Name: "acme-api"}, "test")
	go func() { _ = srv.Run(ctx, serverT) }()
	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "narrow-test", Version: "0"}, nil)
	cs, err := mc.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect the MCP client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	out := make(map[string]map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: schema does not encode: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: schema is not an object: %v", tool.Name, err)
		}
		props := make(map[string]bool, len(schema.Properties))
		for p := range schema.Properties {
			props[p] = true
		}
		out[tool.Name] = props
	}
	return out
}
