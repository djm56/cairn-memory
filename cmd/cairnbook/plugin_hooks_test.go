package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// pluginHooksFile is the plugin's hooks file, relative to this package.
const pluginHooksFile = "../../plugins/claude-code/hooks/hooks.json"

// pluginHookPrefix is how each hook of the plugin starts its command: the launcher, with the plugin's root quoted (the
// quoting keeps a plugin path with spaces as one word).
const pluginHookPrefix = `"${CLAUDE_PLUGIN_ROOT}"/bin/cairnbook-launch hook `

// TestPluginHooksMatchEnable checks that the plugin's hooks/hooks.json registers the same events, matchers and cairnbook
// subcommands as the hooks cairnbook enable installs (claudeHooks). The plugin carries the hooks in place of enable (D34), so the
// two must not drift apart.
func TestPluginHooksMatchEnable(t *testing.T) {
	raw, err := os.ReadFile(pluginHooksFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("hooks.json is not JSON: %v", err)
	}

	var got []string
	for event, groups := range doc.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Type != "command" {
					t.Errorf("%s hook has type %q, want command", event, h.Type)
				}
				sub, ok := strings.CutPrefix(h.Command, pluginHookPrefix)
				if !ok {
					t.Errorf("%s runs %q, want the launcher with hook and a subcommand", event, h.Command)
					continue
				}
				got = append(got, event+" | "+g.Matcher+" | cairnbook hook "+sub)
			}
		}
	}

	var want []string
	for _, h := range claudeHooks {
		want = append(want, h.event+" | "+h.matcher+" | "+h.cmdline)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("hooks.json registers\n%s\nwant what enable installs\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
