package main

import (
	"bytes"
	"fmt"
	"testing"
)

// TestTagLinesPrefixesEachLine checks the writer enable and doctor put on an experimental client's output: every line
// starts with the prefix, a line that a write ends partway through gets one prefix, and a blank line is tagged too.
func TestTagLinesPrefixesEachLine(t *testing.T) {
	var b bytes.Buffer
	w := tagLines(&b, "P: ")
	fmt.Fprint(w, "one\ntw")
	fmt.Fprint(w, "o\n\nthree")
	fmt.Fprint(w, "\n")
	if want := "P: one\nP: two\nP: \nP: three\n"; b.String() != want {
		t.Errorf("tagged output %q, want %q", b.String(), want)
	}
}
