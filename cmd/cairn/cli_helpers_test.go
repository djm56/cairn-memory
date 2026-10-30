package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useDir runs the commands from dir, with no caller set in the environment: the
// project and server come only from the files the test writes.
func useDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	prev := workDir
	workDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { workDir = prev })
}

// runCmd runs one command line and returns its exit code and both streams.
func runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// writeFile writes content to name under dir, and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// readBytes returns the bytes of path, failing the test when it cannot be read.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// destructive are the words no message may suggest (#70, CONTRACT §1.2). The audit
// reads the source; these tests read what a person is actually shown.
var destructive = []string{"--force", "rm ", "reset", "overwrite"}

// assertNoDestructive fails when any of the texts names a destructive step.
func assertNoDestructive(t *testing.T, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, word := range destructive {
			if strings.Contains(text, word) {
				t.Errorf("output %q names %q, which is destructive advice", text, word)
			}
		}
	}
}
