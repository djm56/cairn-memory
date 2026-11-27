package main

import (
	"bytes"
	"io"
)

// experimentalPrefix is the start of every line enable and doctor print for a client that is experimental: opencode and
// pi. The status is on each line, so a line copied out of a terminal still says it.
func experimentalPrefix(agent string) string {
	return "EXPERIMENTAL " + agent + ": "
}

// tagLines returns a writer that puts prefix at the start of every line written through it. A write may end partway
// through a line, and the next write carries on that line without a second prefix.
func tagLines(w io.Writer, prefix string) io.Writer {
	return &taggedWriter{w: w, prefix: prefix, atStart: true}
}

// taggedWriter is the writer tagLines returns. atStart is set when the next byte begins a line.
type taggedWriter struct {
	w       io.Writer
	prefix  string
	atStart bool
}

// Write puts the prefix before each line of p that starts a line, and writes the result to the writer under it. It
// reports len(p) bytes written when the underlying write succeeds, since the prefix is not the caller's bytes.
func (t *taggedWriter) Write(p []byte) (int, error) {
	var out bytes.Buffer
	for _, b := range p {
		if t.atStart {
			out.WriteString(t.prefix)
			t.atStart = false
		}
		out.WriteByte(b)
		if b == '\n' {
			t.atStart = true
		}
	}
	if _, err := t.w.Write(out.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}
