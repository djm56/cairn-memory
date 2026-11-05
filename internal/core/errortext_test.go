package core_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// unsafeAdvice lists the words an error string must not contain. Each one
// advises a destructive or irreversible step (Plan v1 rule 2, CONTRACT §1.2).
// "rm " has its trailing space, so it matches the command and not "form" or
// "confirm". A text is matched in lower case, so "RESET" and "--FORCE" are caught too.
var unsafeAdvice = []string{"--force", "rm ", "reset"}

// auditRoot is the repository root, as this package sees it. The audit walks it, so a
// package that carries error text is audited from the day it is added, and no list has to
// name it (#108).
const auditRoot = "../.."

// skipDirs are the directories the audit's walk does not enter when they sit directly under
// the root. They hold no Go source of the product: version control, the wiki and docs, build
// output and vendored code. A directory of the same name lower in the tree is a package like
// any other, and is audited (6-W5).
var skipDirs = map[string]bool{
	".git": true, "wiki": true, "docs": true, "bin": true, "vendor": true, ".claude": true,
}

// errorConstructors are the core helpers that build an error for the caller.
var errorConstructors = map[string]bool{
	"Invalid": true, "Refused": true, "NotFound": true, "OperatorOnly": true,
}

// TestErrorTextNamesOnlySafeFix is the error-text audit (issue #70). It reads
// the non-test source of every package under the repository root, and collects every
// string literal that forms an error message: the arguments of the core error helpers,
// written bare inside core or as core.Invalid and its siblings elsewhere, of errors.New
// and fmt.Errorf, the Message of a core.Error literal, the text written to the error
// stream (os.Stderr, ErrOrStderr(), a writer named for stderr), and the detail of a
// doctor check, which is printed. A string constant or variable passed by name is read
// through its literal (collectConsts). A literal that contains one of unsafeAdvice, in
// any case, fails the test, and the message names the file, the line and the word.
//
// Only literals are read. Text a caller supplies at run time is quoted back to
// it, not advised, so it is not checked. Text that is built at run time, text written
// to stdout, and text passed through a helper that writes it for the caller are not read.
//
// An earlier audit read two strings in ../identity and missed the five or six that name
// `cairn init`. TestErrorAuditCanFail holds the forms the audit reads, so a form that
// drops out fails there, not here.
func TestErrorTextNamesOnlySafeFix(t *testing.T) {
	dirs, err := auditDirs(auditRoot)
	if err != nil {
		t.Fatal(err)
	}
	// The walk must reach the packages that carry the errors, or it has lost the tree.
	for _, want := range []string{"internal/core", "internal/client", "cmd/cairn"} {
		if !containsSuffix(dirs, want) {
			t.Fatalf("the audit walk did not reach %s: it found %d packages", want, len(dirs))
		}
	}
	var checked int
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		var asts []*ast.File
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			asts = append(asts, f)
		}
		// A string named in one file of the package may be passed in another, so the
		// constants are collected across the whole package before any file is read.
		consts := collectConsts(asts...)
		for _, f := range asts {
			n, faults, err := auditFile(fset, f, consts)
			if err != nil {
				t.Fatalf("%s: %v", fset.Position(f.Pos()).Filename, err)
			}
			checked += n
			for _, fault := range faults {
				t.Error(fault)
			}
		}
	}
	t.Logf("the audit read %d error literals in %d packages", checked, len(dirs))
	// The audit is only meaningful if it found the messages it reads.
	if checked < 50 {
		t.Fatalf("the audit read %d error literals, want at least 50: the source layout may have changed", checked)
	}
}

// TestAuditWalkFindsEveryPackage checks the walk that picks the audited packages: a
// directory with a non-test Go file is audited, one with only test files is not, and the
// directories in skipDirs are not entered when they sit directly under the root, so a
// package added anywhere in the tree is found without a list naming it. A skip-listed name
// lower in the tree (internal/docs, cmd/cairn/bin) is a package and is audited (6-W5). A
// testdata directory is not audited at any depth.
func TestAuditWalkFindsEveryPackage(t *testing.T) {
	root := auditTree(t)
	got, err := auditDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "a"), filepath.Join(root, "b", "c"), filepath.Join(root, "cmd", "cairn", "bin"),
		filepath.Join(root, "internal", "docs"), filepath.Join(root, "newpkg"),
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("auditDirs = %q, want %q", got, want)
	}
}

// TestAuditWalkAcceptsARootWithATrailingSeparator checks that a root written with a trailing
// separator skips the same directories as the root written without one (8-S10). The skip is
// decided by comparing the parent of each directory with the root, so the root is cleaned first.
func TestAuditWalkAcceptsARootWithATrailingSeparator(t *testing.T) {
	root := auditTree(t)
	want, err := auditDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 5 {
		t.Fatalf("the tree audits %d directories, want 5", len(want))
	}
	got, err := auditDirs(root + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("auditDirs with a trailing separator = %q, want %q", got, want)
	}
}

// auditTree writes the tree the walk tests read, and returns its root. It holds the skipped
// directories, the test-only ones, a package under a name skipDirs also has lower down, and a
// testdata directory at two depths.
func auditTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/x.go")
	write("a/x_test.go")
	write("b/c/y.go")
	write("only/z_test.go")
	write(".git/hooks/h.go")
	write("wiki/w.go")
	write("newpkg/n.go")
	write("internal/docs/d.go")
	write("cmd/cairn/bin/b.go")
	write("a/testdata/t.go")
	write("testdata/r.go")
	return root
}

// auditDirs returns, in order, the directories under root that hold a non-test Go file.
// A directory named in skipDirs is not entered when it sits directly under root. A directory
// named testdata is not entered at any depth, as the go tool ignores it; its files are test
// inputs, not packages, and one that does not parse would fail the audit.
func auditDirs(root string) ([]string, error) {
	seen := map[string]bool{}
	clean := filepath.Clean(root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "testdata" || (filepath.Dir(path) == clean && skipDirs[d.Name()])) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			seen[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// containsSuffix reports whether one of the paths ends in the path suffix, in slash form.
func containsSuffix(paths []string, suffix string) bool {
	for _, p := range paths {
		if strings.HasSuffix(filepath.ToSlash(p), "/"+suffix) {
			return true
		}
	}
	return false
}

// collectConsts returns the string constants and variables of files, by name. A const or
// var spec whose value is a string literal is read, at package level or inside a function.
// A name that is bound to anything else, or to text built at run time, is not. A name that
// two declarations share keeps both literals, so each is read.
func collectConsts(files ...*ast.File) map[string][]*ast.BasicLit {
	out := map[string][]*ast.BasicLit{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if i >= len(spec.Values) {
					continue
				}
				if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					out[name.Name] = append(out[name.Name], lit)
				}
			}
			return true
		})
	}
	return out
}

// auditFile reads the error literals of one parsed file, with the constants of its package.
// It returns how many it read, and one fault for each one that contains a word of
// unsafeAdvice, in any case.
func auditFile(fset *token.FileSet, f *ast.File, consts map[string][]*ast.BasicLit) (int, []string, error) {
	var faults []string
	lits := errorLiterals(f, consts)
	for _, lit := range lits {
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			return 0, nil, err
		}
		lower := strings.ToLower(text)
		for _, bad := range unsafeAdvice {
			if strings.Contains(lower, bad) {
				faults = append(faults, fset.Position(lit.Pos()).String()+": error text "+strconv.Quote(text)+" contains "+strconv.Quote(bad)+"; name only a safe next step")
			}
		}
	}
	return len(lits), faults, nil
}

// TestErrorAuditCanFail proves the audit reads each form it claims to, and that a
// banned word in that form is a fault. Each snippet holds one error literal in one
// form and says how many literals the audit must read and how many faults it must
// give. A form that the audit does not read gives zero literals and fails here.
func TestErrorAuditCanFail(t *testing.T) {
	const pkg = "package p\n"
	cases := []struct {
		name     string
		src      string
		literals int
		faults   int
	}{
		{"a bare core.Invalid, as inside core", pkg + "func f() { Invalid(\"run rm x\") }\n", 1, 1},
		{"core.Invalid, as outside core (W2)", pkg + "func f() { core.Invalid(\"run rm x\") }\n", 1, 1},
		{"core.Invalid with a format and a safe fix", pkg + "func f() { core.Invalid(\"no server: run `cairn init`, %s\", \"x\") }\n", 2, 0},
		{"core.Refused with --force", pkg + "func f() { core.Refused(\"use --force\") }\n", 1, 1},
		{"core.NotFound with reset", pkg + "func f() { core.NotFound(\"reset the id\") }\n", 1, 1},
		{"core.OperatorOnly with a safe fix", pkg + "func f() { core.OperatorOnly(\"ask the operator\") }\n", 1, 0},
		{"fmt.Errorf with --force", pkg + "import \"fmt\"\nfunc f() { _ = fmt.Errorf(\"--force\") }\n", 1, 1},
		{"errors.New with rm", pkg + "import \"errors\"\nfunc f() { _ = errors.New(\"rm -r\") }\n", 1, 1},
		{"a core.Error literal's Message", pkg + "func f() { _ = &core.Error{Kind: 1, Message: \"reset it\"} }\n", 1, 1},
		{"a doctor check detail", pkg + "func f() { _ = check{name: \"x\", status: \"fail\", detail: \"run rm -r\"} }\n", 1, 1},
		{"a doctor check name is not read", pkg + "func f() { _ = check{name: \"rm\", status: \"ok\", detail: \"fine\"} }\n", 1, 0},
		{"words that end in a banned fragment", pkg + "func f() { core.Invalid(\"confirm: the form\") }\n", 1, 0},
		{"a constant passed by name (the shape of checkReviewData)", pkg + "const shape = \"run rm x\"\nfunc f() { Invalid(shape) }\n", 1, 1},
		{"a constant passed by name, with a format", pkg + "const shape = \"run rm x\"\nfunc f() { Invalid(\"%s: fix it\", shape) }\n", 2, 1},
		{"a constant with a safe text", pkg + "const shape = \"run cairn init\"\nfunc f() { Invalid(shape) }\n", 1, 0},
		{"an upper-case reset", pkg + "func f() { core.Invalid(\"RESET the id\") }\n", 1, 1},
		{"an upper-case --FORCE", pkg + "func f() { core.Refused(\"use --FORCE\") }\n", 1, 1},
		{"a write to ErrOrStderr()", pkg + "import \"fmt\"\nfunc f(c C) { fmt.Fprintln(c.ErrOrStderr(), \"use --force\") }\n", 1, 1},
		{"a write to a writer named for stderr", pkg + "import (\n\"fmt\"\n\"io\"\n)\nfunc f(errOut io.Writer) { fmt.Fprintf(errOut, \"rm x\") }\n", 1, 1},
		{"os.Stderr.WriteString", pkg + "import \"os\"\nfunc f() { os.Stderr.WriteString(\"reset it\") }\n", 1, 1},
		{"io.WriteString to a stderr writer", pkg + "import \"io\"\nfunc f(stderr io.Writer) { io.WriteString(stderr, \"reset\") }\n", 1, 1},
		{"stdout is not read", pkg + "import (\n\"fmt\"\n\"os\"\n)\nfunc f() { fmt.Fprintf(os.Stdout, \"rm x\") }\n", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", c.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			n, faults, err := auditFile(fset, f, collectConsts(f))
			if err != nil {
				t.Fatalf("audit snippet: %v", err)
			}
			if n != c.literals {
				t.Errorf("the audit read %d literals, want %d", n, c.literals)
			}
			if len(faults) != c.faults {
				t.Errorf("the audit gave %d faults %q, want %d", len(faults), faults, c.faults)
			}
		})
	}
}

// errorLiterals returns the string literals in f that form an error message. A name in an
// error call that is a string constant or variable of the package (consts) adds that
// constant's literals. Each literal is returned once.
func errorLiterals(f *ast.File, consts map[string][]*ast.BasicLit) []*ast.BasicLit {
	var out []*ast.BasicLit
	seen := map[token.Pos]bool{}
	add := func(lit *ast.BasicLit) {
		if !seen[lit.Pos()] {
			seen[lit.Pos()] = true
			out = append(out, lit)
		}
	}
	collect := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					add(n)
				}
			case *ast.Ident:
				for _, lit := range consts[n.Name] {
					add(lit)
				}
			}
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if isErrorCall(n) {
				for _, arg := range n.Args {
					collect(arg)
				}
			}
		case *ast.CompositeLit:
			// A core.Error literal: only its Message is text the caller reads.
			if isErrorType(n.Type) {
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok && identName(kv.Key) == "Message" {
						collect(kv.Value)
					}
				}
			}
			// A doctor check: its detail is printed to a person.
			if identName(n.Type) == "check" {
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok && identName(kv.Key) == "detail" {
						collect(kv.Value)
					}
				}
			}
		}
		return true
	})
	return out
}

// isErrorCall reports whether call builds an error message: a core error helper, bare or
// as core.<helper>, errors.New, fmt.Errorf, or a write to the error stream: a Fprint call
// or io.WriteString whose writer is the error stream (isErrWriter), or os.Stderr.WriteString.
func isErrorCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return errorConstructors[fn.Name]
	case *ast.SelectorExpr:
		pkg := identName(fn.X)
		switch {
		case pkg == "core" && errorConstructors[fn.Sel.Name]:
			return true
		case pkg == "errors" && fn.Sel.Name == "New",
			pkg == "fmt" && fn.Sel.Name == "Errorf":
			return true
		case pkg == "fmt" && strings.HasPrefix(fn.Sel.Name, "Fprint") && len(call.Args) > 0:
			return isErrWriter(call.Args[0])
		case pkg == "io" && fn.Sel.Name == "WriteString" && len(call.Args) > 0:
			return isErrWriter(call.Args[0])
		case fn.Sel.Name == "WriteString":
			return identName(fn.X) == "Stderr"
		}
	}
	return false
}

// isErrWriter reports whether a writer is the error stream: os.Stderr, a call that gives it
// (cmd.ErrOrStderr()), or a name that says so (stderr, errOut, errW). Text written to
// stdout is not an error and is not read.
func isErrWriter(arg ast.Expr) bool {
	if call, ok := arg.(*ast.CallExpr); ok {
		arg = call.Fun
	}
	name := strings.ToLower(identName(arg))
	return name == "stderr" || strings.HasPrefix(name, "err")
}

// isErrorType reports whether typ names the core Error type, as Error or
// core.Error.
func isErrorType(typ ast.Expr) bool {
	return identName(typ) == "Error"
}

// identName returns the name an identifier or a selector ends in, or "".
func identName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}
