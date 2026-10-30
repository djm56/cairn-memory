package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxLineRunes caps a server-derived text that the hook or status prints on one line:
// 200 runes. A longer text is cut and ends in an ellipsis. The hook's block goes into
// a model's context, so the cap is what keeps one long next action from filling it.
const maxLineRunes = 200

// plain returns a server-derived text as one printable line, capped at maxLineRunes.
// Line breaks become spaces, control characters and escape sequences are dropped, and
// a text that is too long is cut with an ellipsis. Every text the hook or status
// prints from the server goes through it (step 10 review W3), so a title cannot end a
// line and start another, or move a terminal's cursor, or carry a line into a model's
// context that the hook did not write.
func plain(s string) string {
	return capRunes(foldLine(s), maxLineRunes)
}

// foldLine returns s as one line of printable text, with no cap. Export uses it for
// the single-line fields of a document: a title, an owner, a file path, an event's
// text. Every line break, tab and separator becomes a space, every other control
// character is dropped, and an escape sequence is removed whole. Edge spaces are
// trimmed.
func foldLine(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b || r == 0x9b:
			// ESC, or the single-byte CSI: a sequence that changes the terminal. Its
			// parameters are not text, so the whole sequence goes.
			i = skipEscape(rs, i)
		case r == '\r' && i+1 < len(rs) && rs[i+1] == '\n':
			// A CRLF is one line break, not two spaces.
			b.WriteByte(' ')
			i++
		case isLineBreak(r):
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// Dropped: C0, DEL and C1 controls have no printable form.
		default:
			b.WriteRune(r)
		}
	}
	return collapseSpaces(b.String())
}

// collapseSpaces turns each run of ASCII spaces in s into one space, and trims the edges.
// A removed control sequence can leave two spaces where one line break was, and a line
// should not read that way. Other spaces are left as they are.
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' {
			if !space {
				b.WriteRune(r)
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// blockText returns a text that keeps its line breaks, for the body of a section of a
// document. Carriage returns and the Unicode line separators become line feeds, and
// every control character other than the line feed and the tab is dropped, with any
// escape sequence. The Markdown structure of the body is the author's, so it stays.
func blockText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, " ", "\n")
	s = strings.ReplaceAll(s, " ", "\n")
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b || r == 0x9b:
			i = skipEscape(rs, i)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r):
			// Dropped, as foldLine drops it.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isLineBreak reports whether r ends a line or separates what a person reads as
// separate words: the line feed, the carriage return, the other vertical controls, the
// tab, NEL, and the Unicode line and paragraph separators.
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', '\t', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// skipEscape returns the index of the last rune of the escape sequence that starts at
// rs[i], which is ESC or the C1 CSI. A sequence that is cut off by the end of the text
// ends the text. The forms handled are the control sequence (ESC [ ... final), the
// operating system command (ESC ] ... BEL or ESC \), a sequence with intermediate
// bytes, and the two-byte escape (ESC followed by one byte).
func skipEscape(rs []rune, i int) int {
	if rs[i] == 0x9b {
		return csiEnd(rs, i)
	}
	if i+1 >= len(rs) {
		return i
	}
	switch next := rs[i+1]; {
	case next == '[':
		return csiEnd(rs, i+1)
	case next == ']':
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == 0x07 {
				return j
			}
			if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	case next >= 0x20 && next <= 0x2f:
		j := i + 1
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}
		if j < len(rs) {
			return j
		}
		return len(rs) - 1
	default:
		return i + 1
	}
}

// csiEnd returns the index of the final byte of the control sequence whose introducer
// is at rs[i]: the first rune after it in 0x40 to 0x7e, or the last rune when there is
// none.
func csiEnd(rs []rune, i int) int {
	for j := i + 1; j < len(rs); j++ {
		if rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}
	}
	return len(rs) - 1
}

// capRunes returns s cut to n runes, the last of them an ellipsis, when s is longer.
func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
