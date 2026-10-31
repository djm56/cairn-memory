package core_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// The subcommand rule (step 8 review W2). The guard's import and selector rules see a
// call only through its package. cmd/cairn is one package, so a function in mcp.go that
// calls runServe, which starts the listener, names no import and no listener selector,
// and nothing above it reports the call. This rule closes that route: the names the two
// subcommand files declare may be named only by the dispatch table in main.go, and by
// those two files. The rule is a denylist of one package's names, so the guard's own
// file set is what it reads (nonTestGoFiles), and a file added to cmd/cairn is checked
// from its first commit.
//
// The dispatch identifiers of main.go are guarded too (step 10 review 10-W1). run and
// commands are how main.go reaches the two files: a call to run with a subcommand's
// name starts that subcommand, and names no guarded identifier. main.go may name them
// anywhere in the file. No other non-test file in cmd/cairn may.

// subcommandFiles are the files of cmd/cairn whose top-level names are reachable only
// from the dispatch table. serve.go starts the listener through server.Run, and key.go
// makes and revokes keys in the server's database.
var subcommandFiles = map[string]bool{
	"cmd/cairn/serve.go": true,
	"cmd/cairn/key.go":   true,
}

// dispatchFile is the one file that names the subcommand functions outside the two files:
// the table that maps each sub-command name to its run function.
const dispatchFile = "cmd/cairn/main.go"

// dispatchNames are the identifiers main.go declares that route to a subcommand: the
// dispatch function, the table, and the table's entry type. The rule guards them as it
// guards the subcommand names, so no file but main.go may name them. The test fails if
// main.go stops declaring one, so the list cannot go stale unnoticed.
var dispatchNames = []string{"run", "commands", "command"}

// declaredNames returns the top-level names f declares: its functions that are not
// methods, and its types, variables and constants. init and blank names are left out.
func declaredNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	add := func(name string) {
		if name != "_" && name != "init" {
			out[name] = true
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						add(id.Name)
					}
				}
			}
		}
	}
	return out
}

// dispatchFaults returns one description for each identifier of f that is one of names,
// with its position, unless allowed says f may name them. A field or method selected from
// a value (x.runServe) is not a reference to the package's name, so it is not counted. The
// match is by name: a local variable that shares a name with one of these functions is a
// fault too, and the fix is to rename the variable.
func dispatchFaults(fset *token.FileSet, f *ast.File, names map[string]bool, allowed bool) []string {
	if allowed {
		return nil
	}
	selected := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			selected[s.Sel] = true
		}
		return true
	})
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && names[id.Name] && !selected[id] {
			out = append(out, fmt.Sprintf("%s: %s is a subcommand name or a dispatch identifier, and only main.go may name it", fset.Position(id.Pos()), id.Name))
		}
		return true
	})
	return out
}

// TestSubcommandsAreReachedOnlyFromDispatch is the rule over the package. The names the two
// subcommand files declare must not appear in any other non-test file of cmd/cairn, except
// the dispatch table. Test files are outside the guard, as they are for every rule here.
func TestSubcommandsAreReachedOnlyFromDispatch(t *testing.T) {
	dir := filepath.Join(repoRoot, "cmd", "cairn")
	names := map[string]bool{}
	for _, file := range nonTestGoFiles(t, dir) {
		if subcommandFiles[moduleRel(file)] {
			fset := token.NewFileSet()
			for name := range declaredNames(parseFile(t, fset, file)) {
				names[name] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("the subcommand files declare no names: the rule would check nothing")
	}
	mainNames := declaredNames(parseFile(t, token.NewFileSet(), filepath.Join(repoRoot, dispatchFile)))
	for _, name := range dispatchNames {
		if !mainNames[name] {
			t.Fatalf("%s no longer declares %s: update dispatchNames", dispatchFile, name)
		}
		names[name] = true
	}
	for _, file := range nonTestGoFiles(t, dir) {
		rel := moduleRel(file)
		allowed := rel == dispatchFile || subcommandFiles[rel]
		fset := token.NewFileSet()
		for _, fault := range dispatchFaults(fset, parseFile(t, fset, file), names, allowed) {
			t.Errorf("%s", fault)
		}
	}
}

// TestSubcommandRuleCanFail proves the rule can fail: each snippet holds a reference the rule
// must count, or one it must not, and the count is what the rule must answer. With the rule
// disabled, the faults are zero and the snippets that name a subcommand fail.
func TestSubcommandRuleCanFail(t *testing.T) {
	names := map[string]bool{"runServe": true, "keyFlags": true, "run": true}
	cases := []struct {
		name    string
		src     string
		allowed bool
		faults  int
	}{
		{"cairn mcp calling runServe, in another file", "package main\nfunc serveMCP() { runServe(nil, nil, nil) }\n", false, 1},
		{"a key helper called from another file", "package main\nfunc f() { keyFlags(\"add\", nil, nil, nil, nil) }\n", false, 1},
		{"a subcommand function used as a value", "package main\nvar h = runServe\n", false, 1},
		{"cairn mcp calling run with serve, in another file", "package main\nfunc serveMCP() { run([]string{\"serve\"}, nil, nil) }\n", false, 1},
		{"a field of the same name, which is no reference", "package main\nfunc f(x T) { x.runServe() }\n", false, 0},
		{"the dispatch table in main.go", "package main\nvar h = runServe\n", true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", c.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			if got := dispatchFaults(fset, f, names, c.allowed); len(got) != c.faults {
				t.Fatalf("dispatchFaults = %d faults %q, want %d", len(got), got, c.faults)
			}
		})
	}
}
