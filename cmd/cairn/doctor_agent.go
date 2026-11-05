package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/djm56/cairn-memory/internal/cli"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// settingsFileName is the shared settings file of a project, which Claude Code may commit. enable does not write it,
// but a hook installed there runs, so doctor reads it.
const settingsFileName = "settings.json"

// settingsFile is one settings file doctor reads: its path, and whether it is the user's file, which doctor follows through a
// link and whose refusals carry no note that enable refuses it, since enable never writes it.
type settingsFile struct {
	path string
	user bool
}

// installedHook is one cairn hook found in a settings file: the file, the event it is registered under, and the
// matcher of the entry that holds it. An entry with no matcher has "". notString is set when the entry has a matcher
// that is not a string, which the settings format does not allow: doctor cannot read such a matcher, and says so.
type installedHook struct {
	path      string
	event     string
	matcher   string
	notString bool
}

// claudeCodeChecks is the section cairn doctor --agent claude-code adds after its own checks. It reads the Claude Code
// settings files that can hold cairn's hooks, and never writes one: the project's .claude/settings.local.json, then the
// project's .claude/settings.json, then the user's ~/.claude/settings.json. It reports each cairn hook it finds under
// its event, with the file it is in and its matcher. A hook that is not installed is a warning, not a failure: the
// hooks are optional, and a project that has not run enable is not broken. A file that enable would refuse is a
// warning with its reason, and the other files are still read. A project file that is a link, is not a regular file, or is
// larger than maxSettingsBytes is not read at all. The user's file is the exception to the link rule: it is followed, since
// the user owns that path and a dotfiles link is common, and its target must still be a regular file under the cap. wd is
// the directory the command runs from, which finds the project as the other commands do.
func claudeCodeChecks(wd string) []check {
	var checks []check
	var files []settingsFile
	project, err := identity.Resolve(wd)
	switch {
	case err == nil && project.Path != "":
		root := filepath.Dir(project.Path)
		files = append(files, settingsFile{path: filepath.Join(root, settingsDir, settingsName)}, settingsFile{path: filepath.Join(root, settingsDir, settingsFileName)})
	case err == nil:
		// CAIRN_PROJECT names the project, and the project directory is found from a file, so with no file there is none.
		checks = append(checks, check{name: "settings", status: "skip", detail: "CAIRN_PROJECT names the project, but there is no project file to find its settings from, so the project's settings are not read"})
	case errors.Is(err, core.ErrNoProject):
		checks = append(checks, check{name: "settings", status: "skip", detail: "no project file, so the project's settings are not read"})
	default:
		checks = append(checks, check{name: "settings", status: "skip", detail: "the project file could not be read, so the project's settings are not read: " + reason(err)})
	}
	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, settingsFile{path: filepath.Join(home, ".claude", settingsFileName), user: true})
	} else {
		checks = append(checks, check{name: "settings", status: "skip", detail: "no home directory, so the user's settings are not read"})
	}

	var found []installedHook
	for _, f := range files {
		hooks, err := readSettingsHooks(f.path, f.user)
		if err != nil {
			checks = append(checks, check{name: "settings", status: "warn", detail: f.path + " " + err.Error()})
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
			checks = append(checks, absentHookLine(h))
		}
	}
	return checks
}

// absentHookLine is the line for a cairn hook that no settings file read holds. A missing hook is a warning, and enable
// is the fix. PreCompact is an operator decision (D15), so its absence is ok, with the same note hookLine gives.
func absentHookLine(h claudeHook) check {
	if h.event == "PreCompact" {
		return check{name: h.event, status: "ok", detail: "not installed in any settings file read; installed per D15, but Claude Code does not show its output, so this is an operator decision"}
	}
	return check{name: h.event, status: "warn", detail: "not installed in any settings file read: run cairn enable --agent claude-code"}
}

// dryRunChecks runs each cairn hook once, in-process, and reports what each printed (dryRunCheck). The hooks read the
// active task and write no file. The server may still create the project's row on the first read, as the hooks do
// (TaskCurrent), so the dry run is not read-only on the server.
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
	return judgeDryRun(arg, code, stdout.Bytes(), stderr.Bytes())
}

// judgeDryRun is the check for one hook's dry run, given the hook's name, its exit code, and what it printed on each
// stream. session-start and pre-compact always print their block when they work, so printing nothing is a warning, with
// stderr or without it. stop prints nothing when it has nothing to say, and that is ok when stderr is empty too.
func judgeDryRun(arg string, code int, out, errOut []byte) check {
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
	case len(out) == 0 && arg != "stop":
		status = "warn"
		parts = append(parts, "nothing printed, but the block is always printed when the hook works")
	case len(out) == 0 && len(errOut) > 0:
		status = "warn"
		parts = append(parts, "nothing printed")
	case len(out) == 0:
		parts = append(parts, "nothing printed")
	}
	if len(out) > 0 {
		parts = append(parts, "first line: "+cli.Plain(firstLine(out)))
	}
	if len(errOut) > 0 {
		parts = append(parts, "stderr: "+cli.Plain(firstLine(errOut)))
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

// maxSettingsBytes caps the read of one settings file. A settings file is a few kilobytes, so a larger one is not read,
// and the check says so.
const maxSettingsBytes = 1 << 20

// tailRefused ends the message for a settings file that enable refuses as well, so the check says what enable does.
const tailRefused = ": cairn enable refuses it and leaves it as it is"

// readSettingsHooks reads the settings file at path and returns each cairn hook under the three events. A file that is
// not there has none. A file that cannot be read, or that enable refuses, is an error whose text completes the sentence
// "<path> ...": readSettingsFile gives the reasons for the file itself, and the merge refusals are enable's own. For a
// project file the refusals end by saying enable refuses the file; for the user's file they do not, since enable never
// writes it. Entries that are not shaped as the settings format gives them are not matches, as in enable.
func readSettingsHooks(path string, user bool) ([]installedHook, error) {
	tail := tailRefused
	if user {
		tail = ""
	}
	raw, err := readSettingsFile(path, user)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("is not plain JSON (comments or a syntax error)" + tail)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return nil, errors.New("is not a JSON object" + tail)
	}
	var byEvent map[string]json.RawMessage
	if hooksRaw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &byEvent); err != nil || byEvent == nil {
			return nil, errors.New(`has a "hooks" key that is not a JSON object` + tail)
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
			return nil, fmt.Errorf(`has a "hooks" entry for %s that is not a JSON array%s`, h.event, tail)
		}
		for _, e := range commandEntries(entries, h.cmdline) {
			found = append(found, installedHook{path: path, event: h.event, matcher: e.matcher, notString: e.notString})
		}
	}
	return found, nil
}

// readCheckedFile returns the bytes of the project file at path, which enable merges, or the reason doctor does not read it.
// It is readSettingsFile for a file enable writes: a link is refused, and the refusal says so.
func readCheckedFile(path string) ([]byte, error) {
	return readSettingsFile(path, false)
}

// readSettingsFile returns the bytes of the file at path, or the reason doctor does not read it. The path is looked at with
// Lstat before it is opened, so a symbolic link, a FIFO, a device or a directory is not opened: opening a FIFO or a device
// can block, and a link would take doctor to a file enable does not touch. The open file is checked again, and the read
// stops one byte past the cap, so a file that changes between the two looks is still refused. A file that is not there is
// fs.ErrNotExist, unchanged, so the caller can treat it as having no hooks.
//
// For the user's file (user is set) the path is looked at with Stat instead, so a link is followed to its target, which must
// still be a regular file under the cap. The refusals then carry no note that enable refuses the file.
func readSettingsFile(path string, user bool) ([]byte, error) {
	tail := tailRefused
	stat := os.Lstat
	if user {
		tail = ""
		stat = os.Stat
	}
	info, err := stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("could not be read: %w%s", err, tail)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, errors.New("is a symbolic link, which doctor does not follow" + tail)
	case !info.Mode().IsRegular():
		return nil, errors.New("is not a regular file" + tail)
	case info.Size() > maxSettingsBytes:
		return nil, fmt.Errorf("is larger than %d bytes, so doctor does not read it", maxSettingsBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not be read: %w%s", err, tail)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("could not be read: %w%s", err, tail)
	}
	if !opened.Mode().IsRegular() {
		return nil, errors.New("is not a regular file" + tail)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not be read: %w%s", err, tail)
	}
	if len(raw) > maxSettingsBytes {
		return nil, fmt.Errorf("is larger than %d bytes, so doctor does not read it", maxSettingsBytes)
	}
	return raw, nil
}

// hookLine is the line for one cairn hook found in a settings file. A SessionStart hook whose matcher does not name
// compact is a warning, since compact is what carries a task across /compact, and enable leaves an existing entry as
// it is, so the matcher has to be edited by hand. PreCompact is installed per D15, but Claude Code does not show its
// output, so the line says that is an operator decision.
func hookLine(f installedHook) check {
	where := "installed in " + f.path + ", "
	matcher := "no matcher"
	if f.matcher != "" {
		matcher = "matcher " + shownMatcher(f.matcher)
	}
	switch {
	case f.notString:
		return check{name: f.event, status: "warn", detail: where + "its matcher is not a string, so doctor cannot read it: edit the entry by hand, since enable leaves an existing entry as it is"}
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
