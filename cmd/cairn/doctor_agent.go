package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/djm56/cairn-memory/internal/cli"
	"github.com/djm56/cairn-memory/internal/identity"
)

// settingsFileName is the shared settings file of a project, which Claude Code may commit. enable does not write it,
// but a hook installed there runs, so doctor reads it.
const settingsFileName = "settings.json"

// installedHook is one cairn hook found in a settings file: the file, the event it is registered under, and the
// matcher of the entry that holds it. An entry with no matcher has "".
type installedHook struct {
	path    string
	event   string
	matcher string
}

// claudeCodeChecks is the section cairn doctor --agent claude-code adds after its own checks. It reads the Claude Code
// settings files that can hold cairn's hooks, and never writes one: the project's .claude/settings.local.json, then the
// project's .claude/settings.json, then the user's ~/.claude/settings.json. It reports each cairn hook it finds under
// its event, with the file it is in and its matcher. A hook that is not installed is a warning, not a failure: the
// hooks are optional, and a project that has not run enable is not broken. A file that enable would refuse is a
// warning with its reason, and the other files are still read. wd is the directory the command runs from, which finds
// the project as the other commands do.
func claudeCodeChecks(wd string) []check {
	var checks []check
	var files []string
	if project, err := identity.Resolve(wd); err == nil && project.Path != "" {
		root := filepath.Dir(project.Path)
		files = append(files, filepath.Join(root, settingsDir, settingsName), filepath.Join(root, settingsDir, settingsFileName))
	} else {
		checks = append(checks, check{name: "settings", status: "skip", detail: "no project file, so the project's settings are not read"})
	}
	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".claude", settingsFileName))
	} else {
		checks = append(checks, check{name: "settings", status: "skip", detail: "no home directory, so the user's settings are not read"})
	}

	var found []installedHook
	for _, path := range files {
		hooks, err := readSettingsHooks(path)
		if err != nil {
			checks = append(checks, check{name: "settings", status: "warn", detail: path + " " + err.Error() + ": cairn enable refuses it and leaves it as it is"})
			continue
		}
		found = append(found, hooks...)
	}
	for _, h := range claudeHooks {
		count := 0
		for _, f := range found {
			if f.event == h.event {
				checks = append(checks, hookLine(f))
				count++
			}
		}
		if count == 0 {
			checks = append(checks, check{name: h.event, status: "warn", detail: "not installed in any settings file read: run cairn enable --agent claude-code"})
		}
	}
	return checks
}

// dryRunChecks runs each cairn hook once, in-process, and reports what each printed (dryRunCheck). The hooks read the
// active task and write nothing, so the dry run writes nothing either.
func dryRunChecks() []check {
	checks := make([]check, 0, len(claudeHooks))
	for _, h := range claudeHooks {
		checks = append(checks, dryRunCheck(h))
	}
	return checks
}

// dryRunCheck runs one cairn hook through the same code its client runs, with stdout and stderr captured, against the
// server the command is configured for. It reports the exit code and the byte count of what the hook printed, and the
// first line of it, folded to one printable line: the rest of the output, the context a hook adds, is not printed. The
// hook exits 0 whatever happens, so any other exit code is a fault. stop must print one JSON object with a systemMessage,
// or nothing, and that is checked. A hook that prints nothing and says why on stderr is a warning, since a hook that
// cannot reach the server prints nothing.
func dryRunCheck(h claudeHook) check {
	arg := strings.TrimPrefix(h.cmdline, "cairn hook ")
	var stdout, stderr bytes.Buffer
	code := runHook([]string{arg}, &stdout, &stderr)
	out := stdout.Bytes()

	status := "ok"
	parts := []string{fmt.Sprintf("%s: exit %d, %d bytes", arg, code, len(out))}
	switch {
	case code != 0:
		status = "fail"
		parts = append(parts, "the hook exits 0 whatever happens, so this exit is a fault")
	case arg == "stop" && len(out) > 0 && stopOutputOK(out):
		parts = append(parts, "valid JSON with systemMessage")
	case arg == "stop" && len(out) > 0:
		status = "fail"
		parts = append(parts, "not one JSON object with a systemMessage")
	case len(out) == 0 && stderr.Len() == 0:
		parts = append(parts, "nothing printed")
	case len(out) == 0:
		status = "warn"
		parts = append(parts, "nothing printed")
	}
	if len(out) > 0 {
		parts = append(parts, "first line: "+cli.Plain(firstLine(out)))
	}
	if stderr.Len() > 0 {
		parts = append(parts, "stderr: "+cli.Plain(firstLine(stderr.Bytes())))
	}
	return check{name: "dry run", status: status, detail: strings.Join(parts, ", ")}
}

// stopOutputOK reports whether out is one JSON object whose systemMessage is text that is not empty. Claude Code shows
// that message to the user, so it is the one thing a stop hook must print.
func stopOutputOK(out []byte) bool {
	var v struct {
		SystemMessage *string `json:"systemMessage"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &v) != nil {
		return false
	}
	return v.SystemMessage != nil && *v.SystemMessage != ""
}

// firstLine returns the text of b up to its first line break.
func firstLine(b []byte) string {
	line, _, _ := strings.Cut(string(b), "\n")
	return line
}

// readSettingsHooks reads the settings file at path and returns each cairn hook under the three events. A file that is
// not there has none. The refusals are the ones enable makes before it merges, so a file enable would refuse gets the
// same reason here. An error's text completes the sentence "<path> ...". A file that cannot be read says so, with the
// error. Entries that are not shaped as the settings format gives them are not matches, as in enable.
func readSettingsHooks(path string) ([]installedHook, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not be read: %w", err)
	}
	if !json.Valid(raw) {
		return nil, errors.New("is not valid JSON")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return nil, errors.New("is not a JSON object")
	}
	var byEvent map[string]json.RawMessage
	if hooksRaw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &byEvent); err != nil || byEvent == nil {
			return nil, errors.New(`has a "hooks" key that is not a JSON object`)
		}
	}
	var found []installedHook
	for _, h := range claudeHooks {
		eventRaw, ok := byEvent[h.event]
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(eventRaw, &entries); err != nil || entries == nil {
			return nil, fmt.Errorf(`has a "hooks" entry for %s that is not a JSON array`, h.event)
		}
		for _, matcher := range commandMatchers(entries, h.cmdline) {
			found = append(found, installedHook{path: path, event: h.event, matcher: matcher})
		}
	}
	return found, nil
}

// hookLine is the line for one cairn hook found in a settings file. A SessionStart hook whose matcher does not name
// compact is a warning, since compact is what carries a task across /compact, and enable leaves an existing entry as
// it is, so the matcher has to be edited by hand. PreCompact is installed per D15, but Claude Code does not show its
// output, so the line says that is an operator decision.
func hookLine(f installedHook) check {
	where := "installed in " + f.path + ", "
	matcher := "no matcher"
	if f.matcher != "" {
		matcher = "matcher " + f.matcher
	}
	switch {
	case f.event == "SessionStart" && !coversCompact(f.matcher):
		return check{name: f.event, status: "warn", detail: where + "matcher " + shownMatcher(f.matcher) + " does not name compact, so the task is not carried across /compact: edit the matcher to startup|resume|clear|compact, since enable leaves an existing entry as it is"}
	case f.event == "PreCompact":
		return check{name: f.event, status: "ok", detail: where + matcher + "; installed per D15, but Claude Code does not show its output, so this is an operator decision"}
	}
	return check{name: f.event, status: "ok", detail: where + matcher}
}

// coversCompact reports whether a session-start matcher lets the hook run after /compact. A matcher that is omitted, or
// is "" or "*", matches every source. A matcher made only of names, with | or , between them, matches the names it
// lists. Any other matcher is a pattern doctor does not evaluate, so it does not count as covering compact.
func coversCompact(matcher string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	for _, r := range matcher {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == ' ' || r == ',' || r == '|') {
			return false
		}
	}
	for _, name := range strings.FieldsFunc(matcher, func(r rune) bool { return r == '|' || r == ',' }) {
		if strings.TrimSpace(name) == "compact" {
			return true
		}
	}
	return false
}
