package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
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
