# MCP tools

The fifteen tools that `cairn mcp` serves to an agent, one line each.

<!-- Expanded by the doc writer. Keep the cairn-mcp-tools markers, and keep the first column as one tool name in backticks: internal/mcp/docs_drift_test.go reads this table and fails when it differs from the server's registry. -->
<!-- cairn-mcp-tools:begin -->
| Tool | Description |
|------|-------------|
| `task_current` | The active task, steps, events, and the directives for this project and your agent (agent arg). Ranked, at most 12; expired and omitted count the rest. |
| `task_start` | Start a task with its brief and an optional plan. Returns its id. Refused while another task is active, unless the project allows parallel tasks. |
| `task_get` | One task in full, or one section: brief, steps, events or close. events is the paged journal; limit, cursor and step filter it. |
| `task_list` | Recent tasks in this project, most recently updated first. Filter by status; paged. |
| `task_update` | Change a task's status, priority, owner, next action or blockers, or edit its brief before any step starts. Only the fields given change. |
| `task_close` | Close a task with its outcome, summary, criteria results, lessons and mistakes. Refused with the list of what is missing. |
| `step_plan` | Replace the task's pending steps with this list. Started steps are kept. A pending step that has an event cannot be removed: skip it first. |
| `step_add` | Insert one step after a given step, or at the end of the plan. after number 0 puts it first. |
| `step_update` | Start, finish, fail or skip a step. finish, fail and skip need a summary. assignee reassigns the step. |
| `event_log` | Append a journal event: note, blocked, unblocked, handoff, decision, review or custom. A review needs data with verdict, critical and warnings. |
| `learning_add` | Propose a finding, pattern or agent note. It is stored as proposed; the operator decides what happens to it. |
| `learning_list` | Learnings in this project by status, kind or target, newest first. Set all_projects to see every project. Paged. |
| `directive_list` | Directives for this project, ranked agent, project, global; at most 12. expired: past review_by, left out. omitted: beyond the 12. agent: one role, or empty for every role. |
| `recall` | For a goal: directives, lessons, mistakes and similar tasks, from all projects. Directives: this project and your agent (agent arg), ranked, at most 12; expired, omitted count the rest. |
| `search` | Full-text search over tasks, events, learnings, directives, lessons and mistakes. Plain words only. Paged. |
<!-- cairn-mcp-tools:end -->
