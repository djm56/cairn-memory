package server

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startFreshText is the step every refusal over pre-restore files gives (14b-W3). The test pins
// each part of it: the check, the move by hand out of the folder, the start over, and that nothing
// is deleted.
var startFreshText = []string{
	"To start a new, empty database instead, check these files, then move every one of them out of this folder yourself, to a folder of your own",
	"mkdir -p -- <folder> && mv -- ",
	"Then start cairn serve again. Nothing is deleted.",
}

// TestRefusalsTellTheOperatorHowToStartFresh pins the text of 14b-W3 in both refusals over pre-restore
// files: the one with a main file beside the path, and the one with only -wal and -shm files. Each
// names every file it found, quoted, in the mv command.
func TestRefusalsTellTheOperatorHowToStartFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	oldMain := path + ".pre-restore-20261030T090000Z"
	newMain := path + ".pre-restore-20261031T101112Z"
	for _, name := range []string{oldMain, newMain} {
		if err := os.WriteFile(name, []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	withMain := refuseNewOverRestore(path)
	if withMain == nil {
		t.Fatal("refuseNewOverRestore with two main files returned nil")
	}
	for _, want := range startFreshText {
		if !strings.Contains(withMain.Error(), want) {
			t.Errorf("the refusal with a main file does not contain %q:\n%v", want, withMain)
		}
	}
	for _, name := range []string{oldMain, newMain} {
		if !strings.Contains(withMain.Error(), shellQuote(name)) {
			t.Errorf("the refusal does not name %s quoted for the mv command:\n%v", name, withMain)
		}
	}

	// Only strays: the same text, and the mv goes to a folder, never to the database path.
	strayDir := t.TempDir()
	strayPath := filepath.Join(strayDir, "cairn.db")
	stray := strayPath + ".pre-restore-20261031T101112Z-wal"
	if err := os.WriteFile(stray, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	strays := refuseNewOverRestore(strayPath)
	if strays == nil {
		t.Fatal("refuseNewOverRestore with only strays returned nil")
	}
	for _, want := range startFreshText {
		if !strings.Contains(strays.Error(), want) {
			t.Errorf("the stray-only refusal does not contain %q:\n%v", want, strays)
		}
	}
}

// TestRefusalForAMissingFolderIsNotAnInterruptedRestore is the proof for 16b-W2. The folder the path
// is in does not exist, so no restore could have left files there. The error is the plain missing-
// folder error, which wraps fs.ErrNotExist and does not talk of an interrupted restore. The key
// command goes through openDatabase, which is where the operator sees it.
func TestRefusalForAMissingFolderIsNotAnInterruptedRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-folder", "cairn.db")
	err := refuseNewOverRestore(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("refuseNewOverRestore in a missing folder: err = %v, want the plain not-exist error", err)
	}
	if strings.Contains(err.Error(), "interrupted") || strings.Contains(err.Error(), "restore") {
		t.Errorf("the missing-folder error reads as a restore: %v", err)
	}
	if _, oerr := openDatabase(path, true); oerr == nil || strings.Contains(oerr.Error(), "interrupted") {
		t.Errorf("openDatabase in a missing folder: err = %v, want the plain not-exist error", oerr)
	} else if !errors.Is(oerr, fs.ErrNotExist) {
		t.Errorf("openDatabase in a missing folder: err = %v, want it to wrap fs.ErrNotExist", oerr)
	}
}

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
	// The start-fresh step moves the stray out to a folder of the operator's. What must not be
	// offered is a move onto the database path, which would put a -wal where the database goes.
	if strings.Contains(msg, shellQuote(path)) {
		t.Errorf("the refusal offers a move onto the database path with only a -wal beside it:\n%s", msg)
	}
	if !strings.Contains(msg, stray) || !strings.Contains(msg, "may have been interrupted") {
		t.Errorf("the refusal does not name the stray file or the possible interruption:\n%s", msg)
	}
	if _, serr := os.Lstat(path); serr == nil {
		t.Errorf("the refusal created %s", path)
	}
}
