package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/djm56/cairn-memory/internal/identity"
)

// settingsDir and settingsName are where enable writes a Claude Code project's hooks: the local settings file
// of the project, which Claude Code does not commit (DESIGN.md §4.1; docs/PLAN.md D15).
const (
	settingsDir  = ".claude"
	settingsName = "settings.local.json"
)

// backupStamp is the layout of the UTC time in a backup's name. It has no colons, so the name is valid on every
// system.
const backupStamp = "20060102T150405Z"

// enableNow is the clock for a backup's name. It is a variable so a test can name the backup at a time it
// chooses.
var enableNow = func() time.Time { return time.Now().UTC() }

// claudeHook is one hook enable installs for Claude Code: the event it is registered under, the matcher that
// narrows the event (empty for none), and the command line the hook runs.
type claudeHook struct {
	event   string
	matcher string
	cmdline string
}

// claudeHooks are the hooks enable installs for Claude Code, in the order it adds them (D15).
//
// PreCompact is registered because D15 names it, but it does nothing in Claude Code. The hooks reference says that
// a PreCompact hook's plain stdout is written to the debug log, not the transcript, and that its systemMessage is
// discarded. So the block that cairn hook pre-compact prints never reaches the model. A task survives compaction
// through SessionStart: the compact matcher runs cairn hook session-start again after /compact, and that command's
// stdout is added to the model's context.
var claudeHooks = []claudeHook{
	{event: "SessionStart", matcher: "startup|resume|clear|compact", cmdline: "cairn hook session-start"},
	{event: "PreCompact", cmdline: "cairn hook pre-compact"},
	{event: "Stop", cmdline: "cairn hook stop"},
}

// hookResult is what enable found for one hook: added to the file, or already registered under its event.
type hookResult struct {
	hook  claudeHook
	added bool
}

// hookEntry and hookCommand are the shape the settings format gives an entry under an event: a matcher, and a list
// of handlers. enable writes its own entries in this shape. Entries it did not write are kept as raw JSON.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// hookCommand is one command handler of an entry.
type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// runEnable is the sub-command `cairn enable --agent <name> [--dry-run]` (DESIGN.md §5.2). It installs the hooks a
// client needs into the project's settings. Only claude-code is built. It changes nothing that is already there:
// each hook is added only when its command is not registered under its event yet.
//
// The project is found as the other commands find it, from its .cairn.toml, and the settings file is in the
// directory of that file. Exit codes: 0 when the hooks are in place, or a dry run was done; 1 when the project is
// not found or the settings file cannot be read, merged or written; 2 for a usage error.
func runEnable(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn enable", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	agent := fl.String("agent", "", "the client to install hooks for: claude-code")
	dryRun := fl.Bool("dry-run", false, "print what would be added and write nothing")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeEnableHelp(stdout, fl)
			return 0
		}
		fmt.Fprintf(stderr, "cairn enable: %v; run cairn enable --help for the flags\n", err)
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn enable: takes flags only, got %q\n", fl.Args())
		return 2
	}
	switch *agent {
	case "":
		fmt.Fprintln(stderr, "cairn enable: needs --agent <name>: use --agent claude-code")
		return 2
	case "claude-code":
	case "opencode", "pi":
		fmt.Fprintf(stderr, "cairn enable: --agent %s is not built yet: use --agent claude-code\n", *agent)
		return 2
	default:
		fmt.Fprintf(stderr, "cairn enable: unknown agent %q: use --agent claude-code\n", *agent)
		return 2
	}

	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn enable: the working directory: %v\n", err)
		return 1
	}
	project, err := identity.Resolve(wd)
	if err != nil {
		fmt.Fprintf(stderr, "cairn enable: %s\n", reason(err))
		return 1
	}
	if project.Path == "" {
		// CAIRN_PROJECT names the project, but the project directory is found from the file, and there is none.
		fmt.Fprintln(stderr, "cairn enable: CAIRN_PROJECT names the project, but the project directory is found from its .cairn.toml, and there is none: run cairn enable from the project, or unset CAIRN_PROJECT")
		return 1
	}
	return enableClaudeCode(filepath.Dir(project.Path), *dryRun, stdout, stderr)
}

// writeEnableHelp prints the usage of cairn enable, and its flags, to w.
func writeEnableHelp(w io.Writer, fl *flag.FlagSet) {
	fmt.Fprintln(w, "Usage: cairn enable --agent <name> [--dry-run]")
	fmt.Fprintln(w, "Installs the cairn hooks into the project's Claude Code settings: .claude/settings.local.json in the project directory.")
	fmt.Fprintln(w, "It adds only the hooks whose command is not registered under their event yet, and keeps every other key and entry.")
	fmt.Fprintln(w, "Before it writes, it copies the file to settings.local.json.bak-<UTC time>. A file that is not valid JSON is refused, and left as it is.")
	fmt.Fprintln(w, "Agents: claude-code. opencode and pi are not built yet.")
	fl.SetOutput(w)
	fl.PrintDefaults()
}

// enableClaudeCode merges the Claude Code hooks into the settings file of the project at root. The file is read and
// merged in memory first, so a refusal leaves it as it was. A write copies the original to a backup, then puts the
// new file in place with one rename. Dry run prints the same lines and writes nothing.
func enableClaudeCode(root string, dryRun bool, stdout, stderr io.Writer) int {
	dir := filepath.Join(root, settingsDir)
	path := filepath.Join(dir, settingsName)
	info, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "cairn enable: look for %s: %v\n", path, err)
		return 1
	}
	mode := fs.FileMode(0o644)
	var raw []byte
	if exists {
		// A link is not followed: a rename over it would replace the link with a file, so the file it names is not touched.
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			fmt.Fprintf(stderr, "cairn enable: %s is a symbolic link: enable does not write through a link, so edit the file it names by hand\n", path)
			return 1
		case !info.Mode().IsRegular():
			fmt.Fprintf(stderr, "cairn enable: %s is not a regular file\n", path)
			return 1
		}
		raw, err = os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "cairn enable: read %s: %v\n", path, err)
			return 1
		}
		mode = info.Mode().Perm()
	}

	doc, results, changed, err := addClaudeHooks(raw, exists, claudeHooks)
	if err != nil {
		fmt.Fprintf(stderr, "cairn enable: %s %v. Nothing was changed: fix the file by hand, then run cairn enable again\n", path, err)
		return 1
	}
	for _, r := range results {
		state := "already present"
		if r.added {
			state = "added"
		}
		fmt.Fprintf(stdout, "%-12s %-17s %s\n", r.hook.event, state, r.hook.cmdline)
	}
	if !changed {
		fmt.Fprintf(stdout, "unchanged: %s\n", path)
		return 0
	}
	if dryRun {
		fmt.Fprintf(stdout, "dry run: nothing written to %s\n", path)
		return 0
	}

	content, err := encodeSettings(doc)
	if err != nil {
		fmt.Fprintf(stderr, "cairn enable: encode %s: %v\n", path, err)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "cairn enable: create %s: %v\n", dir, err)
		return 1
	}
	var backup string
	if exists {
		backup = path + ".bak-" + enableNow().Format(backupStamp)
		if err := copyNew(backup, raw, mode); err != nil {
			fmt.Fprintf(stderr, "cairn enable: back up %s: %v; nothing was changed\n", path, err)
			return 1
		}
	}
	if err := replaceFile(path, content, mode, exists); err != nil {
		fmt.Fprintf(stderr, "cairn enable: write %s: %v; the file was not replaced\n", path, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	if backup != "" {
		fmt.Fprintf(stdout, "backup %s\n", backup)
	}
	return 0
}

// addClaudeHooks merges hooks into the settings document in raw, which is the file as it is when exists is true.
// It returns the document with the missing entries added, one result per hook, and whether anything was added. An
// error means the file cannot be merged, and nothing is returned to write. The error text completes the sentence
// "<path> ...". Every key and entry enable does not add to is kept as its raw JSON, so its value is unchanged.
func addClaudeHooks(raw []byte, exists bool, hooks []claudeHook) (map[string]json.RawMessage, []hookResult, bool, error) {
	doc := map[string]json.RawMessage{}
	if exists {
		if !json.Valid(raw) {
			return nil, nil, false, errors.New("is not valid JSON")
		}
		if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
			return nil, nil, false, errors.New("is not a JSON object")
		}
	}
	byEvent := map[string]json.RawMessage{}
	if hooksRaw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &byEvent); err != nil || byEvent == nil {
			return nil, nil, false, errors.New(`has a "hooks" key that is not a JSON object`)
		}
	}

	var results []hookResult
	changed := false
	for _, h := range hooks {
		entries := []json.RawMessage{}
		if eventRaw, ok := byEvent[h.event]; ok {
			if err := json.Unmarshal(eventRaw, &entries); err != nil || entries == nil {
				return nil, nil, false, fmt.Errorf(`has a "hooks" entry for %s that is not a JSON array`, h.event)
			}
		}
		if hasCommand(entries, h.cmdline) {
			results = append(results, hookResult{hook: h})
			continue
		}
		entry, err := marshalRaw(hookEntry{Matcher: h.matcher, Hooks: []hookCommand{{Type: "command", Command: h.cmdline}}})
		if err != nil {
			return nil, nil, false, err
		}
		entries = append(entries, entry)
		eventBytes, err := marshalRaw(entries)
		if err != nil {
			return nil, nil, false, err
		}
		byEvent[h.event] = eventBytes
		results = append(results, hookResult{hook: h, added: true})
		changed = true
	}
	if !changed {
		return doc, results, false, nil
	}
	hooksBytes, err := marshalRaw(byEvent)
	if err != nil {
		return nil, nil, false, err
	}
	doc["hooks"] = hooksBytes
	return doc, results, true, nil
}

// hasCommand reports whether one of entries already runs cmdline, in any of its handlers. The test is on the command
// string alone, so an entry with another matcher still counts. An entry or a handler that is not shaped as the
// settings format gives it is not a match, and is left as it is.
func hasCommand(entries []json.RawMessage, cmdline string) bool {
	for _, raw := range entries {
		var entry struct {
			Hooks []json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		for _, handler := range entry.Hooks {
			var h struct {
				Command any `json:"command"`
			}
			if json.Unmarshal(handler, &h) != nil {
				continue
			}
			if s, ok := h.Command.(string); ok && s == cmdline {
				return true
			}
		}
	}
	return false
}

// marshalRaw encodes v as compact JSON, without escaping <, > and &, so a value enable copies keeps its characters.
// The encoder's trailing newline is left out.
func marshalRaw(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// encodeSettings writes the settings document as indented JSON, with a newline at the end. The encoder sorts the
// top-level keys. The values keep their characters, but their whitespace is laid out again.
func encodeSettings(doc map[string]json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// copyNew creates path with content and mode. It refuses a path that exists, so an earlier backup is never replaced.
// A failure after the file is made removes it.
func copyNew(path string, content []byte, mode fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = finishFile(f, content)
	if err == nil {
		err = os.Chmod(path, mode)
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// replaceFile puts content at path with one rename: the new file is written to a temporary file in the same
// directory, made durable, and renamed over path. So a crash leaves the old file or the new one, never a part of
// either. A file that already exists keeps its mode, since keepMode is set. Its temporary file is made 0600 and is
// given that mode only after the content is synced, so the content is never readable more widely than the file
// ends up. A new file is made at 0644 before the umask, which is its final mode, as cairn init makes its file.
func replaceFile(path string, content []byte, mode fs.FileMode, keepMode bool) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, "."+settingsName+"."+rand.Text()+".tmp")
	createMode := fs.FileMode(0o644)
	if keepMode {
		createMode = 0o600
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, createMode)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := finishFile(f, content); err != nil {
		return err
	}
	if keepMode {
		if err := os.Chmod(tmp, mode); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}
