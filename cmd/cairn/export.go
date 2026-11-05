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

// runExport is the sub-command `cairn export`: `task` (DESIGN.md §5.2) and `project`, which is placed
// in M4 (D22, docs/PLAN.md §8).
func runExport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn export: name what to export: cairn export task [id] or cairn export project")
		return 2
	}
	switch args[0] {
	case "task":
		return runExportTask(args[1:], stdout, stderr)
	case "project":
		return runExportProject(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn export: unknown export %q: use cairn export task [id] or cairn export project\n", args[0])
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
	detail, events, err := fetchTask(t, id)
	if err != nil {
		fmt.Fprintf(stderr, "cairn export task: %s\n", reason(err))
		return 1
	}
	renderTask(stdout, detail, events)
	return 0
}

// runExportProject is `cairn export project`: every task of the caller's project as Markdown on stdout,
// each written as export task writes it, with a blank line between two tasks. The server lists the tasks
// most recently updated first, and they keep that order. Open and closed tasks are all written. It takes no
// argument, and it reads only the caller's project through the server: the task list, every page of it, and
// then each task.
//
// The whole document is built before anything is written, so a failed read prints nothing. A project with no
// task prints one line that says so.
//
// Exit codes: 0 when the project was printed, 1 when a call failed, 2 for a usage error.
func runExportProject(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn export project: takes no argument, got %q\n", args)
		return 2
	}
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn export project: the working directory: %v\n", err)
		return 1
	}
	t, err := dial(wd, 0)
	if err != nil {
		fmt.Fprintf(stderr, "cairn export project: %s\n", reason(err))
		return 1
	}
	tasks, err := allTasks(t)
	if err != nil {
		fmt.Fprintf(stderr, "cairn export project: %s\n", reason(err))
		return 1
	}
	if len(tasks) == 0 {
		fmt.Fprintf(stdout, "no tasks in %s\n", foldLine(t.project.Name))
		return 0
	}
	var out bytes.Buffer
	for i, task := range tasks {
		detail, events, err := fetchTask(t, task.ID)
		if err != nil {
			fmt.Fprintf(stderr, "cairn export project: %s\n", reason(err))
			return 1
		}
		if i > 0 {
			out.WriteString("\n")
		}
		renderTask(&out, detail, events)
	}
	_, _ = stdout.Write(out.Bytes())
	return 0
}

// fetchTask reads one task as the export needs it: its detail, all of the brief, the steps and the close,
// and every page of its journal. Both exports read a task this way, so they cannot read it differently.
func fetchTask(t *target, id string) (*core.TaskDetail, []core.Event, error) {
	detail, err := t.svc.TaskGet(t.ctx, core.TaskGetRequest{ID: id})
	if err != nil {
		return nil, nil, err
	}
	events, err := allEvents(t, id)
	if err != nil {
		return nil, nil, err
	}
	return detail, events, nil
}

// allEvents reads the whole journal of a task, page by page, in the order the server
// gives it: newest first.
func allEvents(t *target, id string) ([]core.Event, error) {
	return allPages(func(cursor string) ([]core.Event, string, error) {
		page, err := t.svc.TaskGet(t.ctx, core.TaskGetRequest{ID: id, Section: core.SectionEvents, Limit: core.MaxLimit, Cursor: cursor})
		if err != nil {
			return nil, "", err
		}
		return page.Events, page.Next, nil
	})
}

// allTasks reads the caller's task list, every page of it, most recently updated first. Every status is
// listed.
func allTasks(t *target) ([]core.TaskSummary, error) {
	return allPages(func(cursor string) ([]core.TaskSummary, string, error) {
		list, err := t.svc.TaskList(t.ctx, core.TaskListRequest{Limit: core.MaxLimit, Cursor: cursor})
		if err != nil {
			return nil, "", err
		}
		return list.Tasks, list.Next, nil
	})
}

// allPages reads a paged list to its end. fetch returns one page and the cursor of the next, and an empty
// cursor ends the list. A cursor that comes back a second time ends the read with an error, so a server
// that loops cannot hang the command.
func allPages[T any](fetch func(cursor string) ([]T, string, error)) ([]T, error) {
	var out []T
	cursor := ""
	seen := map[string]bool{}
	for {
		items, next, err := fetch(cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == "" {
			return out, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("the pages do not end: the cursor %q came back", next)
		}
		seen[next] = true
		cursor = next
	}
}

// renderTask writes the task as Markdown. The journal is written oldest first, as a
// person reads a history; the server gives it newest first. A single-line field is folded
// to one line by foldLine, so a title or a note cannot start a heading or a list item of
// its own, and no control character reaches the document. A section body keeps its own
// line breaks and Markdown, and loses its control characters, as blockText does. Nothing
// in it is a command to run.
func renderTask(w io.Writer, d *core.TaskDetail, events []core.Event) {
	task := d.Task
	fmt.Fprintf(w, "# %s\n\n", foldLine(task.Title))
	fmt.Fprintf(w, "- **ID:** %s\n", foldLine(task.ID))
	fmt.Fprintf(w, "- **Status:** %s\n", foldLine(string(task.Status)))
	fmt.Fprintf(w, "- **Priority:** %s\n", foldLine(string(task.Priority)))
	fmt.Fprintf(w, "- **Project:** %s\n", foldLine(task.Project))
	if task.Owner != "" {
		fmt.Fprintf(w, "- **Owner:** %s\n", foldLine(task.Owner))
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
			line := fmt.Sprintf("- [%s] %s", box, foldLine(c.Criterion))
			if c.Result != "" {
				line += " — " + string(c.Result)
			}
			if c.Evidence != "" {
				line += ": " + foldLine(c.Evidence)
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
			line := fmt.Sprintf("%d. [%s] %s", s.Number, foldLine(string(s.Status)), foldLine(s.Title))
			if s.Agent != "" {
				line += " (assignee: " + foldLine(s.Agent) + ")"
			}
			if s.Summary != "" {
				line += ": " + foldLine(s.Summary)
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.Lessons) > 0 || d.NoLessonsReason != "" {
		fmt.Fprint(w, "\n## Lessons\n\n")
		for _, l := range d.Lessons {
			line := fmt.Sprintf("- **%s:** %s", foldLine(string(l.Kind)), foldLine(l.Text))
			if l.Why != "" {
				line += " Why: " + foldLine(l.Why)
			}
			if l.Correction != "" {
				line += " Correction: " + foldLine(l.Correction)
			}
			fmt.Fprintln(w, line)
		}
		if d.NoLessonsReason != "" {
			fmt.Fprintf(w, "- No lessons: %s\n", foldLine(d.NoLessonsReason))
		}
	}
	if len(d.Mistakes) > 0 {
		fmt.Fprint(w, "\n## Mistakes\n\n")
		for _, m := range d.Mistakes {
			line := fmt.Sprintf("- %s (%s): %s", foldLine(m.Agent), foldLine(string(m.Impact)), foldLine(m.Mistake))
			if m.Correction != "" {
				line += " Correction: " + foldLine(m.Correction)
			}
			if m.Prevention != "" {
				line += " Prevention: " + foldLine(m.Prevention)
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
			line := fmt.Sprintf("- %s **%s**", stamp(e.OccurredAt), foldLine(string(e.Kind)))
			if e.Agent != "" {
				line += " " + foldLine(e.Agent)
			}
			line += ": " + foldLine(e.Text)
			fmt.Fprintln(w, line)
			if data := compactData(e.Data); data != "" {
				fmt.Fprintf(w, "  - data: %s\n", data)
			}
		}
	}
}

// section writes a titled block of text, when the text is not empty. Its body keeps
// its line breaks, since a goal or a context is a paragraph, and loses its control
// characters (blockText).
func section(w io.Writer, title, body string) {
	if body == "" {
		return
	}
	fmt.Fprintf(w, "\n## %s\n\n%s\n", title, strings.TrimSpace(blockText(body)))
}

// fileLine is one key file or deliverable, with its role and note when it has them.
// The path is one line without control characters in a code span
// that a backtick in the path cannot end.
func fileLine(f core.TaskFile) string {
	line := "- " + codeSpan(foldLine(f.Path))
	if f.Role != "" {
		line += " (" + foldLine(string(f.Role)) + ")"
	}
	if f.Note != "" {
		line += ": " + foldLine(f.Note)
	}
	return line
}

// codeSpan returns s as a Markdown code span. The fence is one backtick longer than the
// longest run of backticks in s, so no run in s can close the span, and a space pads
// each side when s begins or ends with a backtick, which the fence would otherwise take
// as its own (CommonMark, section 6.1).
func codeSpan(s string) string {
	longest, ticks := 0, 0
	for _, r := range s {
		if r == '`' {
			ticks++
			longest = max(longest, ticks)
			continue
		}
		ticks = 0
	}
	fence := strings.Repeat("`", longest+1)
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	return fence + pad + s + pad + fence
}

// stamp formats a time as the UTC form the journal uses, YYYY-MM-DD HH:MM UTC.
func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// compactData writes an event's data as one line of JSON, or "" when it has none.
func compactData(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return foldLine(string(data))
	}
	// Valid JSON holds no raw control character, but a line separator may sit in a
	// string, so the line is folded as well.
	return foldLine(buf.String())
}
