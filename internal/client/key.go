package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// EnvKey is the environment variable that holds the API key a client sends (DESIGN.md
// §2.1). It is read before the key file.
const EnvKey = "CAIRN_API_KEY"

// KeySource says where a key came from. Its values are the words cairn doctor prints.
type KeySource string

const (
	// SourceEnv: the key came from CAIRN_API_KEY.
	SourceEnv KeySource = "env"
	// SourceFile: the key came from the client.env file under ~/.config/cairn.
	SourceFile KeySource = "client.env"
)

// Key is the API key a client sends, and where it was found. Its Value is a secret, so
// the key's text forms show the source and never the value. A printed Key shows only that
// it came from a source, so a stray print cannot leak the key.
type Key struct {
	Value  string
	Source KeySource
}

// String shows where the key came from, never the key.
func (k Key) String() string { return fmt.Sprintf("API key from %s", k.Source) }

// GoString is String for %#v, so a debug print shows no value either.
func (k Key) GoString() string { return fmt.Sprintf("client.Key{Source: %q}", k.Source) }

// ClientEnvPath returns the file a key is read from when CAIRN_API_KEY is not set:
// ~/.config/cairn/client.env (DESIGN.md §2.1). The path is fixed. XDG_CONFIG_HOME is not
// read, since nothing else in Cairn reads it, and a second location would be a second
// place for a key to be left behind.
func ClientEnvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory for the API key file: %w", err)
	}
	return filepath.Join(home, ".config", "cairn", "client.env"), nil
}

// LoadKey returns the API key this process sends, and where it came from. CAIRN_API_KEY
// is read first; a value that is empty or only white space counts as unset. Otherwise the
// file ClientEnvPath names is read, when it exists.
//
// A key that is in neither place is not an error: the returned Key has no source and no
// value, and the caller says what to do with NoKeyMessage. A key file that any other user
// can read or write is an error, and nothing is read from it, since a key any other user can
// read is not a key. Errors name the file and the fix, and never a key.
func LoadKey() (Key, error) {
	if v := strings.TrimSpace(os.Getenv(EnvKey)); v != "" {
		return Key{Value: v, Source: SourceEnv}, nil
	}
	path, err := ClientEnvPath()
	if err != nil {
		return Key{}, err
	}
	v, err := readKeyFile(path)
	if err != nil {
		return Key{}, err
	}
	if v == "" {
		return Key{}, nil
	}
	return Key{Value: v, Source: SourceFile}, nil
}

// NoKeyMessage is the instruction a command gives when LoadKey finds no key: the two places a
// key can be given. The caller frames it, as "no API key: " or "missing: ". It never names a
// key.
func NoKeyMessage() string {
	path, err := ClientEnvPath()
	if err != nil {
		return fmt.Sprintf("set %s, or write %s=<key> to the client.env file under ~/.config/cairn", EnvKey, EnvKey)
	}
	return fmt.Sprintf("set %s, or write %s=<key> to %s", EnvKey, EnvKey, path)
}

// readKeyFile returns the key in the file at path, or "" when the file does not exist or
// names no key. The mode is checked on the open file, so the check and the read are of the
// same file. Any group or other permission is refused.
func readKeyFile(path string) (string, error) {
	// The file is opened without a prior stat, and the kind is checked on the descriptor below.
	// A named pipe is refused as not a regular file (8-S1). Opening a named pipe for reading
	// waits for a writer, so the open is non-blocking (step 10 review 10-S3): a FIFO there
	// returns at once, and the kind check refuses it. A regular file reads the same either way,
	// so the flag changes nothing else. A symbolic link is followed by the open, so its target
	// is what is checked.
	// This does not close a race in which a regular file is swapped for a FIFO between two
	// checks: no check precedes the open, so none can be raced. Only the descriptor's kind counts.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the API key file %s: %w", path, err)
	}
	defer f.Close()
	// The mode and the kind are checked on the open file, so they are those of the file read.
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read the API key file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", notRegular(path)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return "", fmt.Errorf("the API key file %s is open to other users (mode %04o): run chmod 600 %s, then try again", path, mode, path)
	}
	v, err := parseKeyFile(f)
	if err != nil {
		return "", fmt.Errorf("the API key file %s: %w", path, err)
	}
	return v, nil
}

// notRegular is the refusal of a key file that is not a regular file. It names the file and the
// fix, and never a key.
func notRegular(path string) error {
	return fmt.Errorf("the API key file %s is not a regular file: replace it with a file that holds the key, as the README shows", path)
}

// parseKeyFile returns the value of the last CAIRN_API_KEY line in r, or "" when there is
// none or its value is empty. The file is read as shell-style lines: a blank line or one
// that starts with # is skipped, a line may begin with "export ", as a shell's does, a value may
// be wrapped in one pair of quotes, and any other name is ignored. A line with no '=' is
// refused by its number and not its text, since it may be a key pasted without its name.
func parseKeyFile(r io.Reader) (string, error) {
	var value string
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, v, ok := strings.Cut(line, "=")
		if !ok {
			return "", fmt.Errorf("line %d has no '=': each line is %s=<key>", n, EnvKey)
		}
		if strings.TrimSpace(name) != EnvKey {
			continue
		}
		value = unquote(strings.TrimSpace(v))
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	return strings.TrimSpace(value), nil
}

// unquote removes one pair of matching single or double quotes from around v.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
