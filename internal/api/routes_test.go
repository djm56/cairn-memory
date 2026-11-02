package api

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestEveryRouteConstHasAMark fails when a route constant has no read or write mark.
// It reads the constants of api.go, so a route added as a constant is checked before
// Handler mounts it. A route mounted without a constant panics at Handler instead,
// since accessOf finds no mark for it.
func TestEveryRouteConstHasAMark(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "api.go", nil, 0)
	if err != nil {
		t.Fatalf("parse api.go: %v", err)
	}
	found := 0
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Route") {
					continue
				}
				found++
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s is not a string literal", name.Name)
				}
				path, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", name.Name, err)
				}
				if a, marked := routeAccess[path]; !marked || a == accessUnmarked {
					t.Errorf("%s (%s) has no read or write mark: add it to routeAccess", name.Name, path)
				}
			}
		}
	}
	if found < 19 {
		t.Fatalf("found %d route constants, want at least 19: the check would pass for nothing", found)
	}
}

// staticLookup answers every hash with one agent key, so the mount test can reach
// each route with a key that passes.
type staticLookup struct{}

func (staticLookup) FindActive(context.Context, string) (auth.Key, error) {
	return auth.Key{ID: 1, Name: "all", Scope: auth.ScopeAgent}, nil
}

func (staticLookup) Touch(context.Context, int64, time.Time) error { return nil }

// TestEveryMarkedRouteIsMounted checks that each marked route answers a call. A
// mount that is missing from Handler answers 404 and is caught here.
func TestEveryMarkedRouteIsMounted(t *testing.T) {
	h := Handler(core.Unimplemented{}, KeyCaller(auth.NewResolver(staticLookup{}, nil)), nil)
	for route := range routeAccess {
		req := httptest.NewRequest(http.MethodPost, Prefix+route, strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+"cairn_"+strings.Repeat("A", 43))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is marked but not mounted: %s", route, rec.Body.String())
		}
	}
}

// TestAccessOfPanicsOnAnUnmarkedRoute checks the mount guard: a route with no mark
// panics at Handler rather than being served without one.
func TestAccessOfPanicsOnAnUnmarkedRoute(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("accessOf of an unmarked route did not panic")
		}
	}()
	accessOf("/agent/no_such_route")
}
