package cli

import (
	"errors"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// Reason is the text of err for a person: the message of a core.Error, without its
// kind, and the error's own text for any other failure. A message the server wrote (a
// client.ServerError) goes through Plain, so it is one printable line capped at
// MaxLineRunes. A local error goes through FoldLine alone: it is still one printable
// line with no control or format character, but it is not cut. A local error names a
// path or a fix, and doctor exists to show that cause, so a cut there hides the thing it
// is for (#113).
func Reason(err error) string {
	var ce *core.Error
	if !errors.As(err, &ce) {
		return FoldLine(err.Error())
	}
	var se *client.ServerError
	if errors.As(err, &se) {
		return Plain(ce.Message)
	}
	return FoldLine(ce.Message)
}
