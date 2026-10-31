package mcp

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
)

// kindInternal is the kind of a failure that is not a core.Error, such as an
// unreachable server. It is the same word the HTTP form uses for a server fault,
// and its message is the cause, since the cause is on the user's own machine.
const kindInternal core.Kind = "internal"

// answer is the success result of a tool: v as compact JSON in one text block.
// Compact, because every result is read by a model (PLAN.md §6). A value that
// does not encode is a failure of the adapter, and is answered as one.
func answer(v any) *mcp.CallToolResult {
	body, err := compact(v)
	if err != nil {
		return failure(err)
	}
	return text(string(body), false)
}

// compact encodes v as one line of JSON, without the HTML escapes json.Marshal
// writes. A task's brief and an event carry code and shell, such as "&&" and "->",
// and the escapes are noise a model reads for no gain (step 8 review S1). The
// trailing newline the encoder adds is removed.
func compact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// failure is the error result of a tool. Its text is the body of the HTTP error
// answer, {"error": {"kind": ..., "message": ..., "missing": [...]}}, so a model
// reads the same shape from both surfaces. The result is a tool error, with
// IsError set, and not a protocol error. A core.Error keeps its kind, message and
// missing list. Any other error is kindInternal, with its own text.
func failure(err error) *mcp.CallToolResult {
	var ce *core.Error
	if !errors.As(err, &ce) {
		ce = &core.Error{Kind: kindInternal, Message: err.Error()}
	}
	body, mErr := compact(struct {
		Error *core.Error `json:"error"`
	}{Error: ce})
	if mErr != nil {
		// A core.Error holds strings and a string list, so this cannot happen. The
		// fallback keeps the answer an error all the same.
		body = []byte(`{"error":{"kind":"internal","message":"the error could not be encoded"}}`)
	}
	return text(string(body), true)
}

// text is a result of one text block, marked as an error when isError is set.
func text(s string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: isError,
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}
}
