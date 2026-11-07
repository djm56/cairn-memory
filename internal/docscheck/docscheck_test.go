package docscheck

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writePage writes body to a file in a fresh directory and returns its path.
func writePage(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "page.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNamesReadsTheMarkedTable checks the parser on a well-formed table, with prose
// around the markers and a separator row, and reads the names in table order.
func TestNamesReadsTheMarkedTable(t *testing.T) {
	page := writePage(t, `# Title

Prose above.

<!-- t:begin -->
| Tool | Description |
|------|-------------|
| `+"`alpha`"+` | first |
| `+"`beta`"+` | second |
<!-- t:end -->

Prose below `+"`not`"+` read.
`)
	got, err := Names(page, "t")
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

// TestNamesRefusesAPageItCannotRead checks each way a page can be malformed, and that
// the error names the fault.
func TestNamesRefusesAPageItCannotRead(t *testing.T) {
	const table = "| Tool | Description |\n|---|---|\n"
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"no markers", "# no table\n", "no <!-- t:begin --> marker"},
		{"no closing marker", "<!-- t:begin -->\n" + table, "no <!-- t:end --> marker"},
		{"repeated opening marker", "<!-- t:begin -->\n<!-- t:begin -->\n<!-- t:end -->\n", "repeated"},
		{"closing before opening", "<!-- t:end -->\n<!-- t:begin -->\n", "comes before"},
		{"no header row", "<!-- t:begin -->\n<!-- t:end -->\n", "no table"},
		{"name not in backticks", "<!-- t:begin -->\n" + table + "| alpha | first |\n<!-- t:end -->\n", "one name in backticks"},
		{"name listed twice", "<!-- t:begin -->\n" + table + "| `a` | x |\n| `a` | y |\n<!-- t:end -->\n", "listed twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Names(writePage(t, tc.body), "t")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Names error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestDiffNamesWhatIsMissingAndWhatIsExtra checks that Diff reports each side's
// names, in the order of the list they came from.
func TestDiffNamesWhatIsMissingAndWhatIsExtra(t *testing.T) {
	missing, extra := Diff([]string{"a", "b", "c"}, []string{"b", "d"})
	if want := []string{"a", "c"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	if want := []string{"d"}; !reflect.DeepEqual(extra, want) {
		t.Errorf("extra = %v, want %v", extra, want)
	}
	missing, extra = Diff([]string{"a"}, []string{"a"})
	if len(missing) != 0 || len(extra) != 0 {
		t.Errorf("equal lists differ: missing %v, extra %v", missing, extra)
	}
}
