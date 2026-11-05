package hardlink

import (
	"io/fs"
	"os"
	"syscall"
	"testing"
)

// TestUnsupportedNamesOnlyTheErrnosThatMeanNoLinks checks the errnos that make the hard-link clause
// of a refusal. Only the not-supported errnos qualify. A name that is taken, and any other
// failure, do not, so their messages do not blame the filesystem.
func TestUnsupportedNamesOnlyTheErrnosThatMeanNoLinks(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"EPERM":      {syscall.EPERM, true},
		"ENOTSUP":    {syscall.ENOTSUP, true},
		"EOPNOTSUPP": {syscall.EOPNOTSUPP, true},
		"EXDEV":      {syscall.EXDEV, true},
		"ENOSYS":     {syscall.ENOSYS, true},
		"EEXIST":     {syscall.EEXIST, false},
		"EIO":        {syscall.EIO, false},
		"ENOENT":     {fs.ErrNotExist, false},
	}
	for name, c := range cases {
		if got := Unsupported(&os.LinkError{Op: "link", Old: "a", New: "b", Err: c.err}); got != c.want {
			t.Errorf("Unsupported(%s) = %v, want %v", name, got, c.want)
		}
	}
}
