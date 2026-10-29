// Package identity finds the project a Cairn call belongs to (docs/DESIGN.md
// §2.1). It resolves the project only. The agent and the session are the
// caller's own fields, supplied by the adapter, and are not resolved here.
//
// The rules are checked in this order, and the first that applies wins:
//
//  1. CAIRN_PROJECT in the environment.
//  2. project = "<name>" in the nearest .cairn.toml, found by walking up from
//     CLAUDE_PROJECT_DIR when Claude Code sets it, or else from the working
//     directory.
//  3. Otherwise core.ErrNoProject. There is no default project.
//
// A project name is checked by core.ValidProjectName, the rule that lives in
// internal/core. The name rule is applied to both sources.
package identity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/djm56/cairn-memory/internal/core"
)

// EnvProject is the environment variable that names the project (DESIGN.md §2.1, rule 1).
const EnvProject = "CAIRN_PROJECT"

// EnvProjectDir is the environment variable Claude Code sets for MCP servers.
// When it is set, the .cairn.toml search starts there, not in the working directory.
const EnvProjectDir = "CLAUDE_PROJECT_DIR"

// FileName is the project file, committed at the project root (DESIGN.md §2.1, rule 2).
const FileName = ".cairn.toml"

// Source records where a resolved project name came from.
type Source string

const (
	// SourceEnv: the name came from CAIRN_PROJECT.
	SourceEnv Source = "env"
	// SourceFile: the name came from the project key of a .cairn.toml file.
	SourceFile Source = "file"
)

// Project is a resolved project name and where it was found.
type Project struct {
	// Name is the project name, already checked by core.ValidProjectName.
	Name string
	// Source says which rule supplied Name.
	Source Source
	// Path is the absolute path of the .cairn.toml that supplied Name. It is
	// empty when Source is SourceEnv.
	Path string
}

// projectFile is the part of .cairn.toml this package reads. Other keys, such
// as the optional server, are ignored here. Project is decoded as any so that a
// value of the wrong type can be reported as such, not as a TOML syntax fault.
type projectFile struct {
	Project any `toml:"project"`
}

// Resolve returns the project for a call made from workDir. It reads
// CAIRN_PROJECT and CLAUDE_PROJECT_DIR from the environment.
//
// An empty CAIRN_PROJECT counts as unset. A CAIRN_PROJECT that is set but
// not a valid name is an error, and Resolve does not fall through to the file:
// an explicit variable is an instruction. The nearest .cairn.toml is
// authoritative too. If it is malformed, or has no valid project key, Resolve
// returns an error and does not look further up.
//
// Errors are *core.Error values of KindInvalid, except for an unreadable
// directory during the walk, which is returned as is. A missing project is
// core.ErrNoProject.
func Resolve(workDir string) (Project, error) {
	if name := os.Getenv(EnvProject); name != "" {
		if !core.ValidProjectName(name) {
			return Project{}, invalidName(EnvProject, name)
		}
		return Project{Name: name, Source: SourceEnv}, nil
	}

	start := os.Getenv(EnvProjectDir)
	if start == "" {
		start = workDir
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return Project{}, fmt.Errorf("identity: resolve start directory %q: %w", start, err)
	}

	path, found, err := findFile(abs)
	if err != nil {
		return Project{}, err
	}
	if !found {
		return Project{}, core.ErrNoProject
	}
	return readFile(path)
}

// findFile walks up from dir and returns the first .cairn.toml it meets. It
// reports found false when the walk reaches the filesystem root without one.
// Only a missing file is passed over. Any other stat error stops the walk, and
// so does an entry named .cairn.toml that is a directory: that is a KindInvalid
// error, because the nearest entry is authoritative and it is not a file.
func findFile(dir string) (string, bool, error) {
	for {
		path := filepath.Join(dir, FileName)
		info, err := os.Stat(path)
		switch {
		case err == nil && info.IsDir():
			return "", false, core.Invalid("%s is a directory, not a project file", path)
		case err == nil:
			return path, true, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", false, fmt.Errorf("identity: look for %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// readFile decodes the project key of the .cairn.toml at path and checks it.
// A file that does not parse is a KindInvalid error that names the file. So is
// a project key of the wrong type, or one that is missing or empty, and a name
// that breaks the rule.
func readFile(path string) (Project, error) {
	var pf projectFile
	if _, err := toml.DecodeFile(path, &pf); err != nil {
		return Project{}, core.Invalid("%s is not valid TOML: %v", path, err)
	}
	if pf.Project == nil {
		return Project{}, core.Invalid("%s has no project key: set project = \"<name>\", or run `cairn init`", path)
	}
	name, ok := pf.Project.(string)
	if !ok {
		return Project{}, core.Invalid("%s has the wrong type for project: the value must be a quoted string, project = \"<name>\"", path)
	}
	if name == "" {
		return Project{}, core.Invalid("%s has no project key: set project = \"<name>\", or run `cairn init`", path)
	}
	if !core.ValidProjectName(name) {
		return Project{}, invalidName(path+" project", name)
	}
	return Project{Name: name, Source: SourceFile, Path: path}, nil
}

// invalidName is the KindInvalid error for a project name that breaks the
// rule. The text states the rule, so the operator can fix the name.
func invalidName(where, name string) *core.Error {
	return core.Invalid("%s %q is not a valid project name: use lowercase letters, digits and single hyphens, at most 64 characters", where, name)
}
