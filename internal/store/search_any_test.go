package store

import (
	"context"
	"errors"
	"testing"
)

// TestSearchAny checks the OR form of the match: a row with one of the words is
// a hit, where the default AND form needs every word. Both forms quote the
// words, so punctuation is still text.
func TestSearchAny(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	insertTask(t, s, TaskRow{ID: "task-20261028-01", Project: "acme-api", Title: "alpha beta",
		Goal: "goal", Status: "open", Priority: "normal", OpenedAt: t0, UpdatedAt: t0})

	and, _, err := Search(ctx, s, SearchFilter{Query: "alpha gamma", Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatalf("AND search: %v", err)
	}
	if len(and) != 0 {
		t.Errorf("AND search of alpha gamma found %d rows, want none", len(and))
	}
	or, _, err := Search(ctx, s, SearchFilter{Query: "alpha gamma", Project: "acme-api", Any: true, Limit: 10})
	if err != nil {
		t.Fatalf("OR search: %v", err)
	}
	if len(or) != 1 || or[0].Kind != "task" {
		t.Errorf("OR search of alpha gamma = %+v, want the one task", or)
	}
	// Punctuation stays text under OR too: no error, and no hit.
	if _, _, err := Search(ctx, s, SearchFilter{Query: `"alpha OR (`, Project: "acme-api", Any: true, Limit: 10}); err != nil {
		t.Errorf("OR search with punctuation: %v", err)
	}
}

// TestGetDirectiveByID checks that a directive is found by row id, whatever its
// status, and that an unknown row id is ErrNotFound.
func TestGetDirectiveByID(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	var id int64
	err := s.WithTx(ctx, func(q Querier) error {
		code, err := NextDirectiveCode(ctx, q)
		if err != nil {
			return err
		}
		id, err = InsertDirective(ctx, q, DirectiveRow{Code: code, Scope: "global", Title: "rule",
			Rule: "a rule", Status: "retired", RatifiedBy: "operator", RatifiedAt: &t0, RetiredAt: &t0})
		return err
	})
	if err != nil {
		t.Fatalf("insert directive: %v", err)
	}
	got, err := GetDirectiveByID(ctx, s, id)
	if err != nil {
		t.Fatalf("GetDirectiveByID(%d): %v", id, err)
	}
	if got.Code != "D-0001" || got.Status != "retired" {
		t.Errorf("directive = %+v, want D-0001 retired", got)
	}
	if _, err := GetDirectiveByID(ctx, s, id+100); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDirectiveByID of an unknown row: %v, want ErrNotFound", err)
	}
}
