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

// maxResultBytes caps the text of one success result, in bytes. The client's cap on an
// answer (client.MaxResponseBytes, 16 MiB) guards the adapter's memory, not a model's
// context, and a task_get with no section sends the whole task. A result over this cap is
// a tool error that names how to narrow the read, so the model asks for less (#111). 64 KiB
// is a judgement: about 16,000 tokens of a model's context for one call.
const maxResultBytes = 64 << 10

// answer is the success result of the tool named tool: v as compact JSON in one text
// block. Compact, because every result is read by a model (PLAN.md §6). A value that
// does not encode is a failure of the adapter, and is answered as one. A value that
// encodes to more than maxResultBytes is a tool error, not a block of that size: the
// message is narrowClause for the tool, so it names only arguments the tool takes (#111).
func answer(v any, tool string) *mcp.CallToolResult {
	body, err := compact(v)
	if err != nil {
		return failure(err)
	}
	if len(body) > maxResultBytes {
		return failure(core.Invalid(
			"the result is %d bytes, more than the %d a tool result may hold: %s",
			len(body), maxResultBytes, narrowClause(tool)))
	}
	return text(string(body), false)
}

// narrowClause is the part of the #111 refusal that tells the model how to ask for less,
// for the tool named tool. It names only the arguments that tool takes. A tool with no
// argument that narrows its result gets the generic clause, which names none.
func narrowClause(tool string) string {
	switch tool {
	case "task_get":
		return "narrow the read with the section argument (brief, steps, events or close); for events, page with the limit and cursor arguments. A brief over the limit cannot be narrowed further, so its text must be shortened"
	case "task_list":
		return "narrow the list with the status argument, or page with the limit and cursor arguments"
	case "learning_list":
		return "narrow the list with the status, kind or target arguments, or page with the limit and cursor arguments"
	case "search":
		return "narrow the search with the kinds argument, or page with the limit and cursor arguments"
	case "directive_list":
		return "narrow the list with the agent argument"
	case "recall":
		return "ask for fewer items with the limit argument"
	}
	return "this tool takes no argument that narrows its result, so the stored text must be shortened"
}

// compact encodes v as one line of JSON, without the HTML escapes json.Marshal
// writes. A task's brief and an event carry code and shell, such as "&&" and "->",
// and the escapes are noise a model reads for no gain. The
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
