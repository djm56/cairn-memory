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

// TestRefusalSkipsAStraysOnlyNewestStamp is the proof for 16-S2. The newest stamp holds only a
// -wal and a -shm, as a restore with the database absent leaves. The refusal names the newest
// stamp that has a main file, never the -wal, and its mv command puts a main file at the path.
func TestRefusalSkipsAStraysOnlyNewestStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	oldMain := path + ".pre-restore-20261030T090000Z"
	newStray := path + ".pre-restore-20261031T101112Z"
	for _, name := range []string{oldMain, newStray + "-wal", newStray + "-shm"} {
		if err := os.WriteFile(name, []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := refuseNewOverRestore(path)
	if err == nil {
		t.Fatal("refuseNewOverRestore with a strays-only newest stamp returned nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"The newest is " + oldMain + ",",
		"mv -- '" + oldMain + "' '" + path + "'",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not contain %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "mv -- '"+newStray+"-wal'") || strings.Contains(msg, "mv -- '"+newStray+"-shm'") {
		t.Errorf("the refusal offers a -wal or -shm to put at the database path:\n%s", msg)
	}
}

// TestRefusalOffersNoMvWhenOnlyStraysRemain is the other half of 16-S2: with no main file of any
// stamp beside the path, the refusal still stops the server but prints no mv command.
func TestRefusalOffersNoMvWhenOnlyStraysRemain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	stray := path + ".pre-restore-20261031T101112Z-wal"
	if err := os.WriteFile(stray, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := refuseNewOverRestore(path)
	if err == nil {
		t.Fatal("refuseNewOverRestore with only strays returned nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "mv --") {
		t.Errorf("the refusal offers an mv with only a -wal beside the path:\n%s", msg)
	}
	if !strings.Contains(msg, stray) || !strings.Contains(msg, "may have been interrupted") {
		t.Errorf("the refusal does not name the stray file or the possible interruption:\n%s", msg)
	}
	if _, serr := os.Lstat(path); serr == nil {
		t.Errorf("the refusal created %s", path)
	}
}
