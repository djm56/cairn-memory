package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

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
// is the one every other command finds, from any directory of the project.
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
	server := fl.String("server", "", "the server URL, such as https://cairn.lan:8787")
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
		fmt.Fprintln(stderr, "cairn init: the server is not an http or https URL with a host, such as https://cairn.lan:8787")
		return 2
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
		fmt.Fprintf(stderr, "cairn init: write %s: %v\n", path, err)
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
		path, shown(have), shown(srv))
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

// writeNew creates path with the project and server keys, all at once or not at all.
// The whole file is written to a temporary file in the same directory, and that file
// is then linked to path. os.Link fails when path exists, so an existing file is never
// replaced, even one that appears after the check, and the link is atomic. The
// temporary file is removed in every case, so a write that fails part way leaves no
// partial .cairn.toml behind (step 10 review W4).
func writeNew(path, project, server string) error {
	content, err := projectContent(project, server)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cairn.toml.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := finishTemp(f, content); err != nil {
		return err
	}
	return os.Link(tmp, path)
}

// finishTemp writes content to f, gives the file the mode of a committed file, makes
// it durable, and closes it. The first failure is the one returned.
func finishTemp(f *os.File, content []byte) error {
	err := writeContent(f, content)
	if err == nil {
		err = f.Chmod(0o644)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
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
