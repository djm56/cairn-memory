package cli

import (
	"strings"
	"testing"
)

// TestPlainFoldsAndStripsServerText checks the rules of plain: each
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
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPlainCapsLength checks the cap: MaxLineRunes runes in all, the last of them an
// ellipsis, and a text at the cap is kept whole.
func TestPlainCapsLength(t *testing.T) {
	exact := strings.Repeat("a", MaxLineRunes)
	if got := Plain(exact); got != exact {
		t.Errorf("a text of %d runes was changed", MaxLineRunes)
	}
	long := strings.Repeat("é", MaxLineRunes+40)
	got := Plain(long)
	if n := len([]rune(got)); n != MaxLineRunes {
		t.Errorf("capped text has %d runes, want %d", n, MaxLineRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capped text %q does not end in an ellipsis", got)
	}
}

// TestBlockTextKeepsLinesAndStripsControls checks BlockText, for section bodies: the
// line feeds and the tabs stay, a carriage return becomes a line feed, and the controls
// and escape sequences go.
func TestBlockTextKeepsLinesAndStripsControls(t *testing.T) {
	in := "first\r\nsecond\rthird\x1b[2J\n\tindented\x07 end"
	want := "first\nsecond\nthird\n\tindented\nend"
	if got := BlockText(in); got != want {
		t.Errorf("BlockText = %q, want %q", got, want)
	}
}

// TestFoldLineHasNoCap checks that the export's single-line fields are folded, not cut.
func TestFoldLineHasNoCap(t *testing.T) {
	long := strings.Repeat("b", MaxLineRunes+10)
	if got := FoldLine(long); got != long {
		t.Errorf("FoldLine cut a text of %d runes", len(long))
	}
}

// TestFormatCharactersAreDropped checks that a format character (Unicode
// category Cf) shows nothing and changes how the text around it is read, so plain,
// FoldLine and BlockText drop each group of them, and keep the text on both sides.
func TestFormatCharactersAreDropped(t *testing.T) {
	groups := map[string][]rune{
		"soft hyphen":            {0x00ad},
		"arabic letter mark":     {0x061c},
		"left and right marks":   {0x200e, 0x200f},
		"bidi embeddings":        {0x202a, 0x202b, 0x202c, 0x202d, 0x202e},
		"zero-width characters":  {0x200b, 0x200c, 0x200d},
		"word joiner":            {0x2060},
		"bidi isolates":          {0x2066, 0x2067, 0x2068, 0x2069},
		"byte order mark":        {0xfeff},
		"tag characters":         {0xe0001, 0xe0041, 0xe007f},
		"interlinear annotation": {0xfff9, 0xfffb},
	}
	for name, runes := range groups {
		for _, r := range runes {
			in := "a" + string(r) + "b"
			for fn, got := range map[string]string{
				"plain":     Plain(in),
				"FoldLine":  FoldLine(in),
				"BlockText": BlockText(in),
			} {
				if got != "ab" {
					t.Errorf("%s: %s(U+%04X) = %q, want %q", name, fn, r, got, "ab")
				}
			}
		}
	}
}

// TestFormatCharactersKeepOtherText checks that the cut is only of format characters: a
// letter with a mark, a CJK pair and an emoji are text, and stay.
func TestFormatCharactersKeepOtherText(t *testing.T) {
	in := "café 日本 👍 \u202eend"
	want := "café 日本 👍 end"
	if got := Plain(in); got != want {
		t.Errorf("Plain(%q) = %q, want %q", in, got, want)
	}
}
