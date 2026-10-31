package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// projectFile is what cairn init writes: the two keys of .cairn.toml that a project
// must have (DESIGN.md §2.1). The API key never goes here.
type projectFile struct {
	Project string `toml:"project"`
	Server  string `toml:"server"`
}

// fileHeader opens the file. It says what the file is, and that it is safe to commit.
const fileHeader = "# Cairn project file (docs/DESIGN.md §2.1). Commit it. It holds no key.\n"

// writeContent puts the bytes of a new project file into its temporary file. It is a
// variable so a test can make the write fail part way (step 10 review W4).
var writeContent = func(w io.Writer, content []byte) error {
	_, err := w.Write(content)
	return err
}

// runInit is the sub-command `cairn init --project <name> --server <url>`. It writes
// .cairn.toml in the project directory (DESIGN.md §5.2) and does not contact the
// server: the project is registered by its first call, as CONTRACT §1.1 says.
//
// The project directory is the one identity starts from: CLAUDE_PROJECT_DIR when Claude
// Code sets it, else the working directory (step 10 review W6). So the file init writes
// is the one every other command finds, from any directory of the project. A run from a
// directory outside CLAUDE_PROJECT_DIR is refused, since it would write into that
// project (step 12 review W5).
//
// It is idempotent (#69). Run again with the same values, it changes nothing and
// says so, here or in a directory below a project file that has the same values. It
// never changes a value the file already has: a different value is refused, with the
// reason and the fix, and the file is left as it was. There is no flag to overwrite,
// since none is in the issue. Its messages name no destructive command (#70), and it
// never prints the server URL with a user name or password (step 10 review W5).
//
// Exit codes: 0 for a written or unchanged file, 1 for a refusal of the file, 2 for
// a usage error. `--help` prints to stdout and exits 0.
func runInit(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn init", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	project := fl.String("project", "", "the project name, such as acme-api")
	server := fl.String("server", "", "the server URL, such as https://cairn.lan:8788")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeInitHelp(stdout, fl)
			return 0
		}
		fmt.Fprintf(stderr, "cairn init: %v; run cairn init --help for the flags\n", err)
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn init: takes flags only, got %q\n", fl.Args())
		return 2
	}
	if *project == "" || *server == "" {
		fmt.Fprintln(stderr, "cairn init: needs --project <name> and --server <url>, which are written to .cairn.toml")
		return 2
	}
	if !core.ValidProjectName(*project) {
		fmt.Fprintf(stderr, "cairn init: %q is not a valid project name: use lowercase letters, digits and single hyphens, at most 64 characters\n", *project)
		return 2
	}
	// The file is committed, so a credential in its server URL would be published. The
	// refusal names no part of the URL.
	if u, err := url.Parse(*server); err == nil && u.User != nil {
		fmt.Fprintln(stderr, "cairn init: the server URL holds a user name or a password, and .cairn.toml is committed: give the URL without them. Keys are not part of M1, and they never go in this file (DESIGN.md §2.1)")
		return 2
	}
	if _, err := client.New(*server, nil); err != nil {
		fmt.Fprintln(stderr, "cairn init: the server is not an http or https URL with a host, such as https://cairn.lan:8788")
		return 2
	}

	if err := checkProjectDir(); err != nil {
		fmt.Fprintf(stderr, "cairn init: %v\n", err)
		return 1
	}
	dir, err := initDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn init: the working directory: %v\n", err)
		return 1
	}
	path := filepath.Join(dir, identity.FileName)
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		fmt.Fprintf(stderr, "cairn init: %s is a directory, not a project file: move it aside by hand, then run cairn init\n", path)
		return 1
	case err == nil:
		return compareExisting(path, *project, *server, stdout, stderr)
	case !errors.Is(err, fs.ErrNotExist):
		fmt.Fprintf(stderr, "cairn init: look for %s: %v\n", path, err)
		return 1
	}

	// A file above this directory belongs to the project above. A second one here would
	// shadow it for everything below, unless it says the same, in which case there is
	// nothing to write.
	if above, found := fileAbove(dir); found {
		return compareAbove(above, *project, *server, stdout, stderr)
	}

	if err := writeNew(path, *project, *server); err != nil {
		if errors.Is(err, fs.ErrExist) {
			fmt.Fprintf(stderr, "cairn init: %s appeared while cairn init was running: run cairn init again to compare it\n", path)
			return 1
		}
		fmt.Fprintf(stderr, "cairn init: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "cairn init: wrote %s with project %q and server %q.\n", path, *project, *server)
	fmt.Fprintln(stdout, "The project is registered on the server by its first call. Commit the file: it holds no key.")
	return 0
}

// writeInitHelp prints the usage of cairn init, and its flags, to w.
func writeInitHelp(w io.Writer, fl *flag.FlagSet) {
	fmt.Fprintln(w, "Usage: cairn init --project <name> --server <url>")
	fmt.Fprintln(w, "Writes .cairn.toml in the project directory: CLAUDE_PROJECT_DIR when it is set, else the working directory.")
	fmt.Fprintln(w, "It changes nothing that is already there.")
	fmt.Fprintln(w, "It refuses to run from a working directory outside CLAUDE_PROJECT_DIR, when that is set.")
	fmt.Fprintln(w, "It writes the file whole or not at all where the filesystem has hard links. On one without them it writes the file directly, and a crash can leave it partly written.")
	fl.SetOutput(w)
	fl.PrintDefaults()
}

// initDir is the directory cairn init writes into: the one identity starts its walk
// from, so the file is the one every command finds (step 10 review W6).
func initDir() (string, error) {
	if dir := os.Getenv(identity.EnvProjectDir); dir != "" {
		return filepath.Abs(dir)
	}
	return workDir()
}

// checkProjectDir refuses an init run from outside CLAUDE_PROJECT_DIR (step 12 review
// W5). The variable is set by Claude Code and inherited by the shells it starts, so a
// cd into another project would otherwise write that project's file into the first
// one's root, and exit 0. Both directories are resolved through their symlinks before
// they are compared, and the comparison is by path elements, so a sibling whose name
// starts the same way is outside. With no variable set there is nothing to check.
func checkProjectDir() error {
	env := os.Getenv(identity.EnvProjectDir)
	if env == "" {
		return nil
	}
	proj, err := filepath.Abs(env)
	if err != nil {
		return fmt.Errorf("%s %q: %w", identity.EnvProjectDir, env, err)
	}
	wd, err := workDir()
	if err != nil {
		return fmt.Errorf("the working directory: %w", err)
	}
	projReal, err := filepath.EvalSymlinks(proj)
	if err != nil {
		return fmt.Errorf("%s is %s, which cannot be read: %w", identity.EnvProjectDir, proj, err)
	}
	wdReal, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return fmt.Errorf("the working directory %s cannot be read: %w", wd, err)
	}
	rel, err := filepath.Rel(projReal, wdReal)
	if err != nil {
		return fmt.Errorf("%s is %s, and the working directory is %s: %w", identity.EnvProjectDir, proj, wd, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%s is %s, but the working directory %s is not inside it, and cairn init would write into %s: unset %s, or cd into the project and run cairn init again",
			identity.EnvProjectDir, proj, wd, proj, identity.EnvProjectDir)
	}
	return nil
}

// shownServer describes a server value read from an existing file, for a message. A
// string is shown through client.DisplayURL, so a user name or a password in it is never
// printed (step 10 review W5; step 12 review W6). A value of another type is described as
// shown describes it.
func shownServer(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", client.DisplayURL(s))
	}
	return shown(v)
}

// compareExisting handles a .cairn.toml that is already in the directory. When it
// has the same project and server, nothing changes and the answer says so. Any other
// content is refused, and the file is not touched. Its other keys are left alone.
func compareExisting(path, project, server string, stdout, stderr io.Writer) int {
	have, srv, err := fileValues(path)
	if err != nil {
		fmt.Fprintf(stderr, "cairn init: %s is not valid TOML: %v; fix it by hand, then run cairn init\n", path, err)
		return 1
	}
	if sameValue(have, project) && sameValue(srv, server) {
		fmt.Fprintf(stdout, "cairn init: %s already has project %q and server %q; nothing changed.\n", path, project, server)
		return 0
	}
	fmt.Fprintf(stderr, "cairn init: %s already sets project %s and server %s. cairn init does not change a value it finds; to use other values, edit the file by hand.\n",
		path, shown(have), shownServer(srv))
	return 1
}

// compareAbove handles a project file above the directory. With the same project and
// server it is the project this directory is in, so nothing is written and the answer
// says so. Any other values would shadow it, and are refused.
func compareAbove(above, project, server string, stdout, stderr io.Writer) int {
	have, srv, err := fileValues(above)
	if err == nil && sameValue(have, project) && sameValue(srv, server) {
		fmt.Fprintf(stdout, "cairn init: %s already sets project %q and server %q, and this directory is inside that project; nothing changed.\n", above, project, server)
		return 0
	}
	fmt.Fprintf(stderr, "cairn init: %s already names the project, and a second .cairn.toml here would shadow it: run cairn init in that directory instead\n", above)
	return 1
}

// fileValues reads the project and server keys of the file at path as they are written.
// A value of another type, or a missing key, comes back as it is, so the caller can say
// what it found. An error means the file is not valid TOML.
func fileValues(path string) (project, server any, err error) {
	var have struct {
		Project any `toml:"project"`
		Server  any `toml:"server"`
	}
	if _, err := toml.DecodeFile(path, &have); err != nil {
		return nil, nil, err
	}
	return have.Project, have.Server, nil
}

// sameValue reports whether v is the string want.
func sameValue(v any, want string) bool {
	s, ok := v.(string)
	return ok && s == want
}

// shown describes a value read from the file, for a message. A value of the wrong
// type is named as such, and an absent one is named as absent.
func shown(v any) string {
	switch v := v.(type) {
	case nil:
		return "no value"
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return "a value of the wrong type"
	}
}

// fileAbove returns the first .cairn.toml above dir, the directory itself excluded,
// and whether there is one. Any entry of that name counts: a directory there is in
// the way as well.
func fileAbove(dir string) (string, bool) {
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
		path := filepath.Join(dir, identity.FileName)
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
}

// linkFile makes a second name for an existing file. It is os.Link, and a variable so a
// test can make the link fail the way a filesystem without hard links does (step 12
// review W4).
var linkFile = os.Link

// writeNew creates path with the project and server keys, all at once or not at all.
// The whole file is written to a temporary file in the same directory, and that file
// is then linked to path. A link fails when path exists, so an existing file is never
// replaced, even one that appears after the check, and the link is atomic. The
// temporary file is removed in every case, so a write that fails part way leaves no
// partial .cairn.toml behind (step 10 review W4).
//
// A filesystem with no hard links refuses the link with an errno that says so. Then the
// file is created at path directly, with O_EXCL, which keeps the no-overwrite rule but
// is not atomic: a failure part way removes the partial file. Every other link failure,
// and an existing name, are returned as they are and never fall back (step 12 review
// W4). Errors name path, and never the temporary file.
func writeNew(path, project, server string) error {
	content, err := projectContent(project, server)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, cause(err))
	}
	f, tmp, err := createTemp(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("write %s: %w", path, cause(err))
	}
	defer os.Remove(tmp)
	if err := finishFile(f, content); err != nil {
		return fmt.Errorf("write %s: %w", path, cause(err))
	}
	if err := linkFile(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) || !noHardLinks(err) {
			return fmt.Errorf("write %s: %w", path, cause(err))
		}
		if err := writeDirect(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, cause(err))
		}
	}
	syncDir(filepath.Dir(path))
	return nil
}

// createTemp creates a new file in dir, named .cairn.toml and a random suffix, with the
// mode a committed file gets (0644, before the umask). os.CreateTemp would make it 0600.
// A name that already exists is tried again.
func createTemp(dir string) (*os.File, string, error) {
	for range 100 {
		name := filepath.Join(dir, identity.FileName+"."+rand.Text()+".tmp")
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", &fs.PathError{Op: "create", Path: dir, Err: fs.ErrExist}
}

// writeDirect creates path with content, and no other name. O_EXCL makes it refuse an
// existing file, so nothing is replaced. The mode is 0644 before the umask, as the
// temporary file's is. A failure after the file is made removes it, since a partial
// .cairn.toml is worse than none.
func writeDirect(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := finishFile(f, content); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// finishFile writes content to f, makes it durable, and closes it. The first failure is
// the one returned. The file keeps the mode it was created with, so the umask applies.
func finishFile(f *os.File, content []byte) error {
	err := writeContent(f, content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// noHardLinks reports whether a failed link means the filesystem cannot make one: EPERM
// (FAT and exFAT on macOS), ENOTSUP and EOPNOTSUPP (one value on Linux, two on macOS),
// EXDEV, and ENOSYS (a mount that does not implement link). It is false for EEXIST, and
// for every other failure, which is returned rather than worked around.
func noHardLinks(err error) bool {
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.ENOSYS} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// syncDir makes the new name durable, where the platform lets a directory be synced. It
// is best effort: the file is already in place when it runs, so a failure is not
// reported as a failed write. It only means a crash could lose the name, which is then
// as if there were no file.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// cause returns the error under a path error or a link error, so a message names the
// file that was meant and not a temporary file the caller made.
func cause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// projectContent is the whole text of a new project file: the header, then the keys.
func projectContent(project, server string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	if err := toml.NewEncoder(&buf).Encode(projectFile{Project: project, Server: server}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
