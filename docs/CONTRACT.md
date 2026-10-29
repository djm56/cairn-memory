# Cairn Memory — Contract

**Status as of 2026-10-29.** Items 1–11 of §9 were approved by the operator on
2026-10-28 (phase 1 exit). Items 12 and later await the operator's approval.
`core.Local` passes all 51 cases in `internal/conformance` (count as of
2026-10-29), and none is deferred. The Go form is `internal/core` (`Service`,
request and result types, errors). The conformance suite checks the behaviour
described here.

This document covers every tool in [DESIGN.md](DESIGN.md) §4, plus the
operator calls behind the web UI and the CLI. Field names are the JSON names
an MCP client sees. The MCP adapter (phase 3) maps each tool's arguments to
one request type, and each result back, without adding rules.

§9 lists what this contract settles that DESIGN.md left open; the operator
approved it on 2026-10-28.

## 1. Conventions

### 1.1 Caller

Every call runs as a **caller**: project, agent, optional session and
client, and a role. The adapter sets it; request types do not carry it.

- **Project.** Required for every agent call, reads included. Absent →
  `invalid`, with a message that names `cairn init`. A name is lowercase
  letters and digits in hyphen-separated groups, at most 64 characters
  (`acme-api`). A project row is created on its first call.
- **Agent.** Required for every write (each tool marked *write* below). It is
  stored on the rows the call creates: `agent`, `reviewer`, `created_by`.
- **Role.** `agent` or `operator`. The MCP adapter always uses `agent`. The
  server derives the role from the API key's scope, never from the request.
  Operator calls (§8) refuse any other role.

### 1.2 Errors

A failure the caller can act on has a kind and a message:

| Kind | Meaning |
|---|---|
| `invalid` | The request is malformed: a required field is empty, a value is unknown, there is no project or agent. Fix the request. |
| `not_found` | The task, step, learning, directive or note does not exist. For an agent call, that means **in the caller's project**: another project's rows are not found, not forbidden. An operator call (§8) finds its row by id or code in any project. |
| `refused` | Well formed, but a rule forbids it now: a status transition, a second active task, the close policy. `task_close` adds `missing`. |
| `operator_only` | An operator call made without the operator role. |

Checks run in this order, so a request with several faults gets the first
one: caller → request shape (`invalid`) → existence (`not_found`) → rules
(`refused`). Any other error is an internal failure.

### 1.3 Time, ids, lists

- Every time is UTC, set by the server. No request carries a timestamp.
- Task ids are `task-YYYYMMDD-NN`. The sequence is daily and global across
  projects (D4), and allocation is atomic.
- Directive codes are `D-0001`, global and sequential.
- **Paged calls** take `limit` (0 means the call's default; the maximum is
  100, and more is `invalid`) and `cursor` (empty for the first page, then
  the previous result's `next`). An empty `next` means it is the last page.
  A cursor is opaque.
- In results, empty optional fields are left out.

### 1.4 Steps

A step is named by a **step ref**: `{"number": 3}` is the step shown as 3
now, and `{"id": 412}` is the step whose id is 412. Ids and numbers start at
1; give exactly one, or the call is `invalid`. So is `{}`, in every call that
takes a ref. Number 0 names
no step: it is valid only as `after` in `step_add`, where it means before the
first step (§3). Numbers are computed from the order each time and change
when steps are inserted. Ids never change.

### 1.5 Task status

```
task_start (queue)                  task_start
        │                                │
        ▼                                ▼
        open ──── task_update ─────────► active ◄──── task_update ────► blocked
        │                                │ task_close                   │ task_close
        │                                ▼                              ▼
        │                                done | failed                  done | failed
        └──────── task_update(abandoned) from open, active, blocked
```

- `task_update` may set `active`, `blocked` or `abandoned`. No move goes into
  `open` except `task_start` with `queue`. `done` and `failed` come only from
  `task_close`. Asking for the status a task already has is `refused`, and the
  call changes nothing.
- `blocked` and `abandoned` need a `reason`, which is written to the journal.
- `done`, `failed` and `abandoned` are terminal. After them, every write to
  the task is `refused`: update, steps, events, reviews and close.
- **One active task per project**, unless the project's settings allow
  parallel tasks. Making a second task active (`task_start` or `task_update`)
  is `refused`. `blocked` does not count as active.

## 2. Task tools

### `task_current` — read

The first call of a session.

**In:** nothing.
**Out:** `task` (null when no task is active), `criteria`, `steps`,
`recent_events` (newest first, at most 5), `directives` (those that apply
to the project, as `directive_list` with no agent), and `other_active`
(when parallel tasks are on and more than one is active, `task` is the one
started last).

```json
{"task": {"id": "task-20261027-03", "project": "acme-api", "title": "Add rate limit",
  "goal": "Requests over 100/min get 429.", "status": "active", "priority": "normal",
  "next_action": "write the test", "opened_at": "…", "updated_at": "…"},
 "criteria": [{"ord": 1, "criterion": "429 after 100 requests"}],
 "steps": [{"id": 41, "number": 1, "title": "Write test", "status": "active", "attempts": 1}],
 "recent_events": [{"id": 9, "kind": "started", "text": "Step 1 started", "agent": "main", "occurred_at": "…"}],
 "directives": [{"code": "D-0004", "scope": "global", "title": "Test first", "rule": "…"}]}
```

### `task_start` — write

**In:** `title`, `goal` (required); `context`, `constraints`; `priority`
(`low|normal|high|urgent`, default `normal`); `criteria` (list of strings,
numbered 1…n); `key_files` (`[{path, note}]`); `steps` (`[{title, agent}]`);
`queue` (true → the task is created `open` instead of `active`).
**Out:** `id`, `status`, `steps`.
**Errors:** `invalid` for a missing title or goal or an unknown priority;
`refused` when another task is active and parallel tasks are off.

```json
{"title": "Add rate limit", "goal": "Requests over 100/min get 429.",
 "criteria": ["429 after 100 requests", "Limit configurable"],
 "steps": [{"title": "Write test"}, {"title": "Implement"}]}
→ {"id": "task-20261027-03", "status": "active", "steps": [{"id": 41, "number": 1, …}, …]}
```

### `task_get` — read

**In:** `id`; `section`: `brief`, `steps`, `events` or `close`. Empty
returns all of them.
**Out:** `task`, always. `brief` adds `criteria` and `key_files`. `steps`
adds `steps` and `reviews`. `events` adds the last 20 `events`. `close`
adds `criteria` with results, `lessons`, `no_lessons_reason`, `mistakes` and
`deliverables`.
**Errors:** `not_found`.

### `task_list` — read, paged

**In:** `status` (a list; empty means all), `limit` (default 20), `cursor`.
**Out:** `tasks` (`[{id, project, title, status, priority, updated_at}]`,
most recently updated first), `next`. Only the caller's project.

### `task_update` — write

**In:** `id`; any of `status`, `reason`, `priority`, `owner`, `next_action`,
`blockers`; the brief fields `title`, `goal`, `context`, `constraints`,
`criteria`, `key_files`. Only the fields given change. A list given replaces
the whole list.
**Out:** the task.
**Errors:** `invalid` for `done` or `failed`, an unknown value, or `blocked`
or `abandoned` without a reason. `not_found`. `refused` for a terminal
task, a transition not in §1.5, a second active task, or a brief edit after
any step has started.

### `task_close` — write

**In:** `id`; `outcome` (`done|failed`); `summary`; `criteria`
(`[{ord, result: met|partial|unmet|unclear, evidence}]`); `lessons`
(`[{kind: wrong-assumption|reuse|similar-task, text, why, correction}]`);
`no_lessons_reason`; `mistakes` (`[{agent, mistake, impact: low|med|high,
correction, prevention}]`); `deliverables` (`[{path, note}]`).

**Absent is not empty.** `"mistakes": []` says "no mistakes". Leaving
`mistakes` out says nothing, and the policy refuses it. The same holds for
`lessons`, which may be `[]` only with a `no_lessons_reason`.

**Out:** the closed task (`status` is the outcome, and `closed_at` is set).
A `close` event is written.

**Errors:** `invalid` for an outcome other than `done`/`failed`, an unknown
enum value, or a result for a criterion number the task does not have.
`not_found`. `refused` when the task is terminal, when it is `open` (start it
first), or when the project's close policy is not met. The refusal lists every
unmet item in `missing`,
using these keys:

| Key | Unmet when (default policy: all on) |
|---|---|
| `outcome` | no outcome (always required) |
| `summary` | no summary |
| `criteria` | a criterion has no result |
| `lessons` | `lessons` absent, or empty with no `no_lessons_reason` |
| `mistakes` | `mistakes` absent |
| `active_steps` | a step is still `active` |

```json
{"kind": "refused", "message": "close policy not met: summary, lessons, mistakes",
 "missing": ["summary", "lessons", "mistakes"]}
```

A refused close changes nothing. Supply the missing items and call again.

## 3. Step tools

All three are writes, and are `refused` on a terminal task.

### `step_plan`

**In:** `task_id`, `steps` (`[{title, agent}]`).
**Out:** `steps`, the whole list.
Every `pending` step is removed. Steps in any other status are kept in
their order, and the new steps follow the last kept step.

A `pending` step that an event or a review refers to cannot be removed, so
the call is `refused` and changes nothing. The way out: skip that
step with `step_update` (`skip`, with a summary), then plan again. A skipped
step is kept.

### `step_add`

**In:** `task_id`, `title`, `agent`; `after`, a step ref. Left out, the step
goes at the end. `{"number": 0}` puts it before the first step.
**Out:** the step, with its number.
No other step's id changes. Display numbers after it move up by one.

### `step_update`

**In:** `task_id`, `step` (a ref), `action`, `summary`, `agent`
(reassigns the step).

| Action | From | To | Also |
|---|---|---|---|
| `start` | `pending`, `failed` | `active` | `attempts` +1; `started_at`; a `started` event. The task must be `active`. |
| `finish` | `active` | `done` | `summary` required; `ended_at`; a `finished` event |
| `fail` | `active` | `failed` | `summary` required; `ended_at`; a `note` event |
| `skip` | `pending`, `failed` | `skipped` | `summary` required (the reason); a `note` event |

**Errors:** `invalid` for a bad ref, an unknown action, or a missing
summary. `not_found`. `refused` for any other transition, or `start` when
the task is not `active`.

## 4. Journal

### `event_log` — write

**In:** `task_id`; `step` (a ref, optional); `kind`; `text` (required);
`data` (a JSON object, optional).
An agent may log `note`, `blocked`, `unblocked`, `handoff`, `decision` and
`custom`. `started`, `finished`, `review` and `close` are written by the
calls they record, and are `invalid` here.
**Out:** the event, with `occurred_at` set by the server.
There is no call that edits or deletes an event.

### `event_list` — read, paged

**In:** `task_id`; `step` (a ref, to filter); `limit` (default 20);
`cursor`.
**Out:** `events`, newest first, and `next`.

### `review_record` — write

**In:** `task_id`, `step` (a ref), `verdict` (`approved|not-approved`),
`critical`, `warnings` (counts, ≥ 0), `summary`.
**Out:** the review. `reviewer` is the calling agent. A `review` event is
written.

## 5. Learnings and directives

### `learning_add` — write

**In:** `kind` (`finding|pattern|agent-note`), `title`, `body` (required);
`task_id`, `target`, `rationale`, `example`.
**Out:** the learning, always with status `proposed`. No agent call changes
it.
**Errors:** `invalid`; `not_found` for an unknown `task_id`.

### `learning_list` — read, paged

**In:** `status`, `kind` (lists; empty means all); `all_projects` (by
default only the caller's project); `target`; `limit` (default 20); `cursor`.
**Out:** `learnings`, newest first, and `next`.

### `directive_list` — read

**In:** `agent` (a role name, optional).
**Out:** `directives`, ordered by code. The list holds active directives
only: every `global` one, the `project` ones for the caller's project, and
`agent` ones. With `agent` set, the `agent` ones are limited to that role.
With `agent` empty, all of them are included.

## 6. Recall and search

### `recall` — read

**In:** `goal` (required); `agent` (narrows directives, as in
`directive_list`); `limit` per list (default 5).
**Out:** `directives` (those that apply to the caller's project), plus
`lessons`, `mistakes` and `tasks` from **every project**, best match to the
goal first. Each lesson and mistake carries its `task_id` and `project`.

### `search` — read, paged

**In:** `query` (required; plain text, never query syntax: quotes,
operators and brackets are matched as words, and never cause an error);
`kinds` (`task|event|learning|directive|note|lesson|mistake`; empty means
all); `all_projects`; `limit` (default 10); `cursor`.
**Out:** `hits` (`[{kind, id, project, task_id, title, snippet}]`, best
match first), and `next`. Global directives are found from every project.
A retired directive is not a hit: it is not listed by
`directive_list`, `recall` or `task_current`, and search does not list it
either.

## 7. Notes

### `note_put` — write

**In:** `title`, `body` (Markdown); `task_id` (only when creating); `id`
(to replace an existing note's title and body).
**Out:** the note.

### `note_get` — read

**In:** `id`. **Out:** the note. **Errors:** `not_found`.

## 8. Operator calls

These are not MCP tools, and never will be. The web UI (phase 6) and the
CLI reach them. Each one refuses a caller without the operator role
(`operator_only`) before any other check. Operators act across projects, so
these calls do not need the caller's project. Every call needs the operator's
name, which the caller carries as its agent; a call without one is `invalid`.
`LearningDecide` and `DirectiveRatify` record the name (`decided_by`,
`ratified_by`). `DirectiveRetire` and `ProjectSettingsSet` store no name, and
refuse a nameless call all the same.

| Call | In | Does |
|---|---|---|
| `LearningDecide` | `id`, `status` (any but `proposed`), `superseded_by` (required for `superseded`), `note` | Sets the status, `decided_by`, `decided_at` and `decision_note`. A `superseded_by` that leads back to this learning, directly or through a chain of superseded learnings, is `refused`, since it would close a cycle. |
| `DirectiveRatify` | `scope`, `project` (scope project), `agent` (scope agent), `title`, `rule` (required), `applies_when`, `rationale`, `reviewer_check`, `example`, `from_learning` | Creates an active directive with the next code. With `from_learning`, that learning must be `accepted` (otherwise `refused`), and it becomes `applied`. |
| `DirectiveRetire` | `code` | Sets `retired`, and `retired_at`. A directive retires once: a second call for the same code is `refused`, and the first time stands. Retired directives are no longer listed. |
| `ProjectSettingsSet` | `project`, `display_name`, `settings` (`parallel_tasks`, `close_policy`) | Replaces the project's settings, all of them: `settings` is required, and an omitted one is `invalid`. The close policy has one switch per row of the table in §2 (`task_close`), except `outcome`, which is always required. |

## 9. Settled here, for review

Each item answers a question that DESIGN.md left open. Each one is checked
by the suite, except where noted. The operator approved items 1–11 on
2026-10-28.

1. **Operator calls are part of `Service`** (§8), refused unless the role is
   operator. Without them, the suite could not test the rule "agents cannot
   decide". The MCP adapter does not expose them.
2. **The project is needed for reads too.** DESIGN §2.1 says writes. With
   reads included, there is one rule and no hidden default.
3. **`task_start` makes the task active by default.** `queue: true` creates
   it `open`. A second active task is refused, not queued silently.
4. **`blocked` and `abandoned` need a reason.** `abandoned` is set through
   `task_update`, and `task_close` covers only `done` and `failed`.
5. **The brief is frozen once any step starts.** DESIGN says "before work
   starts". This is the precise form.
6. **Agents cannot log `started`, `finished`, `review` or `close` events.**
   The calls they record write them, so the journal cannot claim work that
   the step table does not show.
7. **Step refs are an object.** `{"number": n}` or `{"id": n}`, never a
   bare integer, which could mean either.
8. **`fail` and `skip` need a summary.** A failed attempt with no reason
   teaches nothing.
9. **`missing` uses fixed keys** (§2, `task_close`), so a client can act on
   them without parsing the message.
10. **Rows in another project are `not_found`.** `learning_list`, `search`
    and `recall` read across projects only on request (`all_projects`), or
    by design (`recall`).
11. **Agent-scoped directives apply in every project.** DESIGN §3.4 has no
    project on an agent-scoped directive.
12. **Deferred:** the "step-review rule" in project settings is not defined
    here. The operator deferred it to phase 8 (#41), and DESIGN §3.6 no
    longer lists it.
13. **`task_update` never opens a task (ruling 1a, 2026-10-29).** No
    `task_update` moves a task into `open`. Only `task_start` with `queue`
    creates one (§1.5). *Bishop default, pending operator approval.*
14. **A step ref is one positive value (ruling 2a, 2026-10-29).** A ref names
    a step by an id or a number, never both. `{}` is `invalid` in every call
    that takes a ref. `{"number": 0}` is valid only as `step_add`'s `after`,
    where it means before the first step (§1.4, §3). *Bishop default, pending
    operator approval.*
15. **A recorded pending step blocks `step_plan` (ruling 3a, 2026-10-29).** A
    pending step with an event or a review is not removed. The call is
    `refused` and changes nothing. The refusal names the first such step in
    display order, and the way out is to skip it and plan again (§3). *Bishop
    default, pending operator approval.*
16. **`search` hides retired directives (ruling 4a, 2026-10-29).** A retired
    directive is not a `search` hit, as it is not listed by `directive_list`,
    `recall` or `task_current` (§6). *Bishop default, pending operator
    approval.*
17. **Same-status `task_update` is refused (default 1, 2026-10-29).** Asking
    for the status a task already has is `refused`, and the call changes
    nothing, so a reason sent with it is not dropped without a word (§1.5).
    *Bishop default, pending operator approval.*
18. **Recall's word rule (default 2, 2026-10-29).** `recall` keeps the words of
    a goal that can name its subject, and drops the stop words. In a goal that
    mixes cases, it keeps upper-case short words (`JWT`, `SQL`, `TLS`, `CLI`)
    and words with a digit (`v2`). In a goal written wholly in capitals, it
    keeps only words of four or more letters and words with a digit, so
    `ADD RATE LIMIT TO API` matches on `RATE` and `LIMIT`. *Bishop default,
    pending operator approval.*
19. **Ids and numbers start at 1 (default 3, 2026-10-29).** `{"id": 0, "number":
    1}` is `invalid`, and so is any id or number below 1. Give exactly one (§1.4).
    *Bishop default, pending operator approval.*
20. **Every operator call needs a name (default 4, 2026-10-29).** A call with
    no operator name is `invalid`. `LearningDecide` and `DirectiveRatify`
    record the name (`decided_by`, `ratified_by`). `DirectiveRetire` and
    `ProjectSettingsSet` store none, and refuse a nameless call all the same
    (§8). *Bishop default, pending operator approval.*
21. **A directive retires once (default 5, 2026-10-29).** A second
    `directive_retire` of the same code is `refused`, and the first retirement
    time stands (§8). *Bishop default, pending operator approval.*
22. **Learning moves and supersede cycles (default 6, 2026-10-29).**
    `learning_decide` may set any status except `proposed`, from any current
    status. A `superseded_by` that leads back to the learning being decided,
    directly or through a chain of superseded learnings, is `refused`, since
    the links would form a cycle (§8). *Bishop default, pending operator
    approval.*
23. **Settings are required (default 7, 2026-10-29).** `project_settings_set`
    with no `settings` is `invalid`. The call replaces the whole object, and an
    omitted one would otherwise read as every switch off (§8). *Bishop default,
    pending operator approval.*
24. **Close from `open` (reading 14e-1, 2026-10-29).** `task_close` of an open
    task is `refused`: the task must be started first (§1.5, §2). *Bishop
    default, pending operator approval.*
25. **Operator lookups match by id (reading 14e-2, 2026-10-29).** An operator
    call finds its learning or directive by id or code in any project. Only
    agent calls are limited to the caller's project (§1.2, §8). *Bishop
    default, pending operator approval.*
26. **Lessons with a no-lessons reason (reading 14e-3, 2026-10-29).** A close
    that has lessons and a `no_lessons_reason` stores the reason as given. It
    is neither refused nor dropped (§2). *Bishop default, pending operator
    approval.*
27. **Close-policy switches (reading 14e-4, 2026-10-29).** The close policy has
    one switch per row of the §2 table, except `outcome`, which is always
    required (§8). *Bishop default, pending operator approval.*
28. **Abandon and block journal kinds (2026-10-29).** `task_update` to
    `abandoned` writes a `decision` event, and to `blocked` a `blocked` event,
    each with the reason given (§1.5). *Bishop default, pending operator
    approval.*
29. **Skip sets `ended_at` (2026-10-29).** A `skip` step update sets the step's
    `ended_at` (§3). *Bishop default, pending operator approval.*
30. **Learnings and notes on a terminal task (2026-10-29).** `learning_add` and
    `note_put` are allowed on a `done`, `failed` or `abandoned` task. §1.5
    refuses the task's own writes (update, steps, events, reviews and close),
    and these are not among them. *Bishop default, pending operator approval.*
31. **`note_put` shape (2026-10-29).** Giving `id` with `task_id` is `invalid`,
    and so is a blank title or body (§7). *Bishop default, pending operator
    approval.*
32. **`review_record` on any step (2026-10-29).** `review_record` accepts a step
    in any status. The contract names none (§4). *Bishop default, pending
    operator approval.*
33. **Event on a pending step (2026-10-29).** `event_log` may name a pending
    step (§4). *Bishop default, pending operator approval.*
