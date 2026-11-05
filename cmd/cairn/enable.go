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
	"os/exec"
	"path/filepath"
	"time"

	"github.com/djm56/cairn-memory/internal/cli"
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

// backupPath is where enable copies a project's settings file before it writes it: the user's cache directory, under
// cairn/backups/<name>, named settings.local.json and the UTC time at. The cache directory is outside the project, so
// a copy of a local settings file, which may hold secrets, is never in the project's tree, where a version control
// command could pick it up. It is an error when the cache directory cannot be found.
func backupPath(name string, at time.Time) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "cairn", "backups", name, settingsName+"."+at.Format(backupStamp)), nil
}

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

// hookResult is what enable found for one hook: added to the file, or already registered under its event. When it
// was already there, matcher is the matcher of the entry that runs it, and differs is set when the hook names a
// matcher and that entry has another. enable reports such an entry and leaves it as it is. A matcher that still
// covers compact (coversCompact) is reported without the word "not", since doctor reads it as a working entry.
type hookResult struct {
	hook    claudeHook
	added   bool
	matcher string
	differs bool
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
	return enableClaudeCode(filepath.Dir(project.Path), project.Name, *dryRun, stdout, stderr)
}

// writeEnableHelp prints the usage of cairn enable, and its flags, to w.
func writeEnableHelp(w io.Writer, fl *flag.FlagSet) {
	fmt.Fprintln(w, "Usage: cairn enable --agent <name> [--dry-run]")
	fmt.Fprintln(w, "Installs the cairn hooks into the project's Claude Code settings: .claude/settings.local.json in the project directory.")
	fmt.Fprintln(w, "It adds only the hooks whose command is not registered under their event yet, and keeps every other key and entry.")
	fmt.Fprintln(w, "The file is rewritten in a new layout: its top-level keys are sorted and its whitespace is laid out again. The backup holds the original text.")
	fmt.Fprintln(w, "The hooks run cairn by name, so cairn must be on the PATH Claude Code uses; enable warns when it is not on its own PATH.")
	fmt.Fprintln(w, "Before it writes an existing file, it copies the original to cairn/backups/<project> in the user's cache directory (~/Library/Caches on macOS, ~/.cache on Linux), outside the project, and prints the path.")
	fmt.Fprintln(w, "A file that is not valid JSON is refused, and left as it is. If the cache directory cannot be found, it refuses and changes nothing.")
	fmt.Fprintln(w, "Agents: claude-code. opencode and pi are not built yet.")
	fl.SetOutput(w)
	fl.PrintDefaults()
}

// enableClaudeCode merges the Claude Code hooks into the settings file of the project called name, at root. The file
// is read and merged in memory first, so a refusal leaves it as it was. A write copies the original to a backup
// outside the project (backupPath), then puts the new file in place with one rename. Dry run prints the same lines
// and writes nothing.
func enableClaudeCode(root, name string, dryRun bool, stdout, stderr io.Writer) int {
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
	var read fs.FileInfo
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
		// The rename must find the file as the stat before the read saw it. A save between that stat and the read is then
		// a change, and the rename is refused. A stat taken after the read would record such a save as the baseline.
		read = info
		mode = info.Mode().Perm()
	}

	doc, results, changed, err := addClaudeHooks(raw, exists, claudeHooks)
	if err != nil {
		fmt.Fprintf(stderr, "cairn enable: %s %v. Nothing was changed: fix the file by hand, then run cairn enable again\n", path, err)
		return 1
	}
	// The new content and the backup are made before anything is printed or written to the project, so a refusal here
	// reports nothing as done. The backup's directory is made before the project's directory, so a failed backup
	// leaves the project's directory as it was.
	var content []byte
	var backup string
	if changed && !dryRun {
		if content, err = encodeSettings(doc); err != nil {
			fmt.Fprintf(stderr, "cairn enable: encode %s: %v; nothing was changed\n", path, err)
			return 1
		}
		if exists {
			if backup, err = backupPath(name, enableNow()); err != nil {
				fmt.Fprintf(stderr, "cairn enable: find a place for the backup of %s: %v; nothing was changed\n", path, err)
				return 1
			}
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				fmt.Fprintf(stderr, "cairn enable: create %s: %v; nothing was changed\n", filepath.Dir(backup), err)
				return 1
			}
			if err := copyNew(backup, raw); err != nil {
				fmt.Fprintf(stderr, "cairn enable: back up %s: %v; nothing was changed\n", path, err)
				return 1
			}
		}
	}
	// The per-hook lines are built now and printed only when the outcome is known: on the unchanged and dry-run paths,
	// and after the rename. A refusal after this point therefore leaves stdout empty, and stderr says why.
	lines := hookLines(results)
	if !changed {
		fmt.Fprint(stdout, lines)
		fmt.Fprintf(stdout, "unchanged: %s\n", path)
		warnIfNotOnPath(stderr)
		return 0
	}
	if dryRun {
		fmt.Fprint(stdout, lines)
		fmt.Fprintf(stdout, "dry run: nothing written to %s\n", path)
		warnIfNotOnPath(stderr)
		return 0
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "cairn enable: create %s: %v\n", dir, err)
		return 1
	}
	if err := replaceFile(path, content, mode, exists, unchangedCheck(path, read)); err != nil {
		if errors.Is(err, errChanged) {
			if backup != "" {
				fmt.Fprintf(stderr, "cairn enable: %s %v: nothing was replaced, and the original is kept at %s; run cairn enable again\n", path, err, backup)
			} else {
				fmt.Fprintf(stderr, "cairn enable: %s %v: nothing was replaced; run cairn enable again\n", path, err)
			}
			return 1
		}
		fmt.Fprintf(stderr, "cairn enable: write %s: %v; the file was not replaced\n", path, err)
		return 1
	}
	fmt.Fprint(stdout, lines)
	fmt.Fprintf(stdout, "wrote %s\n", path)
	if backup != "" {
		fmt.Fprintf(stdout, "backup %s\n", backup)
	}
	warnIfNotOnPath(stderr)
	return 0
}

// hookLines is the line enable prints for each hook, one per result and in order: the event, what became of its hook, and
// the command. The lines are text, so the caller decides when they are printed.
func hookLines(results []hookResult) string {
	var b bytes.Buffer
	for _, r := range results {
		state := "already present"
		switch {
		case r.added:
			state = "added"
		case r.differs && coversCompact(r.matcher):
			// The entry still does what the hook is for, so it is named, and not called a fault.
			state = fmt.Sprintf("already present, matcher is %s", shownMatcher(r.matcher))
		case r.differs:
			state = fmt.Sprintf("already present, matcher is %s, not %s", shownMatcher(r.matcher), r.hook.matcher)
		}
		fmt.Fprintf(&b, "%-12s %-17s %s\n", r.hook.event, state, r.hook.cmdline)
	}
	return b.String()
}

// warnIfNotOnPath warns on stderr when cairn is not on the PATH of this shell. The hooks run the bare command cairn,
// so the client that runs them must find it on its own PATH. enable cannot see that PATH, so the warning is only a
// guide. It does not change the exit code.
func warnIfNotOnPath(stderr io.Writer) {
	if _, err := exec.LookPath("cairn"); err != nil {
		fmt.Fprintln(stderr, "cairn enable: warning: cairn is not on the PATH of this shell; the hooks run cairn by name, so put the binary on the PATH Claude Code uses")
	}
}

// addClaudeHooks merges hooks into the settings document in raw, which is the file as it is when exists is true.
// It returns the document with the missing entries added, one result per hook, and whether anything was added. An
// error means the file cannot be merged, and nothing is returned to write. The error text completes the sentence
// "<path> ...". Every key and entry enable does not add to is kept as its raw JSON, so its value is unchanged.
//
// A top-level key that appears twice in raw keeps its last value, since the document is decoded into a map. The new
// file holds that last value only. The backup holds the original text, with both.
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
		if matchers := commandMatchers(entries, h.cmdline); len(matchers) > 0 {
			results = append(results, hookResult{hook: h, matcher: matchers[0], differs: h.matcher != "" && matchers[0] != h.matcher})
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

// commandMatchers returns the matcher of each entry that already runs cmdline in one of its handlers, in order. The
// test is on the command string alone, so an entry with another matcher still counts. An entry with no matcher gives
// "". An entry or a handler that is not shaped as the settings format gives it is not a match, and is left as it is.
func commandMatchers(entries []json.RawMessage, cmdline string) []string {
	var matchers []string
	for _, raw := range entries {
		var entry struct {
			Matcher any               `json:"matcher"`
			Hooks   []json.RawMessage `json:"hooks"`
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
				matcher, _ := entry.Matcher.(string)
				matchers = append(matchers, matcher)
				break
			}
		}
	}
	return matchers
}

// shownMatcher is a matcher as the output names it. An entry with no matcher matches every occurrence, so it is
// shown as (none). A matcher is read from a settings file, which a cloned repository may supply, so it is folded to
// one printable line (cli.Plain) before it is printed. A matcher that folds to nothing is shown as (unprintable).
func shownMatcher(matcher string) string {
	if matcher == "" {
		return "(none)"
	}
	if shown := cli.Plain(matcher); shown != "" {
		return shown
	}
	return "(unprintable)"
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

// copyNew creates path with content at mode 0600, whatever the mode of the file it copies. A backup holds the settings
// as they were, and they may hold secrets, so it is never readable by other users. It refuses a path that exists, so an
// earlier backup is never replaced. A failure after the file is made removes it.
func copyNew(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := finishFile(f, content); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// errChanged is what the check before the rename returns when the settings file changed since enable read it. The
// rename would then replace another writer's save, so it is not made.
var errChanged = errors.New("it changed after enable read it")

// beforeRename runs after the new file is synced and before the check and the rename. It is a variable so a test can
// change the settings file at that moment.
var beforeRename = func() {}

// renameFile is the rename that puts the new file in place. It is a variable so a test can make it fail.
var renameFile = os.Rename

// unchangedCheck returns the check replaceFile runs before its rename. The file must still be the one enable read: the
// same size and modification time, or still absent when read is nil, which is when enable found no file. A file that
// is there now, when none was read, or that is gone, or that has changed, is errChanged, so nothing is replaced.
func unchangedCheck(path string, read fs.FileInfo) func() error {
	return func() error {
		now, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			if read == nil {
				return nil
			}
			return errChanged
		}
		if err != nil {
			return err
		}
		if read == nil || !now.Mode().IsRegular() || now.Size() != read.Size() || !now.ModTime().Equal(read.ModTime()) {
			return errChanged
		}
		return nil
	}
}

// replaceFile puts content at path with one rename: the new file is written to a temporary file in the same
// directory, made durable, and renamed over path. So a crash leaves the old file or the new one, never a part of
// either. A file that already exists keeps its mode, since keepMode is set. Its temporary file is made 0600 and is
// given that mode only after the content is synced, so the content is never readable more widely than the file
// ends up. A new file is made at 0644 before the umask, which is its final mode, as cairn init makes its file.
// unchanged runs after the content is synced and before the rename. When it fails, nothing is renamed, and the
// temporary file is removed.
func replaceFile(path string, content []byte, mode fs.FileMode, keepMode bool, unchanged func() error) error {
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
	beforeRename()
	if err := unchanged(); err != nil {
		return err
	}
	if err := renameFile(tmp, path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}
