// Package identity finds the project a Cairnbook call belongs to, and the server it
// goes to (docs/DESIGN.md §2.1). Resolve gives the project; ResolveServer gives
// the server URL, read from CAIRNBOOK_SERVER or the server key of the same file. The
// agent and the session are the caller's own fields, supplied by the adapter, and
// are not resolved here.
//
// The rules are checked in this order, and the first that applies wins:
//
//  1. CAIRNBOOK_PROJECT in the environment, or CAIRN_PROJECT when it is unset.
//  2. project = "<name>" in the nearest .cairnbook.toml (or .cairn.toml), found by
//     walking up from CLAUDE_PROJECT_DIR when Claude Code sets it, or else from the
//     working directory.
//  3. Otherwise core.ErrNoProject. There is no default project.
//
// The old names, from before the rename to Cairnbook, are read only when the new ones
// are missing, so an install made before the rename keeps working (see Legacy below).
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
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/djm56/cairn-memory/internal/core"
)

// EnvProject is the environment variable that names the project (DESIGN.md §2.1, rule 1).
const EnvProject = "CAIRNBOOK_PROJECT"

// EnvServer is the environment variable that names the server a call goes to
// (DESIGN.md §2.1, D11). It is read before the server key of the file.
const EnvServer = "CAIRNBOOK_SERVER"

// EnvAgent is the environment variable that names the calling agent for the CLI's writes, and for
// the reads that take it as the caller's role, when a verb's --agent flag is left out. It is
// the agent's name, such as hicks. This package does not resolve it: the agent is the caller's
// own field, and the CLI reads the variable (CONTRACT §1.1). It is named here beside the other
// variables so that the list of them is in one place.
const EnvAgent = "CAIRNBOOK_AGENT"

// EnvProjectDir is the environment variable Claude Code sets for MCP servers.
// When it is set, the .cairnbook.toml search starts there, not in the working directory.
const EnvProjectDir = "CLAUDE_PROJECT_DIR"

// FileName is the project file, committed at the project root (DESIGN.md §2.1, rule 2).
const FileName = ".cairnbook.toml"

// LegacyFileName is the project file's name before the rename to Cairnbook. In each
// directory of the walk it is read when there is no FileName, so a project set up
// before the rename keeps working. cairnbook init writes only FileName.
const LegacyFileName = ".cairn.toml"

// LegacyEnvPrefix is the prefix of the environment variables before the rename to
// Cairnbook. A variable is read under its old name only when its new name is unset or
// empty, so an install made before the rename keeps working.
const LegacyEnvPrefix = "CAIRN_"

// LegacyName returns the name a Cairnbook environment variable had before the rename:
// CAIRNBOOK_X was CAIRN_X. Any other name is returned as it is.
func LegacyName(name string) string {
	if rest, ok := strings.CutPrefix(name, "CAIRNBOOK_"); ok {
		return LegacyEnvPrefix + rest
	}
	return name
}

// Lookup returns the value of the environment variable name, and the name it was found
// under: name itself, or its old name (LegacyName) when name is unset or empty. Both are
// empty when neither is set.
func Lookup(name string) (value, from string) {
	if v := os.Getenv(name); v != "" {
		return v, name
	}
	if old := LegacyName(name); old != name {
		if v := os.Getenv(old); v != "" {
			return v, old
		}
	}
	return "", ""
}

// Getenv returns the value Lookup finds for name, under the new name or the old one.
func Getenv(name string) string {
	v, _ := Lookup(name)
	return v
}

// Source records where a resolved project name came from.
type Source string

const (
	// SourceEnv: the name came from CAIRNBOOK_PROJECT.
	SourceEnv Source = "env"
	// SourceFile: the name came from the project key of a .cairnbook.toml file.
	SourceFile Source = "file"
)

// Project is a resolved project name and where it was found.
type Project struct {
	// Name is the project name, already checked by core.ValidProjectName.
	Name string
	// Source says which rule supplied Name.
	Source Source
	// Path is the absolute path of the .cairnbook.toml that supplied Name. It is
	// empty when Source is SourceEnv.
	Path string
}

// projectFile is the part of .cairnbook.toml this package reads. Other keys, such
// as the optional server, are ignored here. Project is decoded as any so that a
// value of the wrong type can be reported as such, not as a TOML syntax fault.
type projectFile struct {
	Project any `toml:"project"`
}

// Resolve returns the project for a call made from workDir. It reads
// CAIRNBOOK_PROJECT and CLAUDE_PROJECT_DIR from the environment.
//
// An empty CAIRNBOOK_PROJECT counts as unset. A CAIRNBOOK_PROJECT that is set but
// not a valid name is an error, and Resolve does not fall through to the file:
// an explicit variable is an instruction. The nearest .cairnbook.toml is
// authoritative too. If it is malformed, or has no valid project key, Resolve
// returns an error and does not look further up.
//
// Errors are *core.Error values of KindInvalid, except for an unreadable
// directory during the walk, which is returned as is. A missing project is
// core.ErrNoProject.
func Resolve(workDir string) (Project, error) {
	if name, from := Lookup(EnvProject); name != "" {
		if !core.ValidProjectName(name) {
			return Project{}, invalidName(from, name)
		}
		return Project{Name: name, Source: SourceEnv}, nil
	}

	path, found, err := nearestFile(workDir)
	if err != nil {
		return Project{}, err
	}
	if !found {
		return Project{}, core.ErrNoProject
	}
	return readFile(path)
}

// nearestFile returns the .cairnbook.toml that a call from workDir finds: the nearest
// one, walking up from CLAUDE_PROJECT_DIR when Claude Code sets it, else from
// workDir. found is false when the walk reaches the root without one.
func nearestFile(workDir string) (string, bool, error) {
	start := os.Getenv(EnvProjectDir)
	if start == "" {
		start = workDir
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false, fmt.Errorf("identity: resolve start directory %q: %w", start, err)
	}
	return findFile(abs)
}

// serverFile is the part of .cairnbook.toml that ResolveServer reads. Server is
// decoded as any, as projectFile decodes Project, so a value of the wrong type is
// named as such and not reported as a syntax fault.
type serverFile struct {
	Server any `toml:"server"`
}

// ResolveServer returns the server URL that calls from workDir go to (DESIGN.md
// §2.1, D11). CAIRNBOOK_SERVER is read first, and an empty value counts as unset.
// Otherwise the server key of the nearest .cairnbook.toml is the URL. The URL is not
// checked here: the client checks it when it is built.
//
// A call with no server is core.Invalid, and the message names the fix, as a
// call with no project does. There is no default server.
func ResolveServer(workDir string) (string, error) {
	if server := Getenv(EnvServer); server != "" {
		return server, nil
	}
	path, found, err := nearestFile(workDir)
	if err != nil {
		return "", err
	}
	if !found {
		return "", core.Invalid("no server: run `cairnbook init` in the project to write one, or set %s", EnvServer)
	}
	var sf serverFile
	if _, err := toml.DecodeFile(path, &sf); err != nil {
		return "", core.Invalid("%s is not valid TOML: %v", path, err)
	}
	switch v := sf.Server.(type) {
	case nil:
		return "", noServerKey(path)
	case string:
		if v == "" {
			return "", noServerKey(path)
		}
		return v, nil
	default:
		return "", core.Invalid("%s has the wrong type for server: the value must be a quoted string, server = \"https://...\"", path)
	}
}

// ServerSource says where ResolveServer finds the server for a call from workDir: the name of the variable it
// is read from (CAIRNBOOK_SERVER, or CAIRN_SERVER under its old name), or the base name of the file it is read
// from, FileName or LegacyFileName. It follows the same order as ResolveServer, so the two cannot disagree. It
// returns "" when neither gives a server.
func ServerSource(workDir string) string {
	if _, from := Lookup(EnvServer); from != "" {
		return from
	}
	path, found, err := nearestFile(workDir)
	if err != nil || !found {
		return ""
	}
	return filepath.Base(path)
}

// noServerKey is the KindInvalid error for a .cairnbook.toml with no server key, or an
// empty one. The text names the key to set and the command that writes it.
func noServerKey(path string) *core.Error {
	return core.Invalid("%s has no server key: set server = \"<url>\", or run `cairnbook init`", path)
}

// findFile walks up from dir and returns the first .cairnbook.toml it meets, or, in a
// directory that has none, the first .cairn.toml (LegacyFileName). It reports found
// false when the walk reaches the filesystem root without either.
// Only a missing file is passed over. Any other stat error stops the walk, and
// so does an entry named .cairnbook.toml that is a directory: that is a KindInvalid
// error, because the nearest entry is authoritative and it is not a file.
func findFile(dir string) (string, bool, error) {
	for {
		for _, name := range []string{FileName, LegacyFileName} {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			switch {
			case err == nil && info.IsDir():
				return "", false, core.Invalid("%s is a directory, not a project file", path)
			case err == nil:
				return path, true, nil
			case !errors.Is(err, fs.ErrNotExist):
				return "", false, fmt.Errorf("identity: look for %s: %w", path, err)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// readFile decodes the project key of the .cairnbook.toml at path and checks it.
// A file that does not parse is a KindInvalid error that names the file. So is
// a project key of the wrong type, or one that is missing or empty, and a name
// that breaks the rule.
func readFile(path string) (Project, error) {
	var pf projectFile
	if _, err := toml.DecodeFile(path, &pf); err != nil {
		return Project{}, core.Invalid("%s is not valid TOML: %v", path, err)
	}
	if pf.Project == nil {
		return Project{}, core.Invalid("%s has no project key: set project = \"<name>\", or run `cairnbook init`", path)
	}
	name, ok := pf.Project.(string)
	if !ok {
		return Project{}, core.Invalid("%s has the wrong type for project: the value must be a quoted string, project = \"<name>\"", path)
	}
	if name == "" {
		return Project{}, core.Invalid("%s has no project key: set project = \"<name>\", or run `cairnbook init`", path)
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
