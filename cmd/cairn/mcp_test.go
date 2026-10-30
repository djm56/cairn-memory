package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// TestMCPRefusesWithoutAServer checks D11 and #66: cairn mcp refuses to start with
// no server, names the fix, and writes nothing to stdout, which is the protocol's.
// CLAUDE_PROJECT_DIR points at an empty directory so no .cairn.toml is read.
func TestMCPRefusesWithoutAServer(t *testing.T) {
	t.Setenv("CAIRN_SERVER", "")
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := serveMCP(nil, io.NopCloser(strings.NewReader("")), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "cairn init") || !strings.Contains(stderr.String(), "CAIRN_SERVER") {
		t.Errorf("stderr %q does not name the fix", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want nothing: it is the protocol's", stdout.String())
	}
}

// TestMCPTakesNoArguments checks that a sub-command with arguments is a usage error,
// before any server is looked for.
func TestMCPTakesNoArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"mcp", "--stdio"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "takes no arguments") {
		t.Errorf("stderr %q does not say so", stderr.String())
	}
}

// TestMCPStdioFlow runs cairn mcp over pipes standing in for its stdin and stdout,
// and drives a small task through it with the MCP SDK's client. Each call goes
// through the server (api over a real core.Local), and the session ends cleanly
// when the client closes its end.
func TestMCPStdioFlow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(api.Handler(core.NewLocal(st), api.UnauthenticatedHeaderCaller, nil))
	t.Cleanup(srv.Close)

	t.Setenv("CAIRN_SERVER", srv.URL)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())

	// The server reads what the client writes, and the client reads what it writes.
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- serveMCP(nil, inR, outW, &stderr) }()

	ctx := context.Background()
	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stdio-test", Version: "0"}, nil)
	cs, err := mc.Connect(ctx, &mcpsdk.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "task_start", Arguments: map[string]any{
		"agent": "main", "title": "Stdio flow", "goal": "Reach the server over pipes.",
	}})
	if err != nil || res.IsError {
		t.Fatalf("task_start over stdio: %v, %+v", err, res)
	}
	var started struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcpsdk.TextContent).Text), &started); err != nil {
		t.Fatalf("task_start result: %v", err)
	}
	if started.ID == "" || started.Status != "active" {
		t.Fatalf("task_start = %+v, want an active task", started)
	}

	// The task is on the server, so task_current over the same session finds it.
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "task_current"})
	if err != nil || res.IsError {
		t.Fatalf("task_current over stdio: %v, %+v", err, res)
	}
	if text := res.Content[0].(*mcpsdk.TextContent).Text; !strings.Contains(text, started.ID) {
		t.Errorf("task_current = %s, want task %s", text, started.ID)
	}

	if err := cs.Close(); err != nil {
		t.Fatalf("close the client session: %v", err)
	}
	_ = inW.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("serveMCP exit code %d, want 0 (stderr %q)", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveMCP did not return after the client closed")
	}
}
