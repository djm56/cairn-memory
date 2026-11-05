package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// instructionsTemplate is the task loop an experimental client is given. {prefix} stands where the client names cairn's
// tools: OpenCode and pi each name an MCP server's tools in their own way, so instructionsFor fills it per client.
//
//go:embed enable_instructions.md
var instructionsTemplate string

// The names enable writes for OpenCode. The config lists the instruction file relative to the project, and the file is
// written at the same place.
const (
	openCodeFile         = "opencode.json"
	openCodeInstructions = ".cairn/instructions.md"
	openCodeToolPrefix   = "cairn_"
	openCodeTimeoutMS    = 30000
)

// openCodeEntry is the mcp entry enable writes for the cairn server. DESIGN §6.2 asks for a timeout above the 5 s default:
// the OpenCode docs page gives 30 s as the default, while the schema gives 5 s, so 30 s is written here.
func openCodeEntry() map[string]any {
	return map[string]any{"type": "local", "command": []string{"cairn", "mcp"}, "timeout": openCodeTimeoutMS}
}

// instructionsFor returns the task loop with the client's tool prefix where it names a tool.
func instructionsFor(prefix string) string {
	return strings.ReplaceAll(instructionsTemplate, "{prefix}", prefix)
}

// enableOpenCode sets up OpenCode for the project at root, EXPERIMENTAL. It adds the cairn MCP server to opencode.json
// under "mcp", lists the instruction file in its "instructions", and writes that file. Each is merged as enable merges
// Claude Code's settings: what is there stays, a cairn entry that differs is reported and left, and a file is written only
// when it changes. An instruction file that is there with other content is refused, since enable did not make it.
func enableOpenCode(root, name string, dryRun bool, stdout, stderr io.Writer) int {
	var mcpState, listState, fileState string
	want := []byte(instructionsFor(openCodeToolPrefix))
	file := fileWrite{
		path: filepath.Join(root, filepath.FromSlash(openCodeInstructions)),
		merge: func(raw []byte, exists bool) ([]byte, bool, error) {
			switch {
			case !exists:
				fileState = "added"
				return want, true, nil
			case bytes.Equal(raw, want):
				fileState = "already present"
				return nil, false, nil
			}
			return nil, false, errors.New("has other content, which enable does not write over")
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
	return enableWrites([]fileWrite{file, config}, name, dryRun, report, stdout, stderr)
}

// jsonObject decodes raw as the top-level object of a JSON file, keeping each member as its raw JSON. A file that is not
// there is an empty object. JSON with comments is not valid JSON here, so it is refused. The errors complete the sentence
// "<path> ...".
func jsonObject(raw []byte, exists bool) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if !exists {
		return doc, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("is not valid JSON")
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
