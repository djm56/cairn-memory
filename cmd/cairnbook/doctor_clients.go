package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/djm56/cairn-memory/internal/identity"
)

// experimentalChecks is the section doctor --agent opencode or pi adds, EXPERIMENTAL: whether the registration and the
// instructions that cairnbook enable writes for that client are in the project. It reads the project's files and never writes
// one. A missing piece is a warning, not a failure: the registrations are optional, as the hooks are. wd is the directory
// the command runs from, which finds the project as the other commands do.
func experimentalChecks(agent, wd string) []check {
	project, err := identity.Resolve(wd)
	if err != nil {
		return []check{{name: "project", status: "skip", detail: "the client's files are not read, because the project could not be found: " + reason(err)}}
	}
	if project.Path == "" {
		return []check{{name: "project", status: "skip", detail: "CAIRNBOOK_PROJECT names the project, but there is no project file to find its files from, so the client's files are not read"}}
	}
	root := filepath.Dir(project.Path)
	if agent == "opencode" {
		return openCodeChecks(root)
	}
	return piChecks(root)
}

// openCodeChecks reads the OpenCode files of the project at root. An opencode.jsonc there is a warning first: OpenCode reads
// it, and enable refuses to write a config beside it. The rest is openCodeConfigChecks.
func openCodeChecks(root string) []check {
	var checks []check
	if _, err := os.Lstat(filepath.Join(root, openCodeJSONC)); err == nil {
		checks = append(checks, check{name: "config", status: "warn", detail: filepath.Join(root, openCodeJSONC) + " is there, and OpenCode reads it: cairnbook enable refuses to write beside it, so add the cairnbook entry to it by hand"})
	}
	return append(checks, openCodeConfigChecks(root)...)
}

// openCodeConfigChecks reads the OpenCode files of the project at root: the cairnbook server under mcp, and the instruction list
// and file. A config that cannot be merged is one warning, with the reason.
func openCodeConfigChecks(root string) []check {
	cfg := filepath.Join(root, openCodeFile)
	doc, err := readObjectFile(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		doc, err = map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return []check{{name: "config", status: "warn", detail: cfg + " " + err.Error()}}
	}
	mcp, err := member(doc, "mcp")
	if err != nil {
		return []check{{name: "config", status: "warn", detail: cfg + " " + err.Error() + tailRefused}}
	}
	list, err := stringList(doc, "instructions")
	if err != nil {
		return []check{{name: "config", status: "warn", detail: cfg + " " + err.Error() + tailRefused}}
	}

	var checks []check
	switch have, ok := mcp["cairnbook"]; {
	case !ok:
		checks = append(checks, check{name: "mcp", status: "warn", detail: "cairnbook is not registered in " + cfg + ": run cairnbook enable --agent opencode"})
	case jsonFieldsEqual(have, openCodeCompared()):
		checks = append(checks, check{name: "mcp", status: "ok", detail: "cairnbook is registered in " + cfg + " as cairnbook mcp"})
	default:
		checks = append(checks, check{name: "mcp", status: "warn", detail: "cairnbook is registered in " + cfg + " with other settings, which enable leaves as they are: edit the entry by hand"})
	}

	instructions := filepath.Join(root, filepath.FromSlash(openCodeInstructions))
	switch {
	case !slices.Contains(list, openCodeInstructions):
		checks = append(checks, check{name: "instructions", status: "warn", detail: openCodeInstructions + " is not listed in " + cfg + ": run cairnbook enable --agent opencode"})
	default:
		checks = append(checks, instructionsFileCheck(instructions, openCodeInstructionsText(), "run cairnbook enable --agent opencode"))
	}
	return checks
}

// instructionsFileCheck reports the instruction file at path against the text enable writes into it. The line endings do not
// count. A file that is not there, or is not readable, is a warning. So is a file with cairnbook's marker and older text, which
// enable replaces, and a file without the marker, which is someone's own and which enable leaves as it is. fix is what the
// warning tells the person to do.
func instructionsFileCheck(path, want, fix string) check {
	raw, err := readCheckedFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return check{name: "instructions", status: "warn", detail: "listed, but " + path + " is not there: " + fix}
	case err != nil:
		return check{name: "instructions", status: "warn", detail: path + " " + err.Error()}
	case sameText(raw, []byte(want)):
		return check{name: "instructions", status: "ok", detail: path + " holds the task loop"}
	case hasInstructionsMarker(raw):
		return check{name: "instructions", status: "warn", detail: path + " holds an older cairnbook task loop, not the one enable writes: " + fix + " to replace it"}
	}
	return check{name: "instructions", status: "warn", detail: path + " holds other text, without cairnbook's marker, so enable leaves it as it is: " + fix + " once it is moved aside"}
}

// piChecks reads the pi files of the project at root: the cairnbook server in .pi/mcp.json, and the block in AGENTS.md.
func piChecks(root string) []check {
	return []check{piServerCheck(root), piBlockCheck(root)}
}

// piServerCheck reports the cairnbook server in the project file .pi/mcp.json: registered with the command, the args and the
// exposure enable writes, registered with other values in those fields, or not registered. Other fields of the entry are not
// compared.
func piServerCheck(root string) check {
	cfg := filepath.Join(root, filepath.FromSlash(piConfig))
	doc, err := readObjectFile(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return check{name: "mcp", status: "warn", detail: "cairnbook is not registered: " + cfg + " is not there, so run cairnbook enable --agent pi"}
	}
	if err != nil {
		return check{name: "mcp", status: "warn", detail: cfg + " " + err.Error()}
	}
	servers, err := member(doc, "mcpServers")
	if err != nil {
		return check{name: "mcp", status: "warn", detail: cfg + " " + err.Error() + tailRefused}
	}
	have, ok := servers["cairnbook"]
	switch {
	case !ok:
		return check{name: "mcp", status: "warn", detail: "cairnbook is not registered in " + cfg + ": run cairnbook enable --agent pi"}
	case jsonFieldsEqual(have, piEntry()):
		return check{name: "mcp", status: "ok", detail: "cairnbook is registered in " + cfg + " with exposure direct; pi reads this file only after the project is trusted"}
	}
	return check{name: "mcp", status: "warn", detail: "cairnbook is registered in " + cfg + " with other settings, which enable leaves as they are: edit the entry by hand"}
}

// piBlockCheck reports the cairnbook block in AGENTS.md: whole and current, whole with an older text, absent, or broken.
func piBlockCheck(root string) check {
	path := filepath.Join(root, piAgents)
	raw, err := readCheckedFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return check{name: "instructions", status: "warn", detail: "no cairnbook block: " + path + " is not there, so run cairnbook enable --agent pi"}
	}
	if err != nil {
		return check{name: "instructions", status: "warn", detail: path + " " + err.Error()}
	}
	text := string(raw)
	switch agentsBlock(text) {
	case "whole":
		if !sameText([]byte(agentsBlockText(text)), []byte(strings.TrimSuffix(piBlock(), "\n"))) {
			return check{name: "instructions", status: "warn", detail: "the cairnbook block in " + path + " is an older text: run cairnbook enable --agent pi to update it"}
		}
		return check{name: "instructions", status: "ok", detail: "the cairnbook block is in " + path}
	case "absent":
		return check{name: "instructions", status: "warn", detail: "no cairnbook block in " + path + ": run cairnbook enable --agent pi"}
	}
	return check{name: "instructions", status: "warn", detail: path + " has a cairnbook block that is not whole: fix it by hand, since enable refuses it"}
}

// readObjectFile reads the JSON file at path, which enable merges, and returns its top-level object. A file that is not
// there gives fs.ErrNotExist unchanged, and any other refusal carries the reason enable would give.
func readObjectFile(path string) (map[string]json.RawMessage, error) {
	raw, err := readCheckedFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := jsonObject(raw, true)
	if err != nil {
		return nil, errors.New(err.Error() + tailRefused)
	}
	return doc, nil
}
