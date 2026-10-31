package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefusalNamesTheNewestOfTwoStamps is the proof for 14b-W3. Two restores left pre-restore files
// beside an absent database: an old stamp with its -shm, and a newer stamp. The refusal names the
// newer file as the one to put back, lists both stamps, and says it may be history. The old file
// is not named as the database to recover.
func TestRefusalNamesTheNewestOfTwoStamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	oldMain := path + ".pre-restore-20261030T090000Z"
	newMain := path + ".pre-restore-20261031T101112Z"
	for _, name := range []string{oldMain, oldMain + "-shm", newMain} {
		if err := os.WriteFile(name, []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := refuseNewOverRestore(path)
	if err == nil {
		t.Fatal("refuseNewOverRestore over two stamps returned nil")
	}
	msg := err.Error()
	t.Logf("refusal: %s", msg)
	for _, want := range []string{
		"may have been interrupted",
		"The newest is " + newMain + ",",
		"20261030T090000Z, 20261031T101112Z",
		"these files are history",
		"mv -- '" + newMain + "' '" + path + "'",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not contain %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "The newest is "+oldMain) {
		t.Errorf("the refusal names the oldest stamp as the newest:\n%s", msg)
	}
	if _, serr := os.Lstat(path); serr == nil {
		t.Errorf("the refusal created %s", path)
	}
}
