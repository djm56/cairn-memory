package core_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The reach guard (step 8 review, W1). The M1 promises that the CLI and the MCP
// adapter never open a database and never listen (DESIGN.md §5.2, §5.3; D11, D19)
// are checked here by reading the source, not by a grep run once. The guarded
// packages link core and store, since the client imports core, so an import graph
// cannot tell a call from a link. The guard reads the calls instead. It resolves
// each package name through the file's imports, so an aliased import is still
// seen. It is in package core_test because it reuses the parsing of errortext_test.go.

// guardedDirs are the packages that reach a server and nothing else.
var guardedDirs = []string{"../../cmd/cairn", "../mcp", "../client"}

// repoRoot is the module root, which the header-caller check walks.
const repoRoot = "../.."

// Import paths of the packages the guard names.
const (
	storePath   = "github.com/djm56/cairn-memory/internal/store"
	corePath    = "github.com/djm56/cairn-memory/internal/core"
	netPath     = "net"
	netHTTPPath = "net/http"
)

// banned is one call or literal a guarded package must not contain. A path of ""
// matches the name on any receiver, as ListenAndServe does: it is a method of
// http.Server as well as a function. A name matches by prefix, so ListenAndServeTLS
// is caught with ListenAndServe.
type banned struct {
	path    string
	name    string
	literal bool // a composite literal of the type, such as &http.Server{...}
	why     string
}

// bannedCalls is the list the guard enforces.
var bannedCalls = []banned{
	{path: storePath, name: "Open", why: "opens a database"},
	{path: corePath, name: "NewLocal", why: "builds a local Service over a database"},
	{path: netPath, name: "Listen", why: "listens on a socket"},
	{path: "", name: "ListenAndServe", why: "starts an HTTP listener"},
	{path: netHTTPPath, name: "Server", literal: true, why: "configures an HTTP listener"},
}

// reachFaults returns one description for each banned call or literal in f, with
// its position. An empty result means the file is clean.
func reachFaults(fset *token.FileSet, f *ast.File) []string {
	names := importNames(f)
	var out []string
	report := func(n ast.Node, what string) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), what))
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			for _, b := range bannedCalls {
				if b.literal || !strings.HasPrefix(n.Sel.Name, b.name) {
					continue
				}
				if b.path == "" {
					report(n, fmt.Sprintf("a call to %s %s", n.Sel.Name, b.why))
					continue
				}
				if x, ok := n.X.(*ast.Ident); ok && names[x.Name] == b.path && n.Sel.Name == b.name {
					report(n, fmt.Sprintf("%s.%s %s", path.Base(b.path), b.name, b.why))
				}
			}
		case *ast.CompositeLit:
			sel, ok := n.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			for _, b := range bannedCalls {
				if b.literal && names[x.Name] == b.path && sel.Sel.Name == b.name {
					report(n, fmt.Sprintf("a %s.%s literal %s", path.Base(b.path), b.name, b.why))
				}
			}
		}
		return true
	})
	return out
}

// headerCallerUses returns each reference in f to UnauthenticatedHeaderCaller
// other than its own declaration. The function is for in-process tests only (its
// doc comment and CONTRACT §1.1), so no non-test code may name it.
func headerCallerUses(fset *token.FileSet, f *ast.File) []string {
	const name = "UnauthenticatedHeaderCaller"
	declared := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Recv == nil {
			declared[fd.Name] = true
		}
		return true
	})
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name && !declared[id] {
			out = append(out, fmt.Sprintf("%s: a non-test use of %s", fset.Position(id.Pos()), name))
		}
		return true
	})
	return out
}

// importNames maps each name a file gives its imports to the import path. A
// file's package is named by its last path element unless it is renamed.
func importNames(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = p
	}
	return out
}

// nonTestGoFiles lists the non-test Go files of dir, failing when there are none.
// Glob returns nothing for a directory that is gone, so a renamed package would
// otherwise drop out of the guard without a word.
func nonTestGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			out = append(out, file)
		}
	}
	if len(out) == 0 {
		t.Fatalf("the reach guard lists %s, which has no non-test Go files: update guardedDirs", dir)
	}
	return out
}

// parseFile parses one Go file for the guard.
func parseFile(t *testing.T, fset *token.FileSet, file string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	return f
}

// TestGuardedPackagesNeverOpenStoreOrListen is W1. The CLI, the MCP adapter and
// the client make no call that opens a store or starts a listener, in any of their
// non-test files, including the ones added later.
func TestGuardedPackagesNeverOpenStoreOrListen(t *testing.T) {
	for _, dir := range guardedDirs {
		for _, file := range nonTestGoFiles(t, dir) {
			fset := token.NewFileSet()
			for _, fault := range reachFaults(fset, parseFile(t, fset, file)) {
				t.Errorf("%s: the guarded package must not reach a store or a listener", fault)
			}
		}
	}
}

// TestNoNonTestUseOfUnauthenticatedHeaderCaller is W1's second half. The
// unauthenticated caller trusts the role header, so it is a test helper, and no
// non-test file in the repository may use it. The walk covers the whole module.
func TestNoNonTestUseOfUnauthenticatedHeaderCaller(t *testing.T) {
	var files int
	err := filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != repoRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		files++
		fset := token.NewFileSet()
		for _, use := range headerCallerUses(fset, parseFile(t, fset, p)) {
			t.Errorf("%s", use)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The walk must reach the module, not an empty tree.
	if files < 20 {
		t.Fatalf("the header-caller check read %d non-test files, want at least 20", files)
	}
}

// TestReachGuardCanFail proves the guard can fail. Each snippet holds one banned
// call, or none, and the guard must answer with exactly the count given. A guard
// that always answers zero would pass the test above and check nothing.
func TestReachGuardCanFail(t *testing.T) {
	const pkg = "package p\n"
	storeImp := `import store "` + storePath + `"` + "\n"
	coreImp := `import "` + corePath + `"` + "\n"
	cases := []struct {
		name   string
		src    string
		faults int
	}{
		{"store.Open", pkg + `import "` + storePath + `"` + "\nfunc f() { store.Open(\"x\") }\n", 1},
		{"aliased store import", pkg + storeImp + "func f() { store.Open(\"x\") }\n", 1},
		{"core.NewLocal", pkg + coreImp + "func f() { core.NewLocal(nil) }\n", 1},
		{"net.Listen", pkg + `import "net"` + "\nfunc f() { net.Listen(\"tcp\", \"127.0.0.1:0\") }\n", 1},
		{"http.ListenAndServe", pkg + `import "net/http"` + "\nfunc f() { http.ListenAndServe(\":80\", nil) }\n", 1},
		{"a server's ListenAndServe", pkg + `import "net/http"` + "\nfunc f(s *http.Server) { s.ListenAndServe() }\n", 1},
		{"ListenAndServeTLS", pkg + `import "net/http"` + "\nfunc f(s *http.Server) { s.ListenAndServeTLS(\"c\", \"k\") }\n", 1},
		{"http.Server literal", pkg + `import "net/http"` + "\nvar s = &http.Server{Addr: \":80\"}\n", 1},
		{"clean calls", pkg + coreImp + `import "net/http"` + "\nfunc f() { core.ValidProjectName(\"x\"); _ = http.StatusOK }\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", c.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			got := reachFaults(fset, f)
			if len(got) != c.faults {
				t.Fatalf("reachFaults = %d faults %q, want %d", len(got), got, c.faults)
			}
		})
	}

	headerCases := []struct {
		name   string
		src    string
		faults int
	}{
		{"a use of UnauthenticatedHeaderCaller", pkg + `import "x/api"` + "\nvar h = api.UnauthenticatedHeaderCaller\n", 1},
		{"its declaration alone", pkg + "func UnauthenticatedHeaderCaller() {}\n", 0},
	}
	for _, c := range headerCases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", c.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			if got := headerCallerUses(fset, f); len(got) != c.faults {
				t.Fatalf("headerCallerUses = %d uses %q, want %d", len(got), got, c.faults)
			}
		})
	}
}
