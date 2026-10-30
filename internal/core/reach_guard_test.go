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

// The reach guard (step 8 review W1, step 10 review W1). The M1 promises that the
// CLI, the MCP adapter and the client never open a database and never listen
// (DESIGN.md §5.2, §5.3; D11, D19). That is checked here by reading the source, not
// by a grep run once. The guarded packages link core and store, since the client
// imports core, so an import graph cannot tell a call from a link. The guard reads
// the calls instead.
//
// The guard is a denylist, and it says so: it names the forms of a listener it knows
// (see listenerName and the rules in reachFaults). It does not prove that no listener
// exists. What it does prove is that a new package is guarded by default (guardedDirs
// derives the set from the module, and a package must be exempted by name, with a
// reason), and that each form it names is caught by a snippet that fails without it
// (TestReachGuardCanFail).

const (
	storePath    = "github.com/djm56/cairn-memory/internal/store"
	corePath     = "github.com/djm56/cairn-memory/internal/core"
	netPath      = "net"
	netHTTPPath  = "net/http"
	tlsPath      = "crypto/tls"
	httptestPath = "net/http/httptest"
	// repoRoot is the module root, which the directory walks start from.
	repoRoot = "../.."
)

// serverSide are the packages the guard does not check, each with the reason. Every
// other package in the module is guarded, so a new package is checked from its first
// commit. A name here that is not a package of the module fails the test.
var serverSide = map[string]string{
	"internal/store": "owns the database; it is the one place a store is opened",
	"internal/core":  "the in-process Service over a store; it imports internal/store",
	"internal/api":   "HTTP handlers only; the shipped binary starts no listener (D19). M2's cairn serve is in cmd/cairn, which is guarded",
}

// requiredGuarded are packages that must stay in the guard. Losing one of them to an
// exemption, or to a rename, fails the test and names it.
var requiredGuarded = []string{"cmd/cairn", "internal/mcp", "internal/client", "internal/identity"}

// guardedDirs derives the packages the guard checks: every directory under the module
// with a non-test Go file, except the serverSide ones. It fails when a serverSide name
// is no longer a package, or when a required package is not guarded, and says which.
func guardedDirs(t *testing.T) []string {
	t.Helper()
	pkgs := map[string]bool{}
	err := filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != repoRoot && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			rel, err := filepath.Rel(repoRoot, filepath.Dir(p))
			if err != nil {
				return err
			}
			pkgs[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, why := range serverSide {
		if !pkgs[name] {
			t.Fatalf("the reach guard exempts %s (%s), which has no non-test Go files: update serverSide", name, why)
		}
	}
	var out []string
	for name := range pkgs {
		if _, exempt := serverSide[name]; !exempt {
			out = append(out, name)
		}
	}
	for _, name := range requiredGuarded {
		if !contains(out, name) {
			t.Fatalf("the reach guard does not check %s: it must, since it is client-side", name)
		}
	}
	return out
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// bannedImports are packages a guarded non-test file must not import at all, with the
// reason. Importing one is a fault on its own, since it is the way to reach the call.
var bannedImports = map[string]string{
	storePath:    "opens a database",
	httptestPath: "starts an in-process HTTP listener",
}

// listenerPkgs are the standard packages whose functions and types start or configure
// a listener. A name from one of them is checked by listenerName or by the rules below.
var listenerPkgs = map[string]bool{netPath: true, netHTTPPath: true, tlsPath: true}

// listenerName reports whether a selector qualified by a listener package (net,
// net/http, crypto/tls) starts a listener: a name that starts with Listen, or Serve,
// ServeTLS or FileListener. A type such as net.ListenConfig or net.Listener counts too:
// a guarded package has no use for either, and a listener's configuration is the first
// step to one. ServeHTTP does not count: it hands a request to a handler and opens
// nothing, and neither does ServeFile.
func listenerName(name string) bool {
	return strings.HasPrefix(name, "Listen") || name == "Serve" || name == "ServeTLS" || name == "FileListener"
}

// listenerMethod reports whether a selector with any receiver is a listener's method:
// the exact names of the functions and methods that open a socket or serve one. It is
// exact, not a prefix, so a field or type called Listener on an unrelated value is not
// caught. The package-qualified rule (listenerName) covers the functions by package.
func listenerMethod(name string) bool {
	switch name {
	case "Listen", "ListenPacket", "ListenAndServe", "ListenAndServeTLS", "Serve", "ServeTLS", "FileListener":
		return true
	}
	return false
}

// reachFaults returns one description for each banned import, call or reference in f,
// with its position. An empty result means the file is clean. The rules are:
//   - a banned import (bannedImports);
//   - a dot import, in any file: a name it brings in cannot be resolved to its package
//     by reading the call, so it is refused;
//   - a listener function or method (listenerName for a listener package; listenerMethod
//     for any receiver, so it covers a method value, an embedded *http.Server, and a value
//     of any type with the method);
//   - a reference to the net/http Server type (a literal, a var, new(http.Server), a
//     field), since a Server is the configuration of a listener;
//   - store.Open and core.NewLocal, which open a store or build a Service over one.
func reachFaults(fset *token.FileSet, f *ast.File) []string {
	names := importNames(f)
	var out []string
	report := func(n ast.Node, what string) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), what))
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if why, banned := bannedImports[p]; banned {
			report(imp, fmt.Sprintf("imports %s, which %s", p, why))
		}
		if imp.Name != nil && imp.Name.Name == "." {
			report(imp, fmt.Sprintf("a dot import of %s hides the package of its calls: import it by name", p))
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, isPkg := sel.X.(*ast.Ident)
		pkg := ""
		if isPkg {
			pkg = names[x.Name]
		}
		switch {
		case pkg == storePath && sel.Sel.Name == "Open":
			report(sel, "store.Open opens a database")
		case pkg == corePath && sel.Sel.Name == "NewLocal":
			report(sel, "core.NewLocal builds a local Service over a database")
		case pkg == netHTTPPath && sel.Sel.Name == "Server":
			report(sel, "http.Server configures an HTTP listener")
		case pkg == tlsPath && sel.Sel.Name == "NewListener":
			report(sel, "tls.NewListener wraps a listener")
		case listenerPkgs[pkg] && listenerName(sel.Sel.Name):
			report(sel, fmt.Sprintf("%s.%s starts a listener", path.Base(pkg), sel.Sel.Name))
		case !listenerPkgs[pkg] && listenerMethod(sel.Sel.Name):
			report(sel, fmt.Sprintf("a call to .%s starts a listener", sel.Sel.Name))
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
// file's package is named by its last path element unless it is renamed. A dot import
// has no name and is left out: reachFaults refuses it.
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
		if name != "." {
			out[name] = p
		}
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
		t.Fatalf("the reach guard lists %s, which has no non-test Go files", dir)
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

// TestGuardedPackagesNeverOpenStoreOrListen is W1. Every package the guard derives,
// the CLI, the MCP adapter, the client and identity among them, makes no call that
// opens a store or starts a listener, and imports no store or test server, in any of
// its non-test files, including the ones added later.
func TestGuardedPackagesNeverOpenStoreOrListen(t *testing.T) {
	for _, dir := range guardedDirs(t) {
		for _, file := range nonTestGoFiles(t, filepath.Join(repoRoot, dir)) {
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

// TestReachGuardCanFail proves the guard can fail. Each snippet holds the banned forms
// and the count the guard must answer with. A guard that always answers zero would
// pass the test above and check nothing. A reference to a banned type counts once and
// its call counts once, so a snippet with both is counted twice, and says so.
func TestReachGuardCanFail(t *testing.T) {
	const pkg = "package p\n"
	storeImp := `import store "` + storePath + `"` + "\n"
	coreImp := `import "` + corePath + `"` + "\n"
	netImp := `import "net"` + "\n"
	httpImp := `import "net/http"` + "\n"
	tlsImp := `import "crypto/tls"` + "\n"
	cases := []struct {
		name   string
		src    string
		faults int
	}{
		// The import and the call are each a fault. A ListenConfig is a listener's
		// configuration, so its literal is a second fault, as http.Server's is.
		{"store.Open", pkg + `import "` + storePath + `"` + "\nfunc f() { store.Open(\"x\") }\n", 2},
		{"aliased store import", pkg + storeImp + "func f() { store.Open(\"x\") }\n", 2},
		{"core.NewLocal", pkg + coreImp + "func f() { core.NewLocal(nil) }\n", 1},
		{"net.Listen", pkg + netImp + "func f() { net.Listen(\"tcp\", \"127.0.0.1:0\") }\n", 1},
		{"net.ListenPacket (a Listen prefix)", pkg + netImp + "func f() { net.ListenPacket(\"udp\", \":0\") }\n", 1},
		{"a method of a net.ListenConfig", pkg + netImp + "func f() { (&net.ListenConfig{}).Listen(nil, \"tcp\", \":0\") }\n", 2},
		{"net.FileListener", pkg + netImp + "func f() { net.FileListener(nil) }\n", 1},
		{"http.ListenAndServe", pkg + httpImp + "func f() { http.ListenAndServe(\":80\", nil) }\n", 1},
		{"http.Serve", pkg + httpImp + "func f() { http.Serve(nil, nil) }\n", 1},
		// A *http.Server parameter is a reference to the type, and the call is a second fault.
		{"a server's ListenAndServe", pkg + httpImp + "func f(s *http.Server) { s.ListenAndServe() }\n", 2},
		{"ListenAndServeTLS", pkg + httpImp + "func f(s *http.Server) { s.ListenAndServeTLS(\"c\", \"k\") }\n", 2},
		{"http.Server literal", pkg + httpImp + "var s = &http.Server{Addr: \":80\"}\n", 1},
		{"a var of http.Server, then Serve", pkg + httpImp + "var s http.Server\nfunc f(l net.Listener) { s.Serve(l) }\n", 2},
		{"new(http.Server)", pkg + httpImp + "var s = new(http.Server)\n", 1},
		{"an embedded *http.Server, then a call", pkg + httpImp + "type h struct{ *http.Server }\nfunc f(x h) { x.ListenAndServe() }\n", 2},
		{"a method value of ListenAndServe", pkg + "type s interface{ ListenAndServe() error }\nfunc f(x s) { g := x.ListenAndServe; _ = g }\n", 1},
		{"tls.Listen", pkg + tlsImp + "func f() { tls.Listen(\"tcp\", \":0\", nil) }\n", 1},
		{"tls.NewListener", pkg + tlsImp + "func f(l net.Listener) { tls.NewListener(l, nil) }\n", 1},
		{"a dot import of net", pkg + `import . "net"` + "\nfunc f() { Listen(\"tcp\", \":0\") }\n", 1},
		{"a dot import of a package that has a Listen", pkg + `import . "x/lib"` + "\nvar _ = 1\n", 1},
		{"an import of httptest", pkg + `import "net/http/httptest"` + "\nvar _ = 1\n", 1},
		{"a clean file",
			pkg + coreImp + httpImp + "func f(h http.Handler) { core.ValidProjectName(\"x\"); _ = http.StatusOK; h.ServeHTTP(nil, nil); _ = http.ServeFile }\n", 0},
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

// TestGuardedDirsAreDerived checks the directory rule. The derived set holds the
// required packages, and no exempt one; a package added to the module is in it
// without a line of the guard being changed.
func TestGuardedDirsAreDerived(t *testing.T) {
	dirs := guardedDirs(t)
	for _, name := range requiredGuarded {
		if !contains(dirs, name) {
			t.Errorf("%s is not guarded", name)
		}
	}
	for name := range serverSide {
		if contains(dirs, name) {
			t.Errorf("%s is exempt, and is also guarded", name)
		}
	}
	if len(dirs) < len(requiredGuarded) {
		t.Errorf("the guard checks %d packages, want at least %d", len(dirs), len(requiredGuarded))
	}
}
