package plugincheck

import (
	"os"
	"strings"
)

// init clears the environment variables of their old CAIRN_ names, from before the rename to
// Cairnbook, so a machine that still sets one cannot change what a test sees. A test that
// checks the old names sets them itself.
func init() {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "CAIRN_") {
			os.Unsetenv(name)
		}
	}
}
