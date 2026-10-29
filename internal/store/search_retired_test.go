package store

import (
	"context"
	"testing"
)

// TestSearchHidesRetiredDirectives checks CONTRACT §6: a retired
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
	// The filter sits in the query, so it holds across projects and with no kind filter.
	all, _, err := Search(ctx, s, SearchFilter{Query: "limit", AllProjects: true, Limit: 10})
	if err != nil {
		t.Fatalf("Search all projects: %v", err)
	}
	if len(all) != 1 || all[0].Title != "Limit requests" {
		t.Errorf("all-projects hits = %+v, want only the active directive", all)
	}
	anyKind, _, err := Search(ctx, s, SearchFilter{Query: "limit", Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatalf("Search no kinds: %v", err)
	}
	if len(anyKind) != 1 || anyKind[0].Title != "Limit requests" {
		t.Errorf("no-kind hits = %+v, want only the active directive", anyKind)
	}
}
