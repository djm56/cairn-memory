package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// runExport is the sub-command `cairn export`. Only `task` is built (DESIGN.md §5.2);
// `project` is in the scope of v1 with no milestone yet, so it is named as not built.
func runExport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn export: name what to export: cairn export task [id]")
		return 2
	}
	switch args[0] {
	case "task":
		return runExportTask(args[1:], stdout, stderr)
	case "project":
		fmt.Fprintln(stderr, "cairn export: export project is not built yet (docs/PLAN.md §3, scope of v1)")
		return 2
	}
	fmt.Fprintf(stderr, "cairn export: unknown export %q: use cairn export task [id]\n", args[0])
	return 2
}

// runExportTask is `cairn export task [id]`: one task as Markdown on stdout. Without
// an id it exports the active task. It reads through the server, all of the brief,
// the steps and the close, and every page of the journal.
//
// Exit codes: 0 when the task was printed, 1 when the call failed or no task is
// active, 2 for a usage error.
func runExportTask(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintf(stderr, "cairn export task: takes one task id, got %q\n", args)
		return 2
	}
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn export task: the working directory: %v\n", err)
		return 1
	}
	t, err := dial(wd, 0)
	if err != nil {
		fmt.Fprintf(stderr, "cairn export task: %s\n", reason(err))
		return 1
	}
	id := ""
	if len(args) == 1 {
		id = args[0]
	} else {
		cur, err := t.svc.TaskCurrent(t.ctx)
		if err != nil {
			fmt.Fprintf(stderr, "cairn export task: %s\n", reason(err))
			return 1
		}
		if cur.Task == nil {
			fmt.Fprintln(stderr, "cairn export task: no task is active; give the task id, as cairn export task <id>")
			return 1
		}
		id = cur.Task.ID
	}
	detail, err := t.svc.TaskGet(t.ctx, core.TaskGetRequest{ID: id})
	if err != nil {
		fmt.Fprintf(stderr, "cairn export task: %s\n", reason(err))
		return 1
	}
	events, err := allEvents(t, id)
	if err != nil {
		fmt.Fprintf(stderr, "cairn export task: %s\n", reason(err))
		return 1
	}
	renderTask(stdout, detail, events)
	return 0
}

// allEvents reads the whole journal of a task, page by page, in the order the server
// gives it: newest first. A cursor that comes back a second time ends the read with
// an error, so a server that loops cannot hang the command.
func allEvents(t *target, id string) ([]core.Event, error) {
	var out []core.Event
	cursor := ""
	seen := map[string]bool{}
	for {
		page, err := t.svc.TaskGet(t.ctx, core.TaskGetRequest{ID: id, Section: core.SectionEvents, Limit: core.MaxLimit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Events...)
		if page.Next == "" {
			return out, nil
		}
		if seen[page.Next] {
			return nil, fmt.Errorf("the journal pages do not end: the cursor %q came back", page.Next)
		}
		seen[page.Next] = true
		cursor = page.Next
	}
}

// renderTask writes the task as Markdown. The journal is written oldest first, as a
// person reads a history; the server gives it newest first. Text is written as it is
// stored, with its line breaks folded into list items, so the export is one list per
// record. Nothing in it is a command to run.
func renderTask(w io.Writer, d *core.TaskDetail, events []core.Event) {
	task := d.Task
	fmt.Fprintf(w, "# %s\n\n", oneLine(task.Title))
	fmt.Fprintf(w, "- **ID:** %s\n", task.ID)
	fmt.Fprintf(w, "- **Status:** %s\n", task.Status)
	fmt.Fprintf(w, "- **Priority:** %s\n", task.Priority)
	fmt.Fprintf(w, "- **Project:** %s\n", task.Project)
	if task.Owner != "" {
		fmt.Fprintf(w, "- **Owner:** %s\n", oneLine(task.Owner))
	}
	fmt.Fprintf(w, "- **Opened:** %s\n", stamp(task.OpenedAt))
	if task.StartedAt != nil {
		fmt.Fprintf(w, "- **Started:** %s\n", stamp(*task.StartedAt))
	}
	if task.ClosedAt != nil {
		fmt.Fprintf(w, "- **Closed:** %s\n", stamp(*task.ClosedAt))
	}

	section(w, "Goal", task.Goal)
	section(w, "Context", task.Context)
	section(w, "Constraints", task.Constraints)
	section(w, "Next action", task.NextAction)
	section(w, "Blockers", task.Blockers)
	section(w, "Outcome", task.OutcomeSummary)

	if len(d.Criteria) > 0 {
		fmt.Fprint(w, "\n## Acceptance criteria\n\n")
		for _, c := range d.Criteria {
			box := " "
			if c.Result == core.CriterionMet {
				box = "x"
			}
			line := fmt.Sprintf("- [%s] %s", box, oneLine(c.Criterion))
			if c.Result != "" {
				line += " — " + string(c.Result)
			}
			if c.Evidence != "" {
				line += ": " + oneLine(c.Evidence)
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.KeyFiles) > 0 {
		fmt.Fprint(w, "\n## Key files\n\n")
		for _, f := range d.KeyFiles {
			fmt.Fprintln(w, fileLine(f))
		}
	}
	if len(d.Steps) > 0 {
		fmt.Fprint(w, "\n## Steps\n\n")
		for _, s := range d.Steps {
			line := fmt.Sprintf("%d. [%s] %s", s.Number, s.Status, oneLine(s.Title))
			if s.Agent != "" {
				line += " (assignee: " + oneLine(s.Agent) + ")"
			}
			if s.Summary != "" {
				line += ": " + oneLine(s.Summary)
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.Lessons) > 0 || d.NoLessonsReason != "" {
		fmt.Fprint(w, "\n## Lessons\n\n")
		for _, l := range d.Lessons {
			line := fmt.Sprintf("- **%s:** %s", l.Kind, oneLine(l.Text))
			if l.Why != "" {
				line += " Why: " + oneLine(l.Why)
			}
			if l.Correction != "" {
				line += " Correction: " + oneLine(l.Correction)
			}
			fmt.Fprintln(w, line)
		}
		if d.NoLessonsReason != "" {
			fmt.Fprintf(w, "- No lessons: %s\n", oneLine(d.NoLessonsReason))
		}
	}
	if len(d.Mistakes) > 0 {
		fmt.Fprint(w, "\n## Mistakes\n\n")
		for _, m := range d.Mistakes {
			line := fmt.Sprintf("- %s (%s): %s", oneLine(m.Agent), m.Impact, oneLine(m.Mistake))
			if m.Correction != "" {
				line += " Correction: " + oneLine(m.Correction)
			}
			if m.Prevention != "" {
				line += " Prevention: " + oneLine(m.Prevention)
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.Deliverables) > 0 {
		fmt.Fprint(w, "\n## Deliverables\n\n")
		for _, f := range d.Deliverables {
			fmt.Fprintln(w, fileLine(f))
		}
	}
	if len(events) > 0 {
		fmt.Fprint(w, "\n## Journal\n\n")
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			line := fmt.Sprintf("- %s **%s**", stamp(e.OccurredAt), e.Kind)
			if e.Agent != "" {
				line += " " + oneLine(e.Agent)
			}
			line += ": " + oneLine(e.Text)
			fmt.Fprintln(w, line)
			if data := compactData(e.Data); data != "" {
				fmt.Fprintf(w, "  - data: %s\n", data)
			}
		}
	}
}

// section writes a titled block of text, when the text is not empty.
func section(w io.Writer, title, body string) {
	if body == "" {
		return
	}
	fmt.Fprintf(w, "\n## %s\n\n%s\n", title, strings.TrimSpace(body))
}

// fileLine is one key file or deliverable, with its role and note when it has them.
func fileLine(f core.TaskFile) string {
	line := "- `" + f.Path + "`"
	if f.Role != "" {
		line += " (" + string(f.Role) + ")"
	}
	if f.Note != "" {
		line += ": " + oneLine(f.Note)
	}
	return line
}

// stamp formats a time as the UTC form the journal uses, YYYY-MM-DD HH:MM UTC.
func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// oneLine folds a text's line breaks into spaces, so it fits one list item.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

// compactData writes an event's data as one line of JSON, or "" when it has none.
func compactData(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return string(data)
	}
	return buf.String()
}
