package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

// runInit is the sub-command `cairn init --project <name> --server <url>`. It writes
// .cairn.toml in the current directory (DESIGN.md §5.2) and does not contact the
// server: the project is registered by its first call, as CONTRACT §1.1 says.
//
// It is idempotent (#69). Run again with the same values, it changes nothing and
// says so. It never changes a value the file already has: a different value is
// refused, with the reason and the fix, and the file is left as it was. There is no
// flag to overwrite, since none is in the issue. Its messages name no destructive
// command (#70).
//
// Exit codes: 0 for a written or unchanged file, 1 for a refusal of the file, 2 for
// a usage error.
func runInit(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn init", flag.ContinueOnError)
	fl.SetOutput(stderr)
	project := fl.String("project", "", "the project name, such as acme-api")
	server := fl.String("server", "", "the server URL, such as https://cairn.lan:8787")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
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
	if _, err := client.New(*server, nil); err != nil {
		fmt.Fprintf(stderr, "cairn init: %q is not an http or https URL with a host, such as https://cairn.lan:8787\n", *server)
		return 2
	}

	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn init: the working directory: %v\n", err)
		return 1
	}
	path := filepath.Join(wd, identity.FileName)
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

	// A file above this directory belongs to the project above, and a second one here
	// would shadow it for everything below.
	if above, found := fileAbove(wd); found {
		fmt.Fprintf(stderr, "cairn init: %s already names the project, and a second .cairn.toml here would shadow it: run cairn init in that directory instead\n", above)
		return 1
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

// compareExisting handles a .cairn.toml that is already in the directory. When it
// has the same project and server, nothing changes and the answer says so. Any other
// content is refused, and the file is not touched. Its other keys are left alone.
func compareExisting(path, project, server string, stdout, stderr io.Writer) int {
	var have struct {
		Project any `toml:"project"`
		Server  any `toml:"server"`
	}
	if _, err := toml.DecodeFile(path, &have); err != nil {
		fmt.Fprintf(stderr, "cairn init: %s is not valid TOML: %v; fix it by hand, then run cairn init\n", path, err)
		return 1
	}
	if p, ok := have.Project.(string); ok && p == project {
		if s, ok := have.Server.(string); ok && s == server {
			fmt.Fprintf(stdout, "cairn init: %s already has project %q and server %q; nothing changed.\n", path, project, server)
			return 0
		}
	}
	fmt.Fprintf(stderr, "cairn init: %s already sets project %s and server %s. cairn init does not change a value it finds; to use other values, edit the file by hand.\n",
		path, shown(have.Project), shown(have.Server))
	return 1
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

// writeNew creates path with the project and server keys. It opens the file with
// O_EXCL, so an existing file is never truncated or replaced, even if one appears
// after the check.
func writeNew(path, project, server string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	if _, err := fmt.Fprint(f, fileHeader); err != nil {
		return err
	}
	return toml.NewEncoder(f).Encode(projectFile{Project: project, Server: server})
}
