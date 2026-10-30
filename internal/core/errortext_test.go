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
// core, the HTTP handlers and client that carry its errors to a caller, the MCP
// tools that carry them to a model, and the CLI, which prints them.
var errorSources = []string{"../core", "../api", "../client", "../mcp", "../../cmd/cairn"}

// errorConstructors are the core helpers that build an error for the caller.
var errorConstructors = map[string]bool{
	"Invalid": true, "Refused": true, "NotFound": true, "OperatorOnly": true,
}

// TestErrorTextNamesOnlySafeFix is the error-text audit (issue #70). It reads
// the non-test source of the core and the CLI, and collects every string literal
// that forms an error message: the arguments of the core error helpers, of
// errors.New and fmt.Errorf, the Message of a core.Error literal, and the text
// the CLI writes to stderr. A literal that contains one of unsafeAdvice fails
// the test, and the message names the file, the line and the word.
//
// Only literals are read. Text a caller supplies at run time is quoted back to
// it, not advised, so it is not checked.
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
			for _, lit := range errorLiterals(f) {
				checked++
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
				}
				for _, bad := range unsafeAdvice {
					if strings.Contains(text, bad) {
						t.Errorf("%s: error text %q contains %q; name only a safe next step", fset.Position(lit.Pos()), text, bad)
					}
				}
			}
		}
	}
	// The audit is only meaningful if it found the messages it reads.
	if checked < 50 {
		t.Fatalf("the audit read %d error literals, want at least 50: the source layout may have changed", checked)
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
		}
		return true
	})
	return out
}

// isErrorCall reports whether call builds an error message: a core error
// helper, errors.New, fmt.Errorf, or a write of the CLI to stderr.
func isErrorCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return errorConstructors[fn.Name]
	case *ast.SelectorExpr:
		pkg := identName(fn.X)
		switch {
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
