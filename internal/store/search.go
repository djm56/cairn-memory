package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// SearchFilter is one search over search_index. Query is plain text: each
// word is matched as a word, and no word is read as query syntax (CONTRACT §6).
// Kinds, when not empty, keeps only those kinds (task, event, learning,
// directive, note, lesson, mistake). Project scopes the hits to one project
// unless AllProjects is set. A directive with no project (global, or
// agent-scoped) is a hit from every project. Offset and Limit page the result;
// Limit must be at least 1.
type SearchFilter struct {
	Query       string
	Kinds       []string
	Project     string
	AllProjects bool
	Offset      int
	Limit       int
}

// ftsQuery turns plain text into an FTS5 expression. Each whitespace-separated
// word becomes a quoted phrase, with any double quote inside it doubled. The
// words are ANDed, and no word can be read as an operator, a column filter or a
// prefix. A text with no words returns "".
func ftsQuery(text string) string {
	words := strings.Fields(text)
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}

// Search returns up to f.Limit hits, best match first, and reports whether more
// follow. A query with no words returns no hits and no error. Each hit's
// Snippet is an excerpt around the matched words, with them in square brackets.
//
// Read function: call it on s, outside a transaction.
func Search(ctx context.Context, q Querier, f SearchFilter) ([]SearchRow, bool, error) {
	if f.Limit < 1 {
		return nil, false, fmt.Errorf("store: search: limit %d is below 1", f.Limit)
	}
	if f.Offset < 0 {
		return nil, false, fmt.Errorf("store: search: offset %d is negative", f.Offset)
	}
	match := ftsQuery(f.Query)
	if match == "" {
		return nil, false, nil
	}
	query := `SELECT kind, ref, project, task_id, title,
		snippet(search_index, -1, '[', ']', '…', 12)
		FROM search_index WHERE search_index MATCH ?`
	args := []any{match}
	if len(f.Kinds) > 0 {
		query += ` AND kind IN (` + placeholders(len(f.Kinds)) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if !f.AllProjects {
		// project IS NULL is the global and agent-scoped directives, which every
		// project sees (CONTRACT §9, items 10 and 11).
		query += ` AND (project = ? OR project IS NULL)`
		args = append(args, f.Project)
	}
	query += ` ORDER BY rank, rowid LIMIT ? OFFSET ?`
	args = append(args, f.Limit+1, f.Offset)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()
	var out []SearchRow
	for rows.Next() {
		var h SearchRow
		var project, taskID, title, snippet sql.NullString
		if err := rows.Scan(&h.Kind, &h.Ref, &project, &taskID, &title, &snippet); err != nil {
			return nil, false, fmt.Errorf("store: search: %w", err)
		}
		h.Project = nullString(project)
		h.TaskID = nullString(taskID)
		h.Title = nullString(title)
		h.Snippet = nullString(snippet)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: search: %w", err)
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}

// ParseChildRef splits a search hit's ref for a lesson or a mistake, which is
// "task-id/ord", into its task id and ord. It returns an error for a ref with
// no slash or a non-number ord.
func ParseChildRef(ref string) (string, int, error) {
	i := strings.LastIndex(ref, "/")
	if i < 0 {
		return "", 0, fmt.Errorf("store: ref %q has no ord", ref)
	}
	ord, err := strconv.Atoi(ref[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("store: ref %q has a bad ord: %w", ref, err)
	}
	return ref[:i], ord, nil
}
