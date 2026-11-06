# MCP tools

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da: the tool names and the parameter lists come from `internal/mcp/tools.go`, and the results from `internal/core`._

The fifteen tools that `cairn mcp` serves to an agent. Eight write and seven read. The table below lists them, one line each. Each tool has its own section after the table, with its parameters, its result, its limits and an example call. [Connecting-Agents](Connecting-Agents) shows how to register `cairn mcp` with a client.

<!-- The table between the markers is read by internal/mcp/docs_drift_test.go. Keep the markers, and keep the first column as one tool name in backticks. -->
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

## How a client reaches the tools

- **Transport.** `cairn mcp` runs as a child process of the MCP client and speaks MCP over its standard input and output. It opens no socket and no database. It takes no arguments: `cairn mcp --help` is refused with exit 2.
- **The server.** Each tool call is one HTTP `POST` from `cairn mcp` to the server, at `/v1/agent/<tool>`, with the JSON arguments as the body. A client never calls that path itself. The server URL comes from `CAIRN_SERVER`, else the `server` line of `.cairn.toml`. The server must be an `http://` or `https://` address with a host, and no query or fragment. The client does not follow redirects.
- **The key.** Every call sends `Authorization: Bearer <key>`. The key comes from `CAIRN_API_KEY`, else from `~/.config/cairn/client.env`. The file must not be readable by other users.
- **Start-up refusals.** `cairn mcp` will not start with no server (`invalid: no server: ...`) or with no key (`invalid: no API key: ...`). Both exit 1. With no project it starts, and every call is refused with the reason.
- **The project.** The project is read once, when the session starts, from `CAIRN_PROJECT` or the nearest `.cairn.toml`. A fix made later is not seen until the session restarts. The refusal says so.
- **The caller's name.** A write tool needs its `agent` argument, which is the caller's name, such as `main` or `hicks`. A blank `agent` on a write is refused as `invalid`, and the message asks for the name. A read tool takes `agent` only where it is listed below. Over MCP the adapter sends no caller name on a read. So `recall` and `task_current` list no agent-scoped directives unless their `agent` argument is given. Other reads, such as `directive_list`, list every role when `agent` is left out.

## Who may call which tool

The key decides the role. The client cannot choose it.

| Key scope | Tools it can call | What it cannot do |
|---|---|---|
| `agent` | All 15 | Operator decisions, and the dashboard and browse reads, which are web UI routes and not MCP tools |
| `operator` | All 15 | The same as `agent`, plus the operator decisions, which are web UI routes and not MCP tools |
| `viewer` | The 7 reads: `task_current`, `task_get`, `task_list`, `learning_list`, `directive_list`, `recall`, `search` | Every write tool, which is refused with `read_only` |

The five operator decisions, `learning_decide`, `directive_ratify`, `directive_retire`, `directive_reaffirm` and `project_settings_set`, are not MCP tools. Learnings are accepted or rejected, and directives ratified, in the web UI. Agents can only propose.

## Results and errors

- **A success** is one text block of compact JSON. Each section below says what it holds.
- **A failure** is a tool error, not a protocol error, so the model reads it. The text is `{"error":{"kind":"...","message":"...","missing":[...]}}`. `missing` appears only when a close is refused. The kinds you will see are `invalid`, `not_found`, `refused`, `no_route` and `internal`.
- **Authentication and viewer refusals** reach the agent as kind `internal`, as they reach the CLI with exit 1. [Troubleshooting](Troubleshooting) gives the fix for each. Issue #195 asks for typed errors.
- **Unknown fields are refused.** An argument the tool does not name is `invalid`, and so is a second JSON value after the arguments.
- **A panic** inside a tool is answered as `internal`, with its cause in the message. The session continues.

## Limits

| Limit | Value | What happens over it |
|---|---|---|
| Result size, per success | 64 KiB (65,536 bytes) | A tool error of kind `invalid`, which says the result is too large and names the arguments that narrow it |
| Page size, default and maximum | 20 by default and 100 at most, for `task_get` events, `task_list`, `learning_list` | A larger `limit` is `invalid` |
| Search page | 10 by default, 100 at most | As above |
| Recall, per list | 5 by default, 100 at most | As above |
| Directives per call | 12 at most, ranked | The rest are counted in `omitted` |
| Recent events in `task_current` | 5 | Older events are reached with `task_get` |
| Request body, at the server | 1 MiB | Refused |
| Call timeout, in the client | 30 seconds | The call fails |
| Project name | At most 64 characters, lowercase letters and digits in hyphen-separated groups | Refused by the server |

A `task_get` brief larger than 64 KiB cannot be narrowed: the brief is one value and has no page. Issue #191 is open on this. The text of the refusal says the brief must be shortened.

## The tools

Each section gives the parameters, the result, the limits and who may call the tool. The example is the `arguments` object an MCP client sends. Parameters marked required must be present. The `agent` parameter is required on every write tool, and optional on the three reads that name it.

### `task_current`

The active task for this project, with its steps, its recent events, and the directives that apply to it.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | no | Your role, such as `main` or `reviewer`. It names the agent-scoped directives. Left out, none are listed. |

**Returns:** `task`, which is `null` when none is active. Otherwise the task, with `steps`, and `recent_events` (newest first, at most 5). Also `directives` (ranked agent, then project, then global; at most 12), `expired` (past `review_by`, left out), `omitted` (beyond the 12) and `other_active` when the project allows parallel tasks.

**Who:** any key.

**Example:**

```json
{"name": "task_current", "arguments": {"agent": "main"}}
```

### `task_start`

Start a task with its brief and an optional plan. The task is active unless `queue` is true.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name, such as `main` |
| `title` | string | yes | The short name of the task |
| `goal` | string | yes | What done looks like, in a sentence or two |
| `context` | string | no | Why now, background and links |
| `constraints` | string | no | What must not change |
| `priority` | string | no | `low`, `normal`, `high` or `urgent`. `normal` by default |
| `criteria` | array of string | no | Acceptance criteria, in order |
| `key_files` | array of `{path, note}` | no | Files the brief names |
| `steps` | array of `{title, agent}` | no | The plan. Each step has a title and an optional agent |
| `queue` | boolean | no | `true` creates the task open, not active |

**Returns:** `id` and `status`, and `steps` when a plan was given. The `id` has the form `task-YYYYMMDD-NN`.

**Refused** while another task is active, unless the project allows parallel tasks. The brief is frozen once any step starts: `task_update` can only change it before then.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "task_start", "arguments": {
  "agent": "main",
  "title": "Add the export flag",
  "goal": "The export command takes a format flag, and its default output does not change.",
  "criteria": ["The flag is documented", "The default output is unchanged"],
  "steps": [{"title": "Add the flag", "agent": "hicks"}, {"title": "Review the change", "agent": "apone"}]
}}
```

### `task_get`

One task in full, or one section of it.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `id` | string | yes | The task id, such as `task-20261106-01` |
| `section` | string | no | `brief`, `steps`, `events` or `close`. Left out, all of them |
| `limit` | integer | no | Events only. The page size: 20 by default, 100 at most |
| `cursor` | string | no | Events only. The `next` value of the last page |
| `step` | `{"number": n}` or `{"id": n}` | no | Events only. The events of one step |

**Returns:** `task`, always. The other fields depend on the section: `criteria` and `key_files` for the brief, `steps`, `events` with `next` for the journal, and `lessons`, `no_lessons_reason`, `mistakes` and the outcome for the close.

**Limits:** the 64 KiB result cap. The events are paged. The brief is not paged (see the note under Limits).

**Who:** any key.

**Example:**

```json
{"name": "task_get", "arguments": {"id": "task-20261106-01", "section": "events", "limit": 20}}
```

### `task_list`

Recent tasks in this project, most recently updated first.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `status` | array of string | no | Any of `open`, `active`, `blocked`, `done`, `failed`, `abandoned`. Left out lists all |
| `limit` | integer | no | 20 by default, 100 at most |
| `cursor` | string | no | The `next` value of the last page |

**Returns:** `tasks`, each with `id`, `project`, `title`, `status`, `priority` and `updated_at`, and `next` when there is another page.

**Who:** any key.

**Example:**

```json
{"name": "task_list", "arguments": {"status": ["active", "blocked"], "limit": 20}}
```

### `task_update`

Change a task's status, priority, owner, next action or blockers, or edit its brief before any step starts. Only the fields you give change.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name |
| `id` | string | yes | The task id |
| `status` | string | no | `active`, `blocked` or `abandoned`. `open` is refused, and a task closes through `task_close` |
| `reason` | string | no | Required for `blocked` and `abandoned`. It is written to the journal |
| `priority` | string | no | `low`, `normal`, `high` or `urgent` |
| `owner` | string | no | The agent leading the task |
| `next_action` | string | no | The next thing to do |
| `blockers` | string | no | What is blocking the task |
| `title`, `goal`, `context`, `constraints` | string | no | The brief. Only before any step starts |
| `criteria` | array of string | no | The brief. Replaces the whole list |
| `key_files` | array of `{path, note}` | no | The brief. Replaces the whole list |

**Returns:** the task, after the change.

**Refused** with `refused` when the change is not allowed. A status the task already has is refused too.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "task_update", "arguments": {
  "agent": "main",
  "id": "task-20261106-01",
  "status": "blocked",
  "reason": "Waiting for the operator to decide the close policy",
  "blockers": "Operator decision"
}}
```

### `task_close`

Close a task with its outcome, its summary, the result of each criterion, its lessons, its mistakes and its deliverables.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name |
| `id` | string | yes | The task id |
| `outcome` | string | yes in practice | `done` or `failed` |
| `summary` | string | per policy | What was done, in a few sentences |
| `criteria` | array of `{ord, result, evidence}` | per policy | One result per criterion. `result` is `met`, `partial`, `unmet` or `unclear` |
| `lessons` | array of `{kind, text, why, correction}` | per policy | `kind` is `wrong-assumption`, `reuse` or `similar-task` |
| `no_lessons_reason` | string | with an empty `lessons` | Why there are no lessons |
| `mistakes` | array of `{agent, mistake, impact, correction, prevention}` | per policy | `impact` is `low`, `med` or `high`. Send `[]` for none |
| `deliverables` | array of `{path, note}` | no | Files the task produced |

**Close policy.** Each project sets what a close must carry, so the list above says which fields a policy can require. A refused close lists what is missing:

```json
{"error":{"kind":"refused","message":"close policy not met: summary, lessons, mistakes","missing":["summary","lessons","mistakes"]}}
```

The keys the refusal can list are `outcome`, `summary`, `criteria`, `lessons`, `mistakes` and `active_steps`. A `lessons` of `[]` is treated as none, so it is refused unless `no_lessons_reason` is given. A closed task takes no further change.

**Returns:** the closed task.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "task_close", "arguments": {
  "agent": "main",
  "id": "task-20261106-01",
  "outcome": "done",
  "summary": "The flag is in, and the default output is unchanged.",
  "criteria": [{"ord": 1, "result": "met", "evidence": "make test passed"}],
  "lessons": [],
  "no_lessons_reason": "Nothing new came out of this task.",
  "mistakes": []
}}
```

### `step_plan`

Replace the task's pending steps with this list. Started steps are kept.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name |
| `task_id` | string | yes | The task id |
| `steps` | array of `{title, agent}` | yes | The new pending steps. `agent` is optional |

**Refused** when a pending step to be removed already has an event. Skip that step first.

**Returns:** `steps`, the list of the task's steps.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "step_plan", "arguments": {
  "agent": "main",
  "task_id": "task-20261106-01",
  "steps": [{"title": "Add the flag", "agent": "hicks"}]
}}
```

### `step_add`

Insert one step after a given step, or at the end of the plan.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name. This is the caller, not the step's owner |
| `task_id` | string | yes | The task id |
| `after` | `{"number": n}` or `{"id": n}` | no | Insert after this step. `0` puts the new step first. Left out, it goes last |
| `title` | string | yes | What the step does |
| `assignee` | string | no | The agent the step is for. This is the step's owner, which is why it is not called `agent` |

**Returns:** the new step.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "step_add", "arguments": {
  "agent": "main",
  "task_id": "task-20261106-01",
  "after": {"number": 1},
  "title": "Review the change",
  "assignee": "apone"
}}
```

### `step_update`

Start, finish, fail or skip a step.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name. This is the caller |
| `task_id` | string | yes | The task id |
| `step` | `{"number": n}` or `{"id": n}` | yes | The step |
| `action` | string | yes | `start`, `finish`, `fail` or `skip` |
| `summary` | string | for `finish`, `fail` and `skip` | The result |
| `assignee` | string | no | Reassigns the step to this agent |

**Returns:** the step, after the change.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "step_update", "arguments": {
  "agent": "hicks",
  "task_id": "task-20261106-01",
  "step": {"number": 1},
  "action": "finish",
  "summary": "The flag is added and make test passes"
}}
```

### `event_log`

Append an event to a task's journal.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name |
| `task_id` | string | yes | The task id |
| `step` | `{"number": n}` or `{"id": n}` | no | The step the event is about. Left out, it is about the task |
| `kind` | string | yes | `note`, `blocked`, `unblocked`, `handoff`, `decision`, `review` or `custom` |
| `text` | string | yes | What happened |
| `data` | JSON object | for `review` | A review needs `verdict` (`approved` or `not-approved`), `critical` and `warnings` |

Some kinds are not agent events. The server writes `started`, `finished` and `close` itself, and refuses them here.

**Returns:** the event.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "event_log", "arguments": {
  "agent": "apone",
  "task_id": "task-20261106-01",
  "step": {"number": 2},
  "kind": "review",
  "text": "Review of step 1",
  "data": {"verdict": "approved", "critical": 0, "warnings": 2}
}}
```

### `learning_add`

Propose a finding, a pattern or an agent note. It is stored as `proposed`. The operator accepts or rejects it in the web UI.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | yes | Your name |
| `task_id` | string | no | The task the learning came from |
| `kind` | string | yes | `finding`, `pattern` or `agent-note` |
| `title` | string | yes | A short title |
| `body` | string | yes | The suggestion, the solution or the note |
| `target` | string | no | What it would change: a file, a rule or an agent |
| `rationale` | string | no | Why it is proposed |
| `example` | string | no | An example |

**Returns:** the learning, with `status` `proposed`.

**Who:** `agent` or `operator`.

**Example:**

```json
{"name": "learning_add", "arguments": {
  "agent": "apone",
  "task_id": "task-20261106-01",
  "kind": "finding",
  "title": "The export help does not name the default format",
  "body": "The flag's help text should say which format is used when the flag is left out.",
  "target": "cmd/cairn/export.go"
}}
```

### `learning_list`

Learnings in this project, newest first. Set `all_projects` to see every project.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `status` | array of string | no | Any of `proposed`, `accepted`, `rejected`, `applied`, `retired`, `superseded`. Left out lists all |
| `kind` | array of string | no | Any of `finding`, `pattern`, `agent-note`. Left out lists all |
| `all_projects` | boolean | no | `true` lists every project's learnings |
| `target` | string | no | Only the learnings with this target |
| `limit` | integer | no | 20 by default, 100 at most |
| `cursor` | string | no | The `next` value of the last page |

**Returns:** `learnings` and `next` when there is another page.

**Who:** any key.

**Example:**

```json
{"name": "learning_list", "arguments": {"status": ["proposed"], "kind": ["finding"], "limit": 20}}
```

### `directive_list`

The directives that apply to this project, ranked agent, then project, then global, at most 12.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `agent` | string | no | A role name, such as `reviewer`. It narrows the agent-scoped directives to that role. Left out, every role's agent-scoped directives are listed |

**Returns:** `directives`, `expired` (past `review_by`, left out) and `omitted` (beyond the 12).

**Who:** any key.

**Example:**

```json
{"name": "directive_list", "arguments": {"agent": "reviewer"}}
```

### `recall`

For a goal, what should be known before the work starts: the directives that apply, the lessons and mistakes from past tasks, and similar tasks.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `goal` | string | yes | What the task is for, in a sentence |
| `agent` | string | no | Your role. It names the agent-scoped directives. **Pass it over MCP:** the adapter does not send the caller's name on a read, so without `agent` no agent-scoped directives are listed. The tool's schema text says the caller's own name is used. That is not what the adapter does on a read; see Notes on the code at the foot of this page |
| `limit` | integer | no | Items per list. 5 by default, 100 at most |

**Returns:** `directives` (this project and your role, ranked, at most 12), `expired` and `omitted`; `lessons` and `mistakes` from every project, best match first; and `tasks`, similar tasks from every project.

**Who:** any key.

**Example:**

```json
{"name": "recall", "arguments": {"goal": "add a format flag to the export command", "agent": "hicks"}}
```

### `search`

Full-text search over tasks, events, learnings, directives, lessons and mistakes. Plain words only: quotes and operators are matched as words.

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `query` | string | yes | Plain words |
| `kinds` | array of string | no | Any of `task`, `event`, `learning`, `directive`, `note`, `lesson`, `mistake`. Left out searches all |
| `all_projects` | boolean | no | `true` searches every project |
| `limit` | integer | no | Hits per page: 10 by default, 100 at most |
| `cursor` | string | no | The `next` value of the last page |

**Returns:** `hits`, best match first. Each hit has `kind`, `id`, `project`, `task_id` where it applies, `title` and `snippet`. `next` when there is another page.

**Who:** any key.

**Example:**

```json
{"name": "search", "arguments": {"query": "export flag", "kinds": ["task", "learning"], "limit": 10}}
```

## Notes on the code

- The table at the top is the list the drift test checks. If a tool is added, renamed or removed, the test fails until this page and the table agree.
- **The `recall` schema text differs from the behaviour.** The schema text for `recall` in `internal/mcp/tools.go` says that without `agent` the caller's own name is used. The adapter sets the caller's name only on write tools (`internal/mcp/mcp.go`, `addTool`). A read reaches the server with no name, so `recall` lists no agent-scoped directives unless `agent` is given. This page describes the behaviour. Pass `agent` on every `recall` and `task_current` call that needs agent-scoped directives.
