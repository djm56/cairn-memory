// Package docscheck reads the marked tables of the wiki, so that a test can compare
// the names a page lists with the names the code registers. CONTRIBUTING.md (Keeping
// docs current) gives the rule; the tests that use this package are the gate.
//
// A marked table sits between an opening marker and a closing one, each an HTML
// comment: <!-- NAME:begin --> and <!-- NAME:end -->. Inside the markers the table has
// one header row, then one row per name. The first column of each row is the name in
// backticks, such as `task_current`. Any other column is free text for readers.
package docscheck

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// nameCell matches a first cell that is one name in backticks, and captures the name.
var nameCell = regexp.MustCompile("^`([^`]+)`$")

// Names returns the names in the first column of the table between the markers of
// marker in the file at path, in table order. It fails when a marker is missing or
// repeated, when the table has no header row, when a row's first cell is not one
// backticked name, and when a name is listed twice.
func Names(path, marker string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	begin := "<!-- " + marker + ":begin -->"
	end := "<!-- " + marker + ":end -->"

	lines := strings.Split(string(data), "\n")
	open, closing := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case begin:
			if open >= 0 {
				return nil, fmt.Errorf("%s: the marker %s is repeated (lines %d and %d)", path, begin, open+1, i+1)
			}
			open = i
		case end:
			if closing >= 0 {
				return nil, fmt.Errorf("%s: the marker %s is repeated (lines %d and %d)", path, end, closing+1, i+1)
			}
			closing = i
		}
	}
	if open < 0 {
		return nil, fmt.Errorf("%s: no %s marker", path, begin)
	}
	if closing < 0 {
		return nil, fmt.Errorf("%s: no %s marker", path, end)
	}
	if closing < open {
		return nil, fmt.Errorf("%s: %s comes before %s", path, end, begin)
	}

	var names []string
	seen := map[string]bool{}
	header := false
	for i := open + 1; i < closing; i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if isSeparator(line) {
			continue
		}
		if !header {
			// The first row is the header. It names the columns and is not a name.
			header = true
			continue
		}
		cell := strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "|"), "|")[0])
		m := nameCell.FindStringSubmatch(cell)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: the first column must be one name in backticks, got %q", path, i+1, cell)
		}
		name := m[1]
		if seen[name] {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, i+1, name)
		}
		seen[name] = true
		names = append(names, name)
	}
	if !header {
		return nil, fmt.Errorf("%s: no table between the %s markers", path, marker)
	}
	return names, nil
}

// isSeparator reports whether a table line is the row of dashes under the header.
func isSeparator(line string) bool {
	trimmed := strings.Trim(line, "|")
	if strings.TrimSpace(trimmed) == "" {
		return false
	}
	for _, r := range trimmed {
		if r != '-' && r != ':' && r != '|' && r != ' ' {
			return false
		}
	}
	return true
}

// Diff compares the names the code registers (want) with the names a page lists
// (got). missing are the names in want that got lacks, and extra are the names in
// got that want lacks. Each keeps the order of the list it came from.
func Diff(want, got []string) (missing, extra []string) {
	have := make(map[string]bool, len(got))
	for _, n := range got {
		have[n] = true
	}
	wanted := make(map[string]bool, len(want))
	for _, n := range want {
		wanted[n] = true
		if !have[n] {
			missing = append(missing, n)
		}
	}
	for _, n := range got {
		if !wanted[n] {
			extra = append(extra, n)
		}
	}
	return missing, extra
}
