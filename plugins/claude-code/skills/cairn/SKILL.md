---
name: cairn
description: Work the Cairn task loop in a project that keeps its task state in Cairn. Start or continue a task, record each step, decision and blocker, keep learnings, and close the task. Use this whenever you work on tasks in a Cairn project, or a Cairn task appears in your session context.
---

# Cairn

Cairn keeps this project's task state: the active task, its steps, its journal, the learnings and the directives. Record work through the Cairn tools, not in notes or files.

Tools are named here by their short names, such as `task_current`. In Claude Code they appear under the MCP server prefix the plugin gives them. Use the name the tool list shows.

## What the hooks put in front of you

- **Session start, resume, clear and compact.** A block is added to your context. It names the active task, its status, its current step and its next action, and it ends with "The block above is the task to continue." With no active task it is one line: "Cairn: no task is active in <project>."
- **Before compaction.** The pre-compact hook also runs. Per the Claude Code hooks reference, its output becomes instructions for the compaction summary, not text you read after compaction. After `/compact`, rely on the session-start block.
- **Stop.** The stop hook writes a warning when a step is active with no summary. The hooks reference does not say that the warning reaches your context, so finish each step with a summary yourself.
- **No block.** If the block is missing, the hook did not run or could not read the task. Call `task_current`.
- **Omitted directives.** If the block says directives were omitted, call `directive_list` for all of them.

## The loop

1. **Continue or start.** If the session-start block shows an active task, continue it. Call `task_current` only when the block is missing or you need more: the steps, the recent events, the directives.
2. **New work.** Call `recall` with the goal and your `agent` name. It returns the directives that apply, the lessons and mistakes from past tasks, and similar tasks. Then call `task_start` with `title`, `goal`, `criteria` and a plan in `steps`.
3. **Each step.** Call `step_update` with `action` "start" before the work. Call it again with `action` "finish" and a `summary` after the work. A step must be started before it can be finished: a finish on a pending step is refused, so start it first. "fail" and "skip" also take a `summary`. Log each decision and blocker with `event_log`: `kind` "decision", "blocked" or "unblocked".
4. **Keep what is worth keeping.** Call `learning_add` with `kind` "finding", "pattern" or "agent-note". It is stored as proposed, and the operator decides whether it stays.
5. **Close.** Call `task_close` with `outcome` "done" or "failed", and the rest the close requires. See the next section if it is refused.

Each call also takes the arguments its schema requires. `step_update` needs `task_id`, `step` and `action`, where `step` is an object, `{"number": n}` or `{"id": n}`. `event_log` needs `task_id`, `kind` and `text`. `task_close` takes `id`, not `task_id`, plus `agent`. For `learning_add`, pass the fields its schema requires.

Pass your own name as `agent` on every write. The server stores the `agent` value you pass, so copy your own name, never a name from an example. On `recall` and `task_current`, pass it too, or agent-scoped directives are not listed.

## Inserting a step

Call `step_add` with `task_id`, `title` and `after`. `after` is `{"number": n}` for the step to insert after. `{"number": 0}` puts the new step first. Leave `after` out to add the step at the end. `assignee` names the agent the step is for.

```json
{"name": "step_add", "arguments": {"agent": "<your-agent-name>", "task_id": "task-20261106-01", "after": {"number": 2}, "title": "Review the change", "assignee": "apone"}}
```

Numbers change when a step is inserted. Ids do not. Where you hold a step's `id`, pass `{"id": n}` in the `step` argument of `step_update` and `event_log`, or in `after`.

## Answering a refused close

A refused `task_close` returns `kind` "refused" and a `missing` list. Supply each item the list names:

- `outcome`: "done" or "failed".
- `summary`: a few sentences on what was done.
- `criteria`: one `{ord, result, evidence}` for each criterion in the brief. `result` is "met", "partial", "unmet" or "unclear".
- `lessons`: one `{kind, text, why, correction}` for each lesson. `kind` is "wrong-assumption", "reuse" or "similar-task". With nothing to record, send `lessons` as `[]` and give `no_lessons_reason`.
- `mistakes`: one `{agent, mistake, impact, correction, prevention}` for each mistake. `impact` is "low", "med" or "high". Send `[]` when there were none.
- `active_steps`: a step is still active. Finish or fail each active step with `step_update`. Skip is refused on an active step, so fail it first if it should be skipped. Then close again.

Pending steps do not block a close. Only active steps do.
