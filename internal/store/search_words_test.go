package store

import (
	"context"
	"testing"
)

// TestSearchIgnoresWordsWithoutLetters checks that a word with no letter or
// digit does not empty a search, in either form. "rate - limit" must still find
// the task that holds rate and limit, by AND (the default) and by OR (Any). The
// two forms must agree here, because a punctuation word can match nothing and
// the other words still decide the hit (CONTRACT §6: punctuation is never an
// error).
func TestSearchIgnoresWordsWithoutLetters(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	insertTask(t, s, TaskRow{ID: "task-20261028-01", Project: "acme-api", Title: "zeppelin rate limit",
		Goal: "goal", Status: "open", Priority: "normal", OpenedAt: t0, UpdatedAt: t0})

	for _, anyWord := range []bool{false, true} {
		for _, query := range []string{"rate - limit", "zeppelin -", "( zeppelin ) &"} {
			hits, _, err := Search(ctx, s, SearchFilter{Query: query, Project: "acme-api", Any: anyWord, Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q, Any=%v): %v", query, anyWord, err)
			}
			if len(hits) != 1 || hits[0].Kind != "task" {
				t.Errorf("Search(%q, Any=%v) = %+v, want the one task", query, anyWord, hits)
			}
		}
	}
	// A query of punctuation alone has no word, so it finds nothing and fails
	// nothing, in either form.
	for _, anyWord := range []bool{false, true} {
		none, _, err := Search(ctx, s, SearchFilter{Query: "- ( &", Project: "acme-api", Any: anyWord, Limit: 10})
		if err != nil || len(none) != 0 {
			t.Errorf("Search of punctuation alone (Any=%v) = %+v, %v; want no hits and no error", anyWord, none, err)
		}
	}
}
