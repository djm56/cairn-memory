package plugincheck

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
	cairnmcp "github.com/djm56/cairn-memory/internal/mcp"
)

// skillFile is the cairn skill of the plugin, relative to this package.
const skillFile = pluginDir + "/skills/cairn/SKILL.md"

// skillArguments are the argument, field and refusal names the skill writes in backticks or in its examples, which are not
// tool names. Every other lowercase name the skill cites must be a registered MCP tool. A name added to the skill that is
// neither a tool nor listed here fails TestSkillNamesOnlyRegisteredTools, so add it here when it is an argument.
var skillArguments = []string{
	"action", "active_steps", "after", "agent", "assignee", "correction", "criteria", "evidence", "goal", "id", "impact",
	"kind", "lessons", "missing", "mistake", "mistakes", "no_lessons_reason", "number", "ord", "outcome", "prevention",
	"result", "step", "steps", "summary", "task_id", "text", "title", "why",
}

// skillTools are the tools the task loop and the close and insert instructions depend on. The skill must name each one.
var skillTools = []string{
	"directive_list", "event_log", "learning_add", "recall", "step_add", "step_update", "task_close", "task_current", "task_start",
}

// backtickName matches a backticked lowercase name, such as `task_current`. A name with a hyphen or a capital is not one.
var backtickName = regexp.MustCompile("`([a-z][a-z_]*)`")

// jsonToolName matches the "name" of a tool call in an example.
var jsonToolName = regexp.MustCompile(`"name": "([a-z][a-z_]*)"`)

// readSkill returns the text of the skill.
func readSkill(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// frontValue returns the value of the key line in the frontmatter text front, with the key and the colon removed.
func frontValue(front, key string) string {
	for _, line := range strings.Split(front, "\n") {
		if rest, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// TestSkillFrontmatter checks the frontmatter the skills docs require: the file opens with the --- line, the name is cairn
// (the directory name), the description is present, is one line, is within the 1,536 characters the skill listing keeps,
// and has no angle brackets, which the docs say show escaped. The skill is under 500 lines, as the docs advise.
func TestSkillFrontmatter(t *testing.T) {
	raw := readSkill(t)
	front, body, ok := splitFrontmatter(raw)
	if !ok {
		t.Fatalf("%s has no frontmatter between --- lines", skillFile)
	}
	if name := frontValue(front, "name"); name != "cairn" {
		t.Errorf("name is %q, want cairn", name)
	}
	desc := frontValue(front, "description")
	switch {
	case desc == "":
		t.Errorf("description is empty; Claude Code loads the skill from it")
	case utf8.RuneCountInString(desc) > 1536:
		t.Errorf("description is %d characters, want at most 1536", utf8.RuneCountInString(desc))
	case strings.ContainsAny(desc, "<>"):
		t.Errorf("description holds an angle bracket, which the docs say to avoid")
	}
	if body == "" {
		t.Errorf("the skill has no text after its frontmatter")
	}
	if n := strings.Count(raw, "\n"); n >= 500 {
		t.Errorf("%s is %d lines, want under 500", skillFile, n)
	}
}

// skillNames returns the tool and argument names the skill cites: each backticked lowercase name outside a code fence, and
// each "name" value in a fenced example. Each name is listed once, in the order the skill gives it.
func skillNames(body string) []string {
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			for _, m := range jsonToolName.FindAllStringSubmatch(line, -1) {
				add(m[1])
			}
			continue
		}
		for _, m := range backtickName.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
	}
	return names
}

// registeredTools returns the names of the tools the cairn MCP server lists, read over the protocol with an in-memory
// transport, as the MCP-Tools drift test reads them (internal/mcp/docs_drift_test.go).
func registeredTools(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := cairnmcp.New(core.Unimplemented{}, cairnmcp.Project{Name: "acme-api"}, "test")
	go func() { _ = srv.Run(ctx, serverT) }()

	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "plugincheck", Version: "0"}, nil)
	cs, err := mc.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect the MCP client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestSkillNamesOnlyRegisteredTools checks that the skill can only name tools the server registers, so it cannot drift from
// the server. Every name it cites is a registered tool or a listed argument, the loop's tools are all named, and the skill
// states no MCP prefix: the prefix is confirmed in a live session (issue #26), not here.
func TestSkillNamesOnlyRegisteredTools(t *testing.T) {
	registered := registeredTools(t)
	_, body, ok := splitFrontmatter(readSkill(t))
	if !ok {
		t.Fatalf("%s has no frontmatter", skillFile)
	}
	names := skillNames(body)

	var unknown []string
	for _, name := range names {
		if !slices.Contains(registered, name) && !slices.Contains(skillArguments, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		t.Errorf("the skill names %s, which the server does not register as a tool and which is not a listed argument", strings.Join(unknown, ", "))
	}
	for _, want := range skillTools {
		if !slices.Contains(names, want) {
			t.Errorf("the skill does not name the tool %s", want)
		}
		if !slices.Contains(registered, want) {
			t.Errorf("the server does not register %s, which the skill's task loop names", want)
		}
	}
	if strings.Contains(body, "mcp__") {
		t.Errorf("the skill states an MCP tool prefix; the prefix is not confirmed until the live check (issue #26)")
	}
}
