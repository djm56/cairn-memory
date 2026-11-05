package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// instructionsTemplate is the task loop an experimental client is given. {prefix} stands where the client names cairn's
// tools: OpenCode and pi each name an MCP server's tools in their own way, so instructionsFor fills it per client. {note}
// is an optional sentence after the one that gives the prefix, empty for OpenCode's own file.
//
//go:embed enable_instructions.md
var instructionsTemplate string

// The names enable writes for OpenCode. The config lists the instruction file relative to the project, and the file is
// written at the same place.
const (
	openCodeFile         = "opencode.json"
	openCodeJSONC        = "opencode.jsonc"
	openCodeInstructions = ".cairn/instructions.md"
	openCodeToolPrefix   = "cairn_"
	openCodeTimeoutMS    = 30000
)

// openCodeConfigAlias refuses when opencode.jsonc is in the project at root. OpenCode reads that file as well as
// opencode.json, and enable does not write a second config beside it, so the person adds the entry by hand. A file that
// cannot be looked for is refused too. Nothing is changed.
func openCodeConfigAlias(root string) error {
	path := filepath.Join(root, openCodeJSONC)
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return fmt.Errorf("%s is there, and OpenCode reads it: enable does not add a second config beside it, so add the cairn entry to %s by hand, or move it aside and run cairn enable again. Nothing was changed", path, openCodeJSONC)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return fmt.Errorf("look for %s: %v. Nothing was changed", path, err)
}

// openCodeEntry is the mcp entry enable writes for the cairn server. DESIGN §6.2 asks for a timeout above the 5 s default:
// the OpenCode docs page gives 30 s as the default, while the schema gives 5 s, so 30 s is written here.
func openCodeEntry() map[string]any {
	return map[string]any{"type": "local", "command": []string{"cairn", "mcp"}, "timeout": openCodeTimeoutMS}
}

// instructionsFor returns the task loop with the client's tool prefix where it names a tool.
func instructionsFor(prefix string) string {
	return instructionsWith(prefix, "")
}

// instructionsWith returns the task loop with the client's tool prefix, and note after the sentence that gives the prefix.
// The AGENTS.md block uses a note: OpenCode reads AGENTS.md too, and names its tools with another prefix.
func instructionsWith(prefix, note string) string {
	return strings.NewReplacer("{prefix}", prefix, "{note}", note).Replace(instructionsTemplate)
}

// openCodeCompared is the part of the cairn entry doctor compares: its type and its command. The other fields, such as the
// timeout, belong to the person who edits the entry, so doctor does not call them a fault.
func openCodeCompared() map[string]any {
	e := openCodeEntry()
	return map[string]any{"type": e["type"], "command": e["command"]}
}

// instructionsMarker is the first line of the instruction file enable writes for OpenCode. It says the file is cairn's own
// text, so enable may replace the file when cairn's text changes, and it is the line a person deletes to keep their own
// edits. The file is not the AGENTS.md block, so the marker is not in that block.
const instructionsMarker = "<!-- cairn task loop, written by cairn enable (EXPERIMENTAL): replaced when cairn's text changes; delete this line to keep your own edits -->"

// openCodeInstructionsText is the whole instruction file enable writes for OpenCode: the marker, then the task loop with
// OpenCode's prefix.
func openCodeInstructionsText() string {
	return instructionsMarker + "\n" + instructionsFor(openCodeToolPrefix)
}

// sameText reports whether a and b are the same text once line endings are taken as LF. A checkout that turns LF into CRLF
// is the same text, so it is not replaced.
func sameText(a, b []byte) bool {
	return bytes.Equal(bytes.ReplaceAll(a, []byte("\r\n"), []byte("\n")), bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
}

// hasInstructionsMarker reports whether the first line of raw, with its line ending taken as LF, is the marker.
func hasInstructionsMarker(raw []byte) bool {
	first, _, _ := bytes.Cut(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\n"))
	return string(first) == instructionsMarker
}

// enableOpenCode sets up OpenCode for the project at root, EXPERIMENTAL. It adds the cairn MCP server to opencode.json
// under "mcp", lists the instruction file in its "instructions", and writes that file. Each is merged as enable merges
// Claude Code's settings: what is there stays, a cairn entry that differs is reported and left, and a file is written only
// when it changes. An instruction file that cairn wrote, which has the marker, is replaced when cairn's text differs, with
// a backup first. An instruction file without the marker is someone's own, so it is left as it is, and the config is still
// merged. A refusal of the config refuses the run, and nothing is written.
func enableOpenCode(root, name string, dryRun bool, stdout, stderr io.Writer) int {
	if err := openCodeConfigAlias(root); err != nil {
		fmt.Fprintf(stderr, "cairn enable: %v\n", err)
		return 1
	}
	var mcpState, listState, fileState string
	want := []byte(openCodeInstructionsText())
	file := fileWrite{
		path: filepath.Join(root, filepath.FromSlash(openCodeInstructions)),
		merge: func(raw []byte, exists bool) ([]byte, bool, error) {
			switch {
			case !exists:
				fileState = "added"
				return want, true, nil
			case sameText(raw, want):
				fileState = "already present"
				return nil, false, nil
			case hasInstructionsMarker(raw):
				fileState = "replaced"
				return want, true, nil
			}
			fileState = "differs, left as is"
			return nil, false, nil
		},
	}
	config := fileWrite{
		path: filepath.Join(root, openCodeFile),
		merge: func(raw []byte, exists bool) ([]byte, bool, error) {
			doc, err := jsonObject(raw, exists)
			if err != nil {
				return nil, false, err
			}
			mcp, err := member(doc, "mcp")
			if err != nil {
				return nil, false, err
			}
			list, err := stringList(doc, "instructions")
			if err != nil {
				return nil, false, err
			}
			changed := false
			if have, ok := mcp["cairn"]; ok {
				mcpState = "differs, left as is"
				if jsonEqual(have, openCodeEntry()) {
					mcpState = "already present"
				}
			} else {
				entry, err := marshalRaw(openCodeEntry())
				if err != nil {
					return nil, false, err
				}
				mcp["cairn"] = entry
				if doc["mcp"], err = marshalRaw(mcp); err != nil {
					return nil, false, err
				}
				mcpState = "added"
				changed = true
			}
			if slices.Contains(list, openCodeInstructions) {
				listState = "already present"
			} else {
				list = append(list, openCodeInstructions)
				if doc["instructions"], err = marshalRaw(list); err != nil {
					return nil, false, err
				}
				listState = "added"
				changed = true
			}
			if !changed {
				return nil, false, nil
			}
			content, err := encodeSettings(doc)
			return content, err == nil, err
		},
	}
	report := func() string {
		return reportLine("mcp", mcpState, "cairn mcp in "+openCodeFile) +
			reportLine("instructions", listState, openCodeInstructions+" in "+openCodeFile) +
			reportLine("file", fileState, openCodeInstructions)
	}
	// The instruction file is written first, so the config never lists a file that is not there.
	return enableWrites([]fileWrite{file, config}, root, name, dryRun, report, stdout, stderr)
}

// The names enable writes for pi. pi reads .pi/mcp.json only after the project is trusted, and reads AGENTS.md without trust.
const (
	piConfig     = ".pi/mcp.json"
	piAgents     = "AGENTS.md"
	piToolPrefix = "mcp__cairn__"
	piBegin      = "<!-- cairn:begin"
	piEnd        = "<!-- cairn:end -->"
)

// piEntry is the mcpServers entry enable writes for cairn. exposure "direct" declares the tools to the model, as pi's docs
// give it; the default, codemode, would leave them out of the model's view.
func piEntry() map[string]any {
	return map[string]any{"command": "cairn", "args": []string{"mcp"}, "exposure": "direct"}
}

// piNote is the sentence the AGENTS.md block adds after the pi prefix. OpenCode reads AGENTS.md too, so the block also says
// the prefix OpenCode gives the same tools.
const piNote = " In OpenCode they start with cairn_, so there put cairn_ where the steps say mcp__cairn__."

// piBlock is the block enable writes into AGENTS.md: a start marker that says how it came to be there, the task loop with
// the pi prefix and the note for OpenCode, and the end marker. Only the text between the markers is enable's.
func piBlock() string {
	return piBegin + " (written by cairn enable --agent pi, EXPERIMENTAL; do not edit inside) -->\n" + instructionsWith(piToolPrefix, piNote) + piEnd + "\n"
}

// agentsBlock says what state the cairn block of AGENTS.md is in: "absent" when there is no marker, "whole" when there is
// one start marker, then one end marker, and "broken" for anything else: a marker without its partner, a doubled marker,
// or an end before its start.
func agentsBlock(text string) string {
	begins, ends := strings.Count(text, piBegin), strings.Count(text, piEnd)
	switch {
	case begins == 0 && ends == 0:
		return "absent"
	case begins == 1 && ends == 1 && strings.Index(text, piBegin) < strings.Index(text, piEnd):
		return "whole"
	}
	return "broken"
}

// agentsBlockText returns the cairn block of text, from its start marker through its end marker, without the line break
// after the end marker. It is meaningful only when agentsBlock says "whole", and is then the same bytes piBlock gives,
// without its last line break, when the block is current.
func agentsBlockText(text string) string {
	begin := strings.Index(text, piBegin)
	return text[begin : strings.Index(text, piEnd)+len(piEnd)]
}

// enablePi sets up pi for the project at root, EXPERIMENTAL. It adds the cairn server to .pi/mcp.json under mcpServers, with
// exposure "direct", and writes the task loop into AGENTS.md between the two markers. Each file is merged as the others are:
// what is there stays, a cairn server that differs is reported and left, text outside the markers is never rewritten, and a
// whole block is left as it is when it is current. A whole block with an older cairn text between its markers is updated to
// the current text, with a backup first.
func enablePi(root, name string, dryRun bool, stdout, stderr io.Writer) int {
	var serverState, blockState string
	config := fileWrite{
		path: filepath.Join(root, filepath.FromSlash(piConfig)),
		merge: func(raw []byte, exists bool) ([]byte, bool, error) {
			doc, err := jsonObject(raw, exists)
			if err != nil {
				return nil, false, err
			}
			servers, err := member(doc, "mcpServers")
			if err != nil {
				return nil, false, err
			}
			if have, ok := servers["cairn"]; ok {
				serverState = "differs, left as is"
				if jsonEqual(have, piEntry()) {
					serverState = "already present"
				}
				return nil, false, nil
			}
			entry, err := marshalRaw(piEntry())
			if err != nil {
				return nil, false, err
			}
			servers["cairn"] = entry
			if doc["mcpServers"], err = marshalRaw(servers); err != nil {
				return nil, false, err
			}
			serverState = "added"
			content, err := encodeSettings(doc)
			return content, err == nil, err
		},
	}
	agents := fileWrite{
		path: filepath.Join(root, piAgents),
		merge: func(raw []byte, exists bool) ([]byte, bool, error) {
			text := string(raw)
			switch agentsBlock(text) {
			case "whole":
				// A block that is cairn's own, between its markers, is updated when cairn's text has changed. Only those bytes are
				// replaced: the text on either side of the markers is kept as it is.
				current := strings.TrimSuffix(piBlock(), "\n")
				if agentsBlockText(text) == current {
					blockState = "already present"
					return nil, false, nil
				}
				blockState = "updated"
				start := strings.Index(text, piBegin)
				end := start + len(agentsBlockText(text))
				return []byte(text[:start] + current + text[end:]), true, nil
			case "absent":
				// The block goes at the end, after a blank line when the file has text. What is above it is kept as it is.
				blockState = "added"
				if text != "" && !strings.HasSuffix(text, "\n") {
					text += "\n"
				}
				if text != "" {
					text += "\n"
				}
				return []byte(text + piBlock()), true, nil
			}
			return nil, false, errors.New("has a cairn block that is not whole: a start or end marker is missing, doubled, or in the wrong order")
		},
	}
	report := func() string {
		return reportLine("mcp", serverState, piConfig+" mcpServers.cairn") +
			reportLine("instructions", blockState, piAgents+" block") +
			reportLine("trust", "needs trust", "pi reads "+piConfig+" only after the project is trusted")
	}
	return enableWrites([]fileWrite{config, agents}, root, name, dryRun, report, stdout, stderr)
}

// jsonObject decodes raw as the top-level object of a JSON file, keeping each member as its raw JSON. A file that is not
// there is an empty object. JSON with comments is not plain JSON here, so it is refused. The errors complete the sentence
// "<path> ...".
func jsonObject(raw []byte, exists bool) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if !exists {
		return doc, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("is not plain JSON (comments or a syntax error)")
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return nil, errors.New("is not a JSON object")
	}
	return doc, nil
}

// member returns the object under key in doc. A key that is not there gives an empty object, and a key whose value is
// not an object is an error.
func member(doc map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	raw, ok := doc[key]
	if !ok {
		return obj, nil
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("has a %q key that is not a JSON object", key)
	}
	return obj, nil
}

// stringList returns the array of strings under key in doc, or nil when the key is not there. A value of another shape is
// an error.
func stringList(doc map[string]json.RawMessage, key string) ([]string, error) {
	raw, ok := doc[key]
	if !ok {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil || list == nil {
		return nil, fmt.Errorf("has a %q key that is not an array of strings", key)
	}
	return list, nil
}

// jsonFieldsEqual reports whether raw is an object whose fields named in want hold the same JSON data as want's values. A
// field that want does not name is not compared, so a person's own fields in the entry do not count.
func jsonFieldsEqual(raw json.RawMessage, want map[string]any) bool {
	var have map[string]json.RawMessage
	if json.Unmarshal(raw, &have) != nil || have == nil {
		return false
	}
	for key, v := range want {
		h, ok := have[key]
		if !ok || !jsonEqual(h, v) {
			return false
		}
	}
	return true
}

// jsonEqual reports whether raw holds the same JSON data as v, once v is encoded. Key order and layout do not count.
func jsonEqual(raw json.RawMessage, v any) bool {
	want, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var have, expected any
	if json.Unmarshal(raw, &have) != nil || json.Unmarshal(want, &expected) != nil {
		return false
	}
	return reflect.DeepEqual(have, expected)
}
