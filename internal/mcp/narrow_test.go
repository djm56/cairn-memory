package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
)

// narrowArgs names, for each tool whose narrowClause names arguments, the arguments the clause
// names. The test checks each one against the tool's input schema, and against the clause text.
// A tool with a clause that is not in this table fails the test, so a new clause is written here
// with the arguments it names.
var narrowArgs = map[string][]string{
	"task_get":       {"section", "limit", "cursor"},
	"task_list":      {"status", "limit", "cursor"},
	"learning_list":  {"status", "kind", "target", "limit", "cursor"},
	"search":         {"kinds", "limit", "cursor"},
	"directive_list": {"agent"},
	"recall":         {"limit"},
}

// TestNarrowClauseNamesOnlyArgumentsTheSchemaHas checks #111 for every tool: a tool whose clause
// names an argument has that argument in its input schema, and the clause names each argument the
// table lists for it. A tool with a paging argument, limit or cursor, must have a clause, not the
// generic one. A renamed argument, or a tool added with paging and no clause, fails here.
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
		named, hasEntry := narrowArgs[name]
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
		for _, arg := range named {
			if !props[arg] {
				t.Errorf("%s: its narrowClause names %q, which its input schema lacks", name, arg)
			}
			if !strings.Contains(clause, arg) {
				t.Errorf("%s: the table names %q, which its narrowClause does not name", name, arg)
			}
		}
	}
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
