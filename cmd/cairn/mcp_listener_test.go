package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/server"
)

// TestM1FlowOverARealListener is the M2 exit check for the M1 flow (docs/PLAN.md §3, M2): cairn
// mcp → internal/client → a real listener → core. The server is server.Run on 127.0.0.1:0, its
// agent key is made by server.AddKey, and the client is given that key as a user gives it, in
// CAIRN_API_KEY. A small task is driven over stdio, and cairn export task prints it. The key
// is checked in every output: the export, its stderr, and the server's log.
func TestM1FlowOverARealListener(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	key, err := server.AddKey(context.Background(), db, "m1-agent", auth.ScopeAgent)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	logs := &syncWriter{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	stopped := make(chan error, 1)
	go func() {
		stopped <- server.Run(ctx, server.Options{Addr: "127.0.0.1:0", DB: db, Log: logs})
	}()
	base := waitForListener(t, logs)

	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", base)
	t.Setenv("CAIRN_API_KEY", key)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CLAUDE_PROJECT_DIR", dir)

	cs, finish := stdioSession(t)
	started := stdioDo(t, cs, "task_start", map[string]any{
		"agent": "main", "title": "Add rate limit", "goal": "Requests over 100 a minute get 429.",
		"criteria": []string{"429 after 100 requests"},
		"steps":    []map[string]any{{"title": "Write test"}, {"title": "Implement"}},
	})
	id, _ := started["id"].(string)
	if id == "" || started["status"] != "active" {
		t.Fatalf("task_start over the listener = %v, want an active task with an id", started)
	}
	started1 := stdioDo(t, cs, "step_update", map[string]any{
		"agent": "main", "task_id": id, "step": map[string]any{"number": 1}, "action": "start",
	})
	if started1["number"] != float64(1) || started1["status"] != "active" {
		t.Errorf("step_update start = %v, want step 1 active", started1)
	}
	finish()

	code, out, errs := runCmd(t, "export", "task", id)
	if code != 0 {
		t.Fatalf("export task %s: exit %d, stderr %q", id, code, errs)
	}
	for _, want := range []string{"Add rate limit", "Requests over 100 a minute get 429.", "Write test", "Implement"} {
		if !strings.Contains(out, want) {
			t.Errorf("export task does not print %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, key) || strings.Contains(errs, key) {
		t.Errorf("export task printed the key: stdout %q, stderr %q", out, errs)
	}

	stop()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("server stopped with %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop within 15s of its stop signal")
	}
	if strings.Contains(logs.String(), key) {
		t.Errorf("the server log holds the key")
	}
}
