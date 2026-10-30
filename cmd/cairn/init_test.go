package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/djm56/cairn-memory/internal/identity"
)

const (
	initProject = "acme-api"
	initServer  = "https://cairn.lan:8787"
)

// initArgs is the command line of cairn init for the test project and server.
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
	path := filepath.Join(dir, ".cairn.toml")
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
	path := filepath.Join(dir, ".cairn.toml")
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
	path := filepath.Join(dir, ".cairn.toml")
	before := readBytes(t, path)

	for _, args := range [][]string{
		initArgs("other-project", initServer),
		initArgs(initProject, "https://elsewhere.lan:8787"),
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
	content := "project = \"acme-api\"\nserver = \"https://cairn.lan:8787\"\nnote = \"kept\"\n\n[extra]\nvalue = 1\n"
	path := writeFile(t, dir, ".cairn.toml", content)

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
		"project not text":  "project = 3\nserver = \"https://cairn.lan:8787\"\n",
		"no project at all": "server = \"https://cairn.lan:8787\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			path := writeFile(t, dir, ".cairn.toml", content)
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

// TestInitRefusesADirectoryInTheWay checks that a .cairn.toml directory is refused,
// and the message says to move it aside by hand.
func TestInitRefusesADirectoryInTheWay(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, ".cairn.toml"), 0o755); err != nil {
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
	writeFile(t, root, ".cairn.toml", "project = \"acme-api\"\nserver = \"https://cairn.lan:8787\"\n")
	sub := filepath.Join(root, "service")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)

	code, _, stderr := runCmd(t, initArgs("other", initServer)...)
	if code != 1 || !strings.Contains(stderr, "would shadow it") {
		t.Errorf("exit %d, stderr %q: want 1 and the shadowing refusal", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairn.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written below the project file: %v", err)
	}
}

// TestInitRejectsBadArguments checks the usage errors, which exit 2 and write
// nothing: a missing flag, a bad name, a server that is not a URL and a stray word.
func TestInitRejectsBadArguments(t *testing.T) {
	cases := map[string][]string{
		"no flags":        {"init"},
		"no server":       {"init", "--project", initProject},
		"bad name":        initArgs("Acme_API", initServer),
		"bad server":      initArgs(initProject, "cairn.lan:8787"),
		"server no host":  initArgs(initProject, "https://"),
		"stray argument":  {"init", "--project", initProject, "--server", initServer, "extra"},
		"unknown flag":    {"init", "--force"},
		"bad server type": initArgs(initProject, "ftp://cairn.lan"),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			code, _, stderr := runCmd(t, args...)
			if code != 2 {
				t.Errorf("exit %d, want 2 (stderr %q)", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, ".cairn.toml")); !os.IsNotExist(err) {
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
	path := writeFile(t, dir, ".cairn.toml", "project = \"keep-me\"\n")
	err := writeNew(path, initProject, initServer)
	if err == nil {
		t.Fatal("writeNew replaced an existing file")
	}
	if string(readBytes(t, path)) != "project = \"keep-me\"\n" {
		t.Errorf("file changed: %q", readBytes(t, path))
	}
}

// TestInitWritesAtomicallyOrNotAtAll is step 10 review W4. A write that fails part way
// leaves no .cairn.toml and no temporary file: the file appears whole, or it does not
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
	if len(entries) != 1 || entries[0].Name() != ".cairn.toml" {
		t.Errorf("the directory holds %d entries, want only .cairn.toml", len(entries))
	}
	if info, err := os.Stat(filepath.Join(dir, ".cairn.toml")); err == nil && info.Mode().Perm() != 0o644 {
		t.Errorf("the file mode is %v, want 0644", info.Mode().Perm())
	}
}

// TestInitRefusesCredentialsInTheServerURL is step 10 review W5. A user name or password
// in the server URL is refused, since the file is committed, and the refusal does not
// print any part of the URL.
func TestInitRefusesCredentialsInTheServerURL(t *testing.T) {
	for _, server := range []string{"https://keeper:secret@cairn.lan:8787", "https://keeper@cairn.lan:8787"} {
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
			if _, err := os.Stat(filepath.Join(dir, ".cairn.toml")); !os.IsNotExist(err) {
				t.Errorf("a refused URL wrote a file: %v", err)
			}
		})
	}
}

// TestInitWritesWhereIdentityLooks is step 10 review W6. With CLAUDE_PROJECT_DIR set,
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
	if _, err := os.Stat(filepath.Join(root, ".cairn.toml")); err != nil {
		t.Errorf("the file is not in the project directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairn.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written in the subdirectory: %v", err)
	}
	p, err := identity.Resolve(sub)
	if err != nil || p.Name != initProject {
		t.Errorf("identity from the subdirectory = %+v, %v; want %q", p, err, initProject)
	}
}

// TestInitBelowAFileWithTheSameValuesChangesNothing is step 10 review W6, the second
// half: init in a directory below a project file that has the same values answers that
// nothing changed, and writes nothing; different values are still refused.
func TestInitBelowAFileWithTheSameValuesChangesNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".cairn.toml", "project = \""+initProject+"\"\nserver = \""+initServer+"\"\n")
	sub := filepath.Join(root, "service")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)

	code, stdout, stderr := runCmd(t, initArgs(initProject, initServer)...)
	if code != 0 || !strings.Contains(stdout, "nothing changed") {
		t.Errorf("same values below the file: exit %d, stdout %q, stderr %q: want 0 and nothing changed", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(sub, ".cairn.toml")); !os.IsNotExist(err) {
		t.Errorf("a file was written below the project file: %v", err)
	}

	code, _, stderr = runCmd(t, initArgs("other", initServer)...)
	if code != 1 || !strings.Contains(stderr, "would shadow it") {
		t.Errorf("other values below the file: exit %d, stderr %q: want 1 and the shadowing refusal", code, stderr)
	}
}

// TestInitHelpGoesToStdout is step 10 review S7. init --help is the usage, on stdout,
// and exits 0. Nothing is written to stderr.
func TestInitHelpGoesToStdout(t *testing.T) {
	useDir(t, t.TempDir())
	for _, args := range [][]string{{"init", "--help"}, {"init", "-h"}} {
		code, stdout, stderr := runCmd(t, args...)
		if code != 0 || stderr != "" {
			t.Errorf("%v: exit %d, stderr %q: want 0 and nothing on stderr", args, code, stderr)
		}
		if !strings.Contains(stdout, "Usage: cairn init") || !strings.Contains(stdout, "-project") {
			t.Errorf("%v: stdout %q does not carry the usage and the flags", args, stdout)
		}
	}
}
