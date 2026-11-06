package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/docscheck"
)

// mcpToolsPage is the wiki page that lists the MCP tools, relative to this package.
const mcpToolsPage = "../../wiki/MCP-Tools.md"

// TestMCPToolsPageMatchesRegisteredTools is the docs-drift gate for the MCP tools (CONTRIBUTING.md,
// Keeping docs current). The names the server registers, read over the protocol, must be the names
// in the marked table of wiki/MCP-Tools.md. A tool added, renamed or removed on either side fails
// the test, and the failure names each tool that differs.
func TestMCPToolsPageMatchesRegisteredTools(t *testing.T) {
	cs := connect(t, core.Unimplemented{}, acme)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	registered := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		registered = append(registered, tool.Name)
	}

	onPage, err := docscheck.Names(mcpToolsPage, "cairn-mcp-tools")
	if err != nil {
		t.Fatalf("read the tool table: %v", err)
	}

	missing, extra := docscheck.Diff(registered, onPage)
	if len(missing) > 0 {
		t.Errorf("wiki/MCP-Tools.md has no row for these registered tools: %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("wiki/MCP-Tools.md has rows for tools the server does not register: %s", strings.Join(extra, ", "))
	}
}
