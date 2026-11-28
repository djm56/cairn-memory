package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/djm56/cairn-memory/internal/identity"
)

const (
	initProject = "acme-api"
	initServer  = "https://cairnbook.lan:8788"
)

// initArgs is the command line of cairnbook init for the test project and server.
func initArgs(project, server string) []string {
	return []string{"init", "--project", project, "--server", server}
}

// TestInitWritesTheFile checks that init writes the two keys the project needs
// (DESIGN §2.1), with the header that says the file is safe to commit.
func TestInitWritesTheFile(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	path := filepath.Join(dir, ".cairnbook.toml")
	var got struct {
		Project string `toml:"project"`
		Server  string `toml:"server"`
	}
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("the written file is not TOML: %v", err)
	}
	if got.Project != initProject || got.Server != initServer {
		t.Errorf("file has project %q server %q, want %q and %q", got.Project, got.Server, initProject, initServer)
	}
	if !strings.Contains(string(readBytes(t, path)), "holds no key") {
		t.Errorf("file has no header: %q", readBytes(t, path))
	}
	if !strings.Contains(stdout, "wrote") {
		t.Errorf("stdout %q does not say the file was written", stdout)
	}
}

// TestInitTwiceChangesNothing is the idempotence of #69: a second run with the same
// values leaves the file byte for byte as it was, and says so.
func TestInitTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	runCmd(t, initArgs(initProject, initServer)...)
	path := filepath.Join(dir, ".cairnbook.toml")
	before := readBytes(t, path)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 {
		t.Fatalf("second run: exit %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "nothing changed") {
		t.Errorf("second run: stdout %q does not say nothing changed", stdout)
	}
	if string(readBytes(t, path)) != string(before) {
		t.Errorf("second run changed the file:\n%s\nwant:\n%s", readBytes(t, path), before)
	}
}

// TestInitRefusesOtherValuesAndKeepsTheFile is the no-overwrite rule: a different
// project or server is refused with the way to change it by hand, and the file is
// not touched.
func TestInitRefusesOtherValuesAndKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	runCmd(t, initArgs(initProject, initServer)...)
	path := filepath.Join(dir, ".cairnbook.toml")
	before := readBytes(t, path)

	for _, args := range [][]string{
		initArgs("other-project", initServer),
		initArgs(initProject, "https://elsewhere.lan:8788"),
	} {
		code, stdout, stderr := runCmd(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
		if !strings.Contains(stderr, "does not change a value it finds") || !strings.Contains(stderr, "edit the file by hand") {
			t.Errorf("%v: stderr %q does not explain the refusal and the fix", args, stderr)
		}
		assertNoDestructive(t, stdout, stderr)
		if string(readBytes(t, path)) != string(before) {
			t.Errorf("%v changed the file", args)
		}
	}
}

// TestInitKeepsOtherKeys checks that a file with more keys than init writes is
// still unchanged when its project and server are the ones asked for.
func TestInitKeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	content := "project = \"acme-api\"\nserver = \"https://cairnbook.lan:8788\"\nnote = \"kept\"\n\n[extra]\nvalue = 1\n"
	path := writeFile(t, dir, ".cairnbook.toml", content)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "nothing changed") {
		t.Errorf("stdout %q does not say nothing changed", stdout)
	}
	if string(readBytes(t, path)) != content {
		t.Errorf("file changed:\n%s", readBytes(t, path))
	}
}

// TestInitRefusesFilesItCannotRead covers the existing files init will not
// interpret: broken TOML, a key of the wrong type and a missing key. Each is refused,
// and none is replaced.
func TestInitRefusesFilesItCannotRead(t *testing.T) {
	cases := map[string]string{
		"broken TOML":       "project = [\n",
		"project not text":  "project = 3\nserver = \"https://cairnbook.lan:8788\"\n",
		"no project at all": "server = \"https://cairnbook.lan:8788\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			path := writeFile(t, dir, ".cairnbook.toml", content)
			code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
			if code != 1 {
				t.Errorf("exit %d, want 1 (stderr %q)", code, stderr)
			}
			assertNoDestructive(t, stderr)
			if string(readBytes(t, path)) != content {
				t.Errorf("file changed to %q", readBytes(t, path))
			}
		})
	}
}

// TestInitRefusesADirectoryInTheWay checks that a .cairnbook.toml directory is refused,
// and the message says to move it aside by hand.
func TestInitRefusesADirectoryInTheWay(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, ".cairnbook.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 || !strings.Contains(stderr, "is a directory, not a project file") {
		t.Errorf("exit %d, stderr %q: want 1 and the directory refusal", code, stderr)
	}
	assertNoDestructive(t, stderr)
}

// TestInitRefusesABelowAFileAbove checks that init in a directory below a project
// file is refused: a second file there would shadow the project for its whole tree.
func TestInitRefusesABelowAFileAbove(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".cairnbook.toml", "project = \"acme-api\"\nserver = \"https://cairnbook.lan:8788\"\n")
	sub := filepath.Join(root, "service")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)

	code, _, stderr := runCmd(t, initArgs("other", initServer)...)
	if code != 1 || !strings.Contains(stderr, "would shadow it") {
		t.Errorf("exit %d, stderr %q: want 1 and the shadowing refusal", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairnbook.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written below the project file: %v", err)
	}
}

// TestInitRefusesALegacyFileWithNoNewOne checks #244: a folder that holds only the old .cairn.toml
// is refused whatever values the old file holds. The message names the rename, and nothing is
// written or changed, so the new file cannot win over the old one with other settings.
func TestInitRefusesALegacyFileWithNoNewOne(t *testing.T) {
	cases := map[string]string{
		"same values":  "project = \"acme-api\"\nserver = \"" + initServer + "\"\n",
		"other values": "project = \"old-api\"\nserver = \"https://old.example:8788\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			legacy := writeFile(t, dir, ".cairn.toml", content)

			newPath := filepath.Join(dir, ".cairnbook.toml")
			code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
			// The temporary folder has no space or quote, so the quoted command is the path in single quotes.
			if code != 1 || !strings.Contains(stderr, "is the project file under its old name") ||
				!strings.Contains(stderr, "git mv '"+legacy+"' '"+newPath+"'") || !strings.Contains(stderr, legacy) {
				t.Errorf("exit %d, stderr %q: want 1 and the rename refusal naming %s and %s", code, stderr, legacy, newPath)
			}
			if stdout != "" {
				t.Errorf("stdout %q, want nothing", stdout)
			}
			if _, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); !os.IsNotExist(err) {
				t.Errorf("a .cairnbook.toml was written beside the old file: %v", err)
			}
			if string(readBytes(t, legacy)) != content {
				t.Errorf("the old file changed: %q", readBytes(t, legacy))
			}
			assertNoDestructive(t, stderr)
		})
	}
}

// TestInitRefusalQuotesThePathsInTheRenameCommand checks that the commands the refusal prints can be
// pasted into a POSIX shell: each path is one single-quoted word, and a single quote inside a path
// becomes a close quote, an escaped quote and a reopened quote. The plain path before "is the project file under its old name" is not quoted.
func TestInitRefusalQuotesThePathsInTheRenameCommand(t *testing.T) {
	cases := map[string]struct {
		folder string
		// quote is the shell form of one path, as a POSIX shell reads it back.
		quote func(string) string
	}{
		"a space": {
			folder: "my projects",
			quote:  func(p string) string { return "'" + p + "'" },
		},
		"a single quote": {
			folder: "it's here",
			quote:  func(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tc.folder)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			useDir(t, dir)
			legacy := writeFile(t, dir, ".cairn.toml", "project = \"old-api\"\nserver = \"https://old.example:8788\"\n")
			newPath := filepath.Join(dir, ".cairnbook.toml")

			code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
			if code != 1 || !strings.Contains(stderr, "is the project file under its old name") {
				t.Fatalf("exit %d, stderr %q: want 1 and the rename refusal", code, stderr)
			}
			if !strings.Contains(stderr, legacy+" is the project file under its old name") {
				t.Errorf("stderr %q does not name the old file by its plain path %s", stderr, legacy)
			}
			for _, cmd := range []string{"git mv ", "mv "} {
				want := cmd + tc.quote(legacy) + " " + tc.quote(newPath)
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr %q does not hold the command %q", stderr, want)
				}
			}
			if stdout != "" {
				t.Errorf("stdout %q, want nothing", stdout)
			}
			if _, err := os.Stat(newPath); !os.IsNotExist(err) {
				t.Errorf("a .cairnbook.toml was written beside the old file: %v", err)
			}
			assertNoDestructive(t, stderr)
		})
	}
}

// TestInitRefusesALegacyDirectoryInTheWay checks #244: a .cairn.toml directory is refused as a
// .cairnbook.toml directory is, and nothing is written beside it.
func TestInitRefusesALegacyDirectoryInTheWay(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, ".cairn.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 || !strings.Contains(stderr, "is a directory, not a project file") {
		t.Errorf("exit %d, stderr %q: want 1 and the directory refusal", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); !os.IsNotExist(err) {
		t.Errorf("a .cairnbook.toml was written beside the directory: %v", err)
	}
	assertNoDestructive(t, stderr)
}

// TestInitDirectoryAtTheNewNameBesideALegacyFileRefusesFirst checks #244: a .cairnbook.toml directory
// beside a legacy .cairn.toml file is refused as a directory, since the new-name check runs first. The
// directory and the legacy file are both left as they were, and nothing is written.
func TestInitDirectoryAtTheNewNameBesideALegacyFileRefusesFirst(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	newPath := filepath.Join(dir, ".cairnbook.toml")
	if err := os.Mkdir(newPath, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := writeFile(t, dir, ".cairn.toml", "project = \"old-api\"\nserver = \"https://old.example:8788\"\n")
	legacyBefore := readBytes(t, legacy)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 || !strings.Contains(stderr, "is a directory, not a project file") || !strings.Contains(stderr, newPath) {
		t.Errorf("exit %d, stderr %q: want 1 and the directory refusal naming %s", code, stderr, newPath)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want nothing", stdout)
	}
	if info, err := os.Stat(newPath); err != nil || !info.IsDir() {
		t.Errorf("%s is no longer a directory: %v", newPath, err)
	}
	if got := dirNames(t, dir); len(got) != 2 {
		t.Errorf("folder holds %v, want the directory and the legacy file only", got)
	}
	if !bytes.Equal(readBytes(t, legacy), legacyBefore) {
		t.Errorf("the legacy file changed: %q", readBytes(t, legacy))
	}
	assertNoDestructive(t, stderr)
}

// TestInitWithBothFilesKeepsTheExistingBehaviour checks #244: when the folder holds the old file and
// .cairnbook.toml, init compares the new file as it does on its own, and leaves the old file as it is.
func TestInitWithBothFilesKeepsTheExistingBehaviour(t *testing.T) {
	t.Run("same values", func(t *testing.T) {
		dir := t.TempDir()
		useDir(t, dir)
		newFile := writeFile(t, dir, ".cairnbook.toml", "project = \"acme-api\"\nserver = \""+initServer+"\"\n")
		legacy := writeFile(t, dir, ".cairn.toml", "project = \"old-api\"\n")
		newBefore, legacyBefore := readBytes(t, newFile), readBytes(t, legacy)

		code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
		if code != 0 || !strings.Contains(stdout, "nothing changed") {
			t.Errorf("exit %d, stdout %q, stderr %q: want 0 and nothing changed", code, stdout, stderr)
		}
		if !bytes.Equal(readBytes(t, newFile), newBefore) || !bytes.Equal(readBytes(t, legacy), legacyBefore) {
			t.Errorf("a file changed")
		}
	})
	t.Run("other values", func(t *testing.T) {
		dir := t.TempDir()
		useDir(t, dir)
		newFile := writeFile(t, dir, ".cairnbook.toml", "project = \"acme-api\"\nserver = \""+initServer+"\"\n")
		legacy := writeFile(t, dir, ".cairn.toml", "project = \"old-api\"\n")
		newBefore, legacyBefore := readBytes(t, newFile), readBytes(t, legacy)

		code, _, stderr := runCmd(t, initArgs("other", initServer)...)
		if code != 1 || !strings.Contains(stderr, "already sets project") {
			t.Errorf("exit %d, stderr %q: want 1 and the refusal of other values", code, stderr)
		}
		if !bytes.Equal(readBytes(t, newFile), newBefore) || !bytes.Equal(readBytes(t, legacy), legacyBefore) {
			t.Errorf("a file changed")
		}
	})
}

// TestInitBelowALegacyFileAboveKeepsTheExistingBehaviour checks #244: a folder below a project file
// under the old name is compared with it as fileAbove has always done. The same values change
// nothing, and other values are refused as shadowing the project.
func TestInitBelowALegacyFileAboveKeepsTheExistingBehaviour(t *testing.T) {
	t.Run("same values", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".cairn.toml", "project = \"acme-api\"\nserver = \""+initServer+"\"\n")
		sub := filepath.Join(root, "service")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		useDir(t, sub)

		code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
		if code != 0 || !strings.Contains(stdout, "nothing changed") {
			t.Errorf("exit %d, stdout %q, stderr %q: want 0 and nothing changed", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(sub, ".cairnbook.toml")); !os.IsNotExist(err) {
			t.Errorf("a file was written below the old project file: %v", err)
		}
	})
	t.Run("other values", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".cairn.toml", "project = \"acme-api\"\nserver = \""+initServer+"\"\n")
		sub := filepath.Join(root, "service")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		useDir(t, sub)

		code, _, stderr := runCmd(t, initArgs("other", initServer)...)
		if code != 1 || !strings.Contains(stderr, "would shadow it") {
			t.Errorf("exit %d, stderr %q: want 1 and the shadowing refusal", code, stderr)
		}
		if _, err := os.Stat(filepath.Join(sub, ".cairnbook.toml")); !os.IsNotExist(err) {
			t.Errorf("a file was written below the old project file: %v", err)
		}
	})
}

// TestInitRejectsBadArguments checks the usage errors, which exit 2 and write
// nothing: a missing flag, a bad name, a server that is not a URL and a stray word.
func TestInitRejectsBadArguments(t *testing.T) {
	cases := map[string][]string{
		"no flags":        {"init"},
		"no server":       {"init", "--project", initProject},
		"bad name":        initArgs("Acme_API", initServer),
		"bad server":      initArgs(initProject, "cairnbook.lan:8788"),
		"server no host":  initArgs(initProject, "https://"),
		"stray argument":  {"init", "--project", initProject, "--server", initServer, "extra"},
		"unknown flag":    {"init", "--force"},
		"bad server type": initArgs(initProject, "ftp://cairnbook.lan"),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			code, _, stderr := runCmd(t, args...)
			if code != 2 {
				t.Errorf("exit %d, want 2 (stderr %q)", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); !os.IsNotExist(err) {
				t.Errorf("a usage error wrote a file: %v", err)
			}
			assertNoDestructive(t, stderr)
		})
	}
}

// TestWriteNewNeverReplacesAFile checks the O_EXCL guard directly: a file that appears
// between the check and the write is not truncated.
func TestWriteNewNeverReplacesAFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".cairnbook.toml", "project = \"keep-me\"\n")
	err := writeNew(path, initProject, initServer)
	if err == nil {
		t.Fatal("writeNew replaced an existing file")
	}
	if string(readBytes(t, path)) != "project = \"keep-me\"\n" {
		t.Errorf("file changed: %q", readBytes(t, path))
	}
}

// TestInitWritesAtomicallyOrNotAtAll checks that a write that fails part way
// leaves no .cairnbook.toml and no temporary file: the file appears whole, or it does not
// appear.
func TestInitWritesAtomicallyOrNotAtAll(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	prev := writeContent
	writeContent = func(w io.Writer, content []byte) error {
		_, _ = w.Write(content[:len(content)/2])
		return errors.New("disk full")
	}
	t.Cleanup(func() { writeContent = prev })

	code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 || !strings.Contains(stderr, "disk full") {
		t.Errorf("exit %d, stderr %q: want 1 and the write error", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a failed write left %v in the directory, want nothing", names)
	}
}

// TestInitLeavesOnlyTheFile checks that a successful write leaves the project file and
// nothing else in the directory: the temporary file is removed once the file is linked.
func TestInitLeavesOnlyTheFile(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	if code, _, stderr := runCmd(t, initArgs(initProject, initServer)...); code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".cairnbook.toml" {
		t.Errorf("the directory holds %d entries, want only .cairnbook.toml", len(entries))
	}
	if info, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); err == nil && info.Mode().Perm() != 0o644 {
		t.Errorf("the file mode is %v, want 0644", info.Mode().Perm())
	}
}

// TestInitRefusesCredentialsInTheServerURL checks that a user name or password
// in the server URL is refused, since the file is committed, and the refusal does not
// print any part of the URL.
func TestInitRefusesCredentialsInTheServerURL(t *testing.T) {
	for _, server := range []string{"https://keeper:secret@cairnbook.lan:8788", "https://keeper@cairnbook.lan:8788"} {
		t.Run(server, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			code, stdout, stderr := runCmd(t, initArgs(initProject, server)...)
			if code != 2 {
				t.Errorf("exit %d, want 2 (stderr %q)", code, stderr)
			}
			if strings.Contains(stderr, "secret") || strings.Contains(stderr, "keeper") || strings.Contains(stdout, "keeper") {
				t.Errorf("the refusal shows the credentials: stdout %q, stderr %q", stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); !os.IsNotExist(err) {
				t.Errorf("a refused URL wrote a file: %v", err)
			}
		})
	}
}

// TestInitWritesWhereIdentityLooks checks that, with CLAUDE_PROJECT_DIR set,
// init writes the file in that directory, even when it runs from a subdirectory, and the
// project resolves from the subdirectory to the value it wrote.
func TestInitWritesWhereIdentityLooks(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "service")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	if code, _, stderr := runCmd(t, initArgs(initProject, initServer)...); code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".cairnbook.toml")); err != nil {
		t.Errorf("the file is not in the project directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairnbook.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written in the subdirectory: %v", err)
	}
	p, err := identity.Resolve(sub)
	if err != nil || p.Name != initProject {
		t.Errorf("identity from the subdirectory = %+v, %v; want %q", p, err, initProject)
	}
}

// TestInitBelowAFileWithTheSameValuesChangesNothing checks that the second half of the
// identity rule holds: init in a directory below a project file that has the same values answers that
// nothing changed, and writes nothing; different values are still refused.
func TestInitBelowAFileWithTheSameValuesChangesNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".cairnbook.toml", "project = \""+initProject+"\"\nserver = \""+initServer+"\"\n")
	sub := filepath.Join(root, "service")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 || !strings.Contains(stdout, "nothing changed") {
		t.Errorf("same values below the file: exit %d, stdout %q, stderr %q: want 0 and nothing changed", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairnbook.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written below the project file: %v", err)
	}

	code, _, stderr = runCmd(t, initArgs("other", initServer)...)
	if code != 1 || !strings.Contains(stderr, "would shadow it") {
		t.Errorf("other values below the file: exit %d, stderr %q: want 1 and the shadowing refusal", code, stderr)
	}
}

// TestInitHelpGoesToStdout checks that init --help is the usage, on stdout,
// and exits 0. Nothing is written to stderr.
func TestInitHelpGoesToStdout(t *testing.T) {
	useDir(t, t.TempDir())
	for _, args := range [][]string{{"init", "--help"}, {"init", "-h"}} {
		code, stdout, stderr := runCmd(t, args...)
		if code != 0 || stderr != "" {
			t.Errorf("%v: exit %d, stderr %q: want 0 and nothing on stderr", args, code, stderr)
		}
		if !strings.Contains(stdout, "Usage: cairnbook init") || !strings.Contains(stdout, "-project") {
			t.Errorf("%v: stdout %q does not carry the usage and the flags", args, stdout)
		}
		if !strings.Contains(stdout, "holds .cairn.toml") || !strings.Contains(stdout, "says how to rename it") {
			t.Errorf("%v: stdout %q does not say init refuses the old file and how to rename it", args, stdout)
		}
	}
}

// stubLink makes every link fail with err, as a filesystem with no hard links does, and
// puts os.Link back when the test ends.
func stubLink(t *testing.T, err error) {
	t.Helper()
	prev := linkFile
	linkFile = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: err}
	}
	t.Cleanup(func() { linkFile = prev })
}

// dirNames returns the names in dir, failing the test when it cannot be read.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestInitFallsBackWhenTheFilesystemHasNoHardLinks checks that a filesystem
// that refuses a hard link gets the file written directly, with the same content, and
// no temporary file is left behind. Each errno that means no hard links takes the
// fallback.
func TestInitFallsBackWhenTheFilesystemHasNoHardLinks(t *testing.T) {
	errnos := map[string]syscall.Errno{
		"EPERM": syscall.EPERM, "ENOTSUP": syscall.ENOTSUP, "EOPNOTSUPP": syscall.EOPNOTSUPP,
		"EXDEV": syscall.EXDEV, "ENOSYS": syscall.ENOSYS,
	}
	for name, errno := range errnos {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			stubLink(t, errno)

			code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
			if code != 0 {
				t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
			}
			want, err := projectContent(initProject, initServer)
			if err != nil {
				t.Fatal(err)
			}
			if got := readBytes(t, filepath.Join(dir, ".cairnbook.toml")); string(got) != string(want) {
				t.Errorf("file:\n%s\nwant:\n%s", got, want)
			}
			if names := dirNames(t, dir); len(names) != 1 || names[0] != ".cairnbook.toml" {
				t.Errorf("the directory holds %v, want only .cairnbook.toml", names)
			}
			if !strings.Contains(stdout, "wrote") {
				t.Errorf("stdout %q does not say the file was written", stdout)
			}
		})
	}
}

// TestInitFallbackRefusesAnExistingFile checks that the fallback keeps the no-overwrite
// rule: when the name is taken, the link is refused and the direct create is refused
// too, and the file is not touched.
func TestInitFallbackRefusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".cairnbook.toml", "project = \"keep-me\"\n")
	stubLink(t, syscall.EPERM)

	err := writeNew(path, initProject, initServer)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("writeNew = %v, want an error that is fs.ErrExist", err)
	}
	if string(readBytes(t, path)) != "project = \"keep-me\"\n" {
		t.Errorf("file changed: %q", readBytes(t, path))
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("the directory holds %v, want only the existing file", names)
	}
}

// TestInitFallbackLeavesNothingOnAFailedWrite checks that a direct write that fails part
// way removes the partial file it made, so no .cairnbook.toml is left behind.
func TestInitFallbackLeavesNothingOnAFailedWrite(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	stubLink(t, syscall.EPERM)
	prev := writeContent
	writeContent = func(w io.Writer, content []byte) error {
		_, _ = w.Write(content[:len(content)/2])
		return errors.New("disk full")
	}
	t.Cleanup(func() { writeContent = prev })

	code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 || !strings.Contains(stderr, "disk full") {
		t.Errorf("exit %d, stderr %q: want 1 and the write error", code, stderr)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("a failed fallback write left %v, want nothing", names)
	}
}

// TestInitDoesNotFallBackOnOtherLinkFailures checks that only the errnos that mean no
// hard links fall back. An existing name, which is a race, and an I/O error are returned,
// and the file is not created another way.
func TestInitDoesNotFallBackOnOtherLinkFailures(t *testing.T) {
	cases := map[string]syscall.Errno{"EEXIST": syscall.EEXIST, "EIO": syscall.EIO}
	for name, errno := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			stubLink(t, errno)
			path := filepath.Join(dir, ".cairnbook.toml")

			err := writeNew(path, initProject, initServer)
			if err == nil {
				t.Fatal("writeNew succeeded, want the link error")
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("the file was created another way: %v", statErr)
			}
			if names := dirNames(t, dir); len(names) != 0 {
				t.Errorf("the directory holds %v, want nothing", names)
			}
		})
	}
}

// TestInitErrorsNameTheProjectFileNotTheTemporaryOne checks that a link failure
// that is not a missing hard link is reported against .cairnbook.toml, never against the
// temporary file the write made.
func TestInitErrorsNameTheProjectFileNotTheTemporaryOne(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	stubLink(t, syscall.EIO)

	code, _, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, filepath.Join(dir, ".cairnbook.toml")) {
		t.Errorf("stderr %q does not name the project file", stderr)
	}
	if strings.Contains(stderr, ".tmp") {
		t.Errorf("stderr %q names the temporary file", stderr)
	}
	if n := strings.Count(stderr, "write "); n != 1 {
		t.Errorf("stderr %q says write %d times, want once", stderr, n)
	}
}

// TestInitRefusesToRunOutsideTheProjectDirectory checks that, with
// CLAUDE_PROJECT_DIR set, a run from a directory outside it is refused with exit 1: the
// message names both directories and says to unset the variable or cd into the project,
// and no file is written in either.
func TestInitRefusesToRunOutsideTheProjectDirectory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	useDir(t, other)
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	for _, want := range []string{root, other, "unset CLAUDE_PROJECT_DIR", "cd into the project"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}
	for _, dir := range []string{root, other} {
		if _, err := os.Stat(filepath.Join(dir, ".cairnbook.toml")); !os.IsNotExist(err) {
			t.Errorf("a file was written in %s: %v", dir, err)
		}
	}
}

// TestInitOutsideTheProjectDirectoryCompares checks the rule's edges: a sibling whose
// name starts the same way is outside, a symlink into the project is inside, and a
// symlink inside the project that leads out is outside, since the paths are resolved
// before they are compared.
func TestInitOutsideTheProjectDirectoryCompares(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "acme")
	sibling := filepath.Join(base, "acme-other")
	sub := filepath.Join(root, "service")
	for _, d := range []string{root, sibling, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// useDir clears CLAUDE_PROJECT_DIR, so the variable is set after each call.
	useDir(t, sibling)
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	if code, _, _ := runCmd(t, initArgs(initProject, initServer)...); code != 1 {
		t.Errorf("a sibling with the same start: exit %d, want 1", code)
	}

	inside := filepath.Join(base, "into-acme")
	if err := os.Symlink(sub, inside); err != nil {
		t.Fatal(err)
	}
	useDir(t, inside)
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	if code, _, stderr := runCmd(t, initArgs(initProject, initServer)...); code != 0 {
		t.Errorf("a symlink into the project: exit %d, want 0 (stderr %q)", code, stderr)
	}

	leak := filepath.Join(root, "leak")
	if err := os.Symlink(sibling, leak); err != nil {
		t.Fatal(err)
	}
	useDir(t, leak)
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	if code, _, _ := runCmd(t, initArgs(initProject, initServer)...); code != 1 {
		t.Errorf("a symlink inside the project that leads out: exit %d, want 1", code)
	}
}

// TestInitAcceptsTheProjectDirectoryUnderAnotherCase checks #114: a run from the project
// directory named in another case is inside it, and writes the project's file. It needs a
// case-insensitive filesystem, such as macOS's default, where the second name is the same
// directory; on a case-sensitive one there is no second name, and the test is skipped.
func TestInitAcceptsTheProjectDirectoryUnderAnotherCase(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "acme")
	variant := filepath.Join(base, "ACME")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(variant); err != nil || !os.SameFile(info, rootInfo) {
		t.Skip("the filesystem is case-sensitive: the project has no second name in another case")
	}

	useDir(t, variant)
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".cairnbook.toml")); err != nil {
		t.Errorf("the project file was not written in the project: %v", err)
	}
}

// TestInitDoesNotPrintACredentialFromAnExistingFile checks that a file that
// already names a server with a user name and a password is refused by a run with other
// values, and the refusal shows the server without them.
func TestInitDoesNotPrintACredentialFromAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairnbook.toml", "project = \""+initProject+"\"\nserver = \"https://keeper:secret@cairnbook.lan:8788\"\n")

	code, stdout, stderr := runCmd(t, initArgs("other-project", initServer)...)
	if code != 1 {
		t.Errorf("exit %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	assertNoCredentials(t, stdout, stderr)
	if !strings.Contains(stderr, `"https://cairnbook.lan:8788"`) {
		t.Errorf("stderr %q does not show the server without its credentials", stderr)
	}
}
