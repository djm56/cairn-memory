# CLI reference

The verbs of the `cairn` binary, one line each, as `cairn help` lists them.

<!-- Expanded by the doc writer. Keep the cairn-cli-verbs markers, and keep the first column as one verb name in backticks: cmd/cairn/docs_drift_test.go reads this table and fails when it differs from the binary's dispatch table. -->
<!-- cairn-cli-verbs:begin -->
| Verb | Summary |
|------|---------|
| `version` | Print the cairn version |
| `help` | List the sub-commands |
| `mcp` | Serve the MCP tools over stdio, through the Cairn server |
| `init` | Write .cairn.toml: the project name and the server |
| `export` | Print a task, or the project's tasks, as Markdown: export task [id] or export project |
| `hook` | Output for a client hook: session-start, pre-compact or stop |
| `enable` | Install a client's hooks into the project: enable --agent claude-code [--dry-run] |
| `status` | Show the server, the project and the active task |
| `task` | Read or write a task: task current\|get\|list\|start\|update\|close, each with --json |
| `step` | Add, update or plan the steps of a task: step add\|update\|plan, each with --json |
| `event` | Log an event on a task: event log, with --json |
| `learning` | Propose a learning or list them: learning add\|list, each with --json |
| `directive` | List the directives that apply: directive list, with --json |
| `recall` | What applies to a goal: recall --goal text [--agent role], with --json |
| `search` | Full-text search of tasks, events, learnings, directives and notes: search --query text, with --json |
| `prime` | The session-start view for an agent with only a shell: prime [--agent role], with --json |
| `doctor` | Check the project file, the server and the connection; changes nothing |
| `serve` | Run the Cairn server: serve --db <file> [--addr host:port] |
| `key` | Make, list and revoke API keys: key add\|list\|revoke --db <file> |
| `backup` | Copy the server's database to a new file: backup --db <file> <out> |
| `restore` | Replace the server's database from a backup, after a typed confirmation: restore --db <file> <backup> |
<!-- cairn-cli-verbs:end -->
