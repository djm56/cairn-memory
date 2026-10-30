package main

import (
	"strings"
	"testing"
)

// TestPlainFoldsAndStripsServerText checks the rules of plain (step 10 review W3): each
// line break and separator is a space, each control character and each escape sequence
// is gone, and the edges are trimmed.
func TestPlainFoldsAndStripsServerText(t *testing.T) {
	cases := map[string]string{
		"line one\nline two":                     "line one line two",
		"carriage\rreturn":                       "carriage return",
		"crlf\r\nis one break":                   "crlf is one break",
		"sep arator and\u0085nel":                "sep arator and nel",
		"tab\tand\vvertical\fform":               "tab and vertical form",
		"bell\x07 and nul\x00 dropped":           "bell and nul dropped",
		"colour \x1b[31mred\x1b[0m done":         "colour red done",
		"title \x1b]0;hijack\x07 kept":           "title kept",
		"title \x1b]0;hijack\x1b\\ kept":         "title kept",
		"c1 \u009b2J cleared":                    "c1 cleared",
		"two-byte \x1b7 saved":                   "two-byte saved",
		"intermediate \x1b(B text":               "intermediate text",
		"  edges  ":                              "edges",
		"a cut sequence \x1b[31":                 "a cut sequence",
		"## Heading\r\n- item":                   "## Heading - item",
		"plain text, unchanged: a -> b && c <d>": "plain text, unchanged: a -> b && c <d>",
	}
	for in, want := range cases {
		if got := plain(in); got != want {
			t.Errorf("plain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPlainCapsLength checks the cap: maxLineRunes runes in all, the last of them an
// ellipsis, and a text at the cap is kept whole.
func TestPlainCapsLength(t *testing.T) {
	exact := strings.Repeat("a", maxLineRunes)
	if got := plain(exact); got != exact {
		t.Errorf("a text of %d runes was changed", maxLineRunes)
	}
	long := strings.Repeat("é", maxLineRunes+40)
	got := plain(long)
	if n := len([]rune(got)); n != maxLineRunes {
		t.Errorf("capped text has %d runes, want %d", n, maxLineRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capped text %q does not end in an ellipsis", got)
	}
}

// TestBlockTextKeepsLinesAndStripsControls checks blockText, for section bodies: the
// line feeds and the tabs stay, a carriage return becomes a line feed, and the controls
// and escape sequences go.
func TestBlockTextKeepsLinesAndStripsControls(t *testing.T) {
	in := "first\r\nsecond\rthird\x1b[2J\n\tindented\x07 end"
	want := "first\nsecond\nthird\n\tindented\nend"
	if got := blockText(in); got != want {
		t.Errorf("blockText = %q, want %q", got, want)
	}
}

// TestFoldLineHasNoCap checks that the export's single-line fields are folded, not cut.
func TestFoldLineHasNoCap(t *testing.T) {
	long := strings.Repeat("b", maxLineRunes+10)
	if got := foldLine(long); got != long {
		t.Errorf("foldLine cut a text of %d runes", len(long))
	}
}
