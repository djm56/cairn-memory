package identity

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestResolveServer covers the order of ResolveServer: CAIRN_SERVER first, then
// the server key of the nearest .cairn.toml, with CLAUDE_PROJECT_DIR as the start
// of the walk. A call with neither is invalid and names cairn init.
//
// As in TestResolve, the "no file" case walks up from the temporary directory, so
// a stray .cairn.toml above it would change that result. The trade-off is recorded
// there.
func TestResolveServer(t *testing.T) {
	const good = "project = \"acme-api\"\nserver = \"https://cairn.lan:8787\"\n"
	cases := []struct {
		name string
		// env is CAIRN_SERVER. Empty means unset.
		env string
		// claudeDir is CLAUDE_PROJECT_DIR, relative to the tree root. Empty means unset.
		claudeDir string
		// work is the working directory, relative to the tree root.
		work  string
		files []file

		want     string
		wantErr  core.Kind // "" for success
		wantText string    // when set, must appear in the error message
	}{
		{name: "env set, no file", env: "http://127.0.0.1:9000", work: "app", want: "http://127.0.0.1:9000"},
		{
			name: "env set beats the file", env: "http://127.0.0.1:9000", work: "app",
			files: []file{{"app/.cairn.toml", good}}, want: "http://127.0.0.1:9000",
		},
		{name: "file server key", work: "app", files: []file{{"app/.cairn.toml", good}}, want: "https://cairn.lan:8787"},
		{
			name: "file found by walking up", work: "app/src",
			files: []file{{".cairn.toml", good}}, want: "https://cairn.lan:8787",
		},
		{
			name: "claude project dir is the start of the walk", work: "elsewhere", claudeDir: "app",
			files: []file{{"app/.cairn.toml", good}, {"elsewhere/.cairn.toml", "project = \"x\"\nserver = \"https://wrong\"\n"}},
			want:  "https://cairn.lan:8787",
		},
		{name: "no env and no file names cairn init", work: "app", wantErr: core.KindInvalid, wantText: "cairn init"},
		{
			name: "file with no server key", work: "app",
			files:   []file{{"app/.cairn.toml", "project = \"acme-api\"\n"}},
			wantErr: core.KindInvalid, wantText: "has no server key",
		},
		{
			name: "file with an empty server", work: "app",
			files:   []file{{"app/.cairn.toml", "project = \"acme-api\"\nserver = \"\"\n"}},
			wantErr: core.KindInvalid, wantText: "has no server key",
		},
		{
			name: "file with a server that is not a string", work: "app",
			files:   []file{{"app/.cairn.toml", "project = \"acme-api\"\nserver = 8787\n"}},
			wantErr: core.KindInvalid, wantText: "wrong type for server",
		},
		{
			name: "file that is not TOML", work: "app",
			files:   []file{{"app/.cairn.toml", "server = \n"}},
			wantErr: core.KindInvalid, wantText: "not valid TOML",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := newTree(t, c.work, c.files)
			setEnv(t, EnvServer, c.env, c.env == "")
			if c.claudeDir != "" {
				setEnv(t, EnvProjectDir, filepath.Join(tr.root, c.claudeDir), false)
			} else {
				setEnv(t, EnvProjectDir, "", true)
			}

			got, err := ResolveServer(filepath.Join(tr.root, c.work))
			if c.wantErr == "" {
				if err != nil || got != c.want {
					t.Fatalf("ResolveServer = %q, %v; want %q", got, err, c.want)
				}
				return
			}
			if core.KindOf(err) != c.wantErr {
				t.Fatalf("ResolveServer error %v (kind %q); want kind %q", err, core.KindOf(err), c.wantErr)
			}
			if c.wantText != "" && !strings.Contains(err.Error(), c.wantText) {
				t.Errorf("error %q does not contain %q", err, c.wantText)
			}
		})
	}
}
