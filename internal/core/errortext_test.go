package core_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unsafeAdvice lists the words an error string must not contain. Each one
// advises a destructive or irreversible step (Plan v1 rule 2, CONTRACT §1.2).
// "rm " has its trailing space, so it matches the command and not "form" or
// "confirm".
var unsafeAdvice = []string{"--force", "rm ", "reset"}

// errorSources are the directories whose error strings the audit reads: the
// core, the HTTP handlers and client that carry its errors to a caller, the
// project and server resolution, the MCP tools that carry the errors to a model,
// and the CLI, which prints them.
// The identity package is included (step 8 review S8): its messages reach a
// caller as core.Invalid, and the audit must read them too.
var errorSources = []string{"../core", "../api", "../client", "../identity", "../mcp", "../../cmd/cairn"}

// errorConstructors are the core helpers that build an error for the caller.
var errorConstructors = map[string]bool{
	"Invalid": true, "Refused": true, "NotFound": true, "OperatorOnly": true,
}

// TestErrorTextNamesOnlySafeFix is the error-text audit (issue #70). It reads
// the non-test source of the core and the CLI, and collects every string literal
// that forms an error message: the arguments of the core error helpers, written
// bare inside core or as core.Invalid and its siblings elsewhere, of errors.New
// and fmt.Errorf, the Message of a core.Error literal, the text the CLI writes to
// stderr, and the detail of a doctor check, which is printed. A literal that
// contains one of unsafeAdvice fails the test, and the message names the file,
// the line and the word.
//
// Only literals are read. Text a caller supplies at run time is quoted back to
// it, not advised, so it is not checked.
//
// The step 10 review (W2) found the audit read two strings in ../identity while it
// missed the five or six that name `cairn init`. TestErrorAuditCanFail holds the
// forms the audit reads, so a form that drops out fails there, not here.
func TestErrorTextNamesOnlySafeFix(t *testing.T) {
	var checked int
	for _, dir := range errorSources {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		// Glob returns nothing, and no error, for a directory that is gone. A
		// renamed package would then drop out of the audit without a word.
		if len(files) == 0 {
			t.Fatalf("the audit lists %s, which matches no Go files: update errorSources", dir)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			n, faults, err := auditFile(fset, f)
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			checked += n
			for _, fault := range faults {
				t.Error(fault)
			}
		}
	}
	t.Logf("the audit read %d error literals", checked)
	// The audit is only meaningful if it found the messages it reads.
	if checked < 50 {
		t.Fatalf("the audit read %d error literals, want at least 50: the source layout may have changed", checked)
	}
}

// auditFile reads the error literals of one parsed file. It returns how many it read,
// and one fault for each one that contains a word of unsafeAdvice.
func auditFile(fset *token.FileSet, f *ast.File) (int, []string, error) {
	var faults []string
	lits := errorLiterals(f)
	for _, lit := range lits {
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			return 0, nil, err
		}
		for _, bad := range unsafeAdvice {
			if strings.Contains(text, bad) {
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "snippet.go", c.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			n, faults, err := auditFile(fset, f)
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

// errorLiterals returns the string literals in f that form an error message.
func errorLiterals(f *ast.File) []*ast.BasicLit {
	var out []*ast.BasicLit
	collect := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				out = append(out, lit)
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
			// A doctor check: its detail is printed to a person (step 10 review W2).
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

// isErrorCall reports whether call builds an error message: a core error
// helper, bare or as core.<helper>, errors.New, fmt.Errorf, or a write of the CLI
// to stderr.
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
			return identName(call.Args[0]) == "stderr" || identName(call.Args[0]) == "Stderr"
		}
	}
	return false
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
