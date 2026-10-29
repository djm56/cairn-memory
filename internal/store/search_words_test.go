package store

import (
	"context"
	"testing"
)

// TestSearchIgnoresWordsWithoutLetters checks that a word with no letter or
// digit does not empty an AND search. "rate - limit" must still find the task
// that holds rate and limit (CONTRACT §6: punctuation is never an error).
func TestSearchIgnoresWordsWithoutLetters(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	insertTask(t, s, TaskRow{ID: "task-20261028-01", Project: "acme-api", Title: "zeppelin rate limit",
		Goal: "goal", Status: "open", Priority: "normal", OpenedAt: t0, UpdatedAt: t0})

	for _, query := range []string{"rate - limit", "zeppelin -", "( zeppelin ) &"} {
		hits, _, err := Search(ctx, s, SearchFilter{Query: query, Project: "acme-api", Limit: 10})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if len(hits) != 1 || hits[0].Kind != "task" {
			t.Errorf("Search(%q) = %+v, want the one task", query, hits)
		}
	}
	// A query of punctuation alone has no word, so it finds nothing and fails
	// nothing.
	none, _, err := Search(ctx, s, SearchFilter{Query: "- ( &", Project: "acme-api", Limit: 10})
	if err != nil || len(none) != 0 {
		t.Errorf("Search of punctuation alone = %+v, %v; want no hits and no error", none, err)
	}
}
