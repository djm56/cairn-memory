package cli

import (
	"go/build"
	"strings"
	"testing"
)

// TestCLIDoesNotImportTheStore checks that this package's own non-test files do not import
// internal/store. DESIGN §5.1 says the CLI calls internal/client, never the store. The check reads
// the direct imports of the package, which is the rule the design states: internal/core holds the
// store, and this package reaches core only through its service interface, so the store is in the
// build through core and not through this package.
func TestCLIDoesNotImportTheStore(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("read the imports of the package: %v", err)
	}
	if len(pkg.Imports) == 0 {
		t.Fatal("the package lists no imports, so the check would pass on nothing")
	}
	for _, imp := range pkg.Imports {
		if importsStore(imp) {
			t.Errorf("internal/cli imports %s; the CLI calls internal/client, never the store (DESIGN §5.1)", imp)
		}
	}
}

// TestImportsStoreRecognisesTheStoreOnly checks the rule the import test applies: the store
// itself, and a package below it, are the store; a package whose name only begins with the same
// letters is not.
func TestImportsStoreRecognisesTheStoreOnly(t *testing.T) {
	const mod = "github.com/djm56/cairn-memory/internal/"
	for imp, want := range map[string]bool{
		mod + "store":      true,
		mod + "store/sub":  true,
		mod + "storefront": false,
		mod + "core":       false,
		mod + "client":     false,
		"database/sql":     false,
	} {
		if got := importsStore(imp); got != want {
			t.Errorf("importsStore(%q) = %v, want %v", imp, got, want)
		}
	}
}

// importsStore reports whether the import path imp is the store, or a package inside it.
func importsStore(imp string) bool {
	const store = "/internal/store"
	return strings.HasSuffix(imp, store) || strings.Contains(imp, store+"/")
}
