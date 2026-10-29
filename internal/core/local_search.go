package core

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/djm56/cairn-memory/internal/store"
)

// validHitKind reports whether k is one of the seven kinds search covers.
func validHitKind(k HitKind) bool {
	switch k {
	case HitTask, HitEvent, HitLearning, HitDirective, HitNote, HitLesson, HitMistake:
		return true
	}
	return false
}

// searchHitFromRow converts one search row to a hit. Most hits name their row
// by the ref the index holds. A directive hit is the exception: the index holds
// its row id, and the hit names it by code, so the caller looks the code up.
func searchHitFromRow(row store.SearchRow) SearchHit {
	return SearchHit{
		Kind: HitKind(row.Kind), ID: row.Ref, Project: row.Project,
		TaskID: row.TaskID, Title: row.Title, Snippet: row.Snippet,
	}
}

// Search finds rows of every kind that match a plain-text query, best match
// first. The query is text: each word must appear, and no word is read as
// syntax (CONTRACT §6). Only the caller's project is searched unless
// AllProjects is set. Global and agent-scoped directives are found from every
// project.
func (l *Local) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if blank(req.Query) {
		return nil, Invalid("a search needs a query")
	}
	kinds := make([]string, 0, len(req.Kinds))
	for _, k := range req.Kinds {
		if !validHitKind(k) {
			return nil, Invalid("unknown kind %q: use task, event, learning, directive, note, lesson or mistake", k)
		}
		kinds = append(kinds, string(k))
	}
	size, err := pageSize(req.Limit, 10)
	if err != nil {
		return nil, err
	}
	offset, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	var from int64
	if offset != nil {
		from = *offset
	}
	if err := l.ensureReadProject(ctx, c.Project); err != nil {
		return nil, err
	}
	rows, more, err := store.Search(ctx, l.st, store.SearchFilter{
		Query: req.Query, Kinds: kinds, Project: c.Project, AllProjects: req.AllProjects,
		Offset: int(from), Limit: size,
	})
	if err != nil {
		return nil, err
	}
	out := &SearchResult{Hits: []SearchHit{}}
	for _, row := range rows {
		hit := searchHitFromRow(row)
		if row.Kind == string(HitDirective) {
			code, err := directiveCodeOf(ctx, l.st, row.Ref)
			if err != nil {
				return nil, err
			}
			hit.ID = code
		}
		out.Hits = append(out.Hits, hit)
	}
	if more {
		out.Next = encodeCursor(from + int64(size))
	}
	return out, nil
}

// directiveCodeOf returns the code of the directive whose row id is ref, the
// form a search row holds it in.
func directiveCodeOf(ctx context.Context, q store.Querier, ref string) (string, error) {
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return "", err
	}
	d, err := store.GetDirectiveByID(ctx, q, id)
	if err != nil {
		return "", err
	}
	return d.Code, nil
}

// recallStopWords are common words that do not say what a goal is about. recall
// leaves them out of its match, so "the" or "with" cannot pull in every row.
var recallStopWords = map[string]bool{
	"about": true, "after": true, "again": true, "against": true, "could": true,
	"from": true, "have": true, "into": true, "make": true, "need": true,
	"should": true, "some": true, "than": true, "that": true, "their": true,
	"then": true, "there": true, "these": true, "they": true, "this": true,
	"those": true, "through": true, "want": true, "were": true, "what": true,
	"when": true, "where": true, "which": true, "while": true, "will": true,
	"with": true, "would": true, "your": true,
}

// goalWords returns the distinct words of goal that can name its subject: the
// words of four or more letters and digits that are not stop words, lowercased,
// in the order they appear. recall matches a row that holds any of them. The
// words are the match's whole input, so no punctuation reaches the query.
func goalWords(goal string) []string {
	seen := map[string]bool{}
	var out []string
	fields := strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range fields {
		if utf8.RuneCountInString(w) < 4 || recallStopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// Recall answers what a caller should know before a goal. Directives are those
// that apply to the caller's project, narrowed by agent as directive_list does.
// Lessons, mistakes and tasks come from every project, best match to the goal
// first, at most limit of each (default 5). A goal with no word that can match
// (see goalWords) gives directives only.
func (l *Local) Recall(ctx context.Context, req RecallRequest) (*RecallResult, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if blank(req.Goal) {
		return nil, Invalid("recall needs a goal")
	}
	size, err := pageSize(req.Limit, 5)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c.Project); err != nil {
		return nil, err
	}
	out := &RecallResult{Lessons: []LessonHit{}, Mistakes: []MistakeHit{}, Tasks: []TaskSummary{}}
	if out.Directives, err = directivesFor(ctx, l.st, c.Project, req.Agent); err != nil {
		return nil, err
	}
	words := goalWords(req.Goal)
	if len(words) == 0 {
		return out, nil
	}
	query := strings.Join(words, " ")
	if err := recallLessons(ctx, l.st, query, size, out); err != nil {
		return nil, err
	}
	if err := recallMistakes(ctx, l.st, query, size, out); err != nil {
		return nil, err
	}
	if err := recallTasks(ctx, l.st, query, size, out); err != nil {
		return nil, err
	}
	return out, nil
}

// searchAll runs one kind's search over every project, matching any word, and
// returns the rows best first.
func searchAll(ctx context.Context, q store.Querier, query string, kind HitKind, size int) ([]store.SearchRow, error) {
	rows, _, err := store.Search(ctx, q, store.SearchFilter{
		Query: query, Kinds: []string{string(kind)}, AllProjects: true, Any: true, Limit: size,
	})
	return rows, err
}

// recallLessons adds the best lessons of every project to out.
func recallLessons(ctx context.Context, q store.Querier, query string, size int, out *RecallResult) error {
	rows, err := searchAll(ctx, q, query, HitLesson, size)
	if err != nil {
		return err
	}
	for _, row := range rows {
		taskID, ord, err := store.ParseChildRef(row.Ref)
		if err != nil {
			return err
		}
		les, err := store.GetLesson(ctx, q, taskID, ord)
		if err != nil {
			return err
		}
		out.Lessons = append(out.Lessons, LessonHit{
			TaskID: taskID, Project: row.Project,
			Lesson: lessonsFromRows([]store.LessonRow{les})[0],
		})
	}
	return nil
}

// recallMistakes adds the best mistakes of every project to out.
func recallMistakes(ctx context.Context, q store.Querier, query string, size int, out *RecallResult) error {
	rows, err := searchAll(ctx, q, query, HitMistake, size)
	if err != nil {
		return err
	}
	for _, row := range rows {
		taskID, ord, err := store.ParseChildRef(row.Ref)
		if err != nil {
			return err
		}
		mis, err := store.GetMistake(ctx, q, taskID, ord)
		if err != nil {
			return err
		}
		out.Mistakes = append(out.Mistakes, MistakeHit{
			TaskID: taskID, Project: row.Project,
			Mistake: mistakesFromRows([]store.MistakeRow{mis})[0],
		})
	}
	return nil
}

// recallTasks adds the best tasks of every project to out.
func recallTasks(ctx context.Context, q store.Querier, query string, size int, out *RecallResult) error {
	rows, err := searchAll(ctx, q, query, HitTask, size)
	if err != nil {
		return err
	}
	for _, row := range rows {
		task, err := store.GetTaskAnyProject(ctx, q, row.Ref)
		if err != nil {
			return err
		}
		out.Tasks = append(out.Tasks, taskSummaryFromRow(task))
	}
	return nil
}
