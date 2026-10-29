package store

import (
	"context"
	"testing"
)

// TestSearchHidesRetiredDirectives checks CONTRACT §6 (ruling 4a): a retired
// directive is not a search hit, as it is not listed by directive_list. Both
// directives hold the word "limit"; only the active one is found.
func TestSearchHidesRetiredDirectives(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	err := s.WithTx(ctx, func(q Querier) error {
		for _, d := range []DirectiveRow{
			{Scope: "global", Title: "Limit requests", Rule: "Rate limit every endpoint.", Status: "active",
				RatifiedBy: "operator", RatifiedAt: &t0},
			{Scope: "global", Title: "Limit retries", Rule: "Limit retries to three.", Status: "retired",
				RatifiedBy: "operator", RatifiedAt: &t0, RetiredAt: &t0},
		} {
			code, err := NextDirectiveCode(ctx, q)
			if err != nil {
				return err
			}
			d.Code = code
			if _, err := InsertDirective(ctx, q, d); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("insert directives: %v", err)
	}

	hits, _, err := Search(ctx, s, SearchFilter{Query: "limit", Kinds: []string{"directive"}, Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Limit requests" {
		t.Errorf("directive hits = %+v, want only the active directive", hits)
	}
}
