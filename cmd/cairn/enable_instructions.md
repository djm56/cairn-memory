# Cairn

This project keeps its task state in Cairn, through the Cairn tools. Their names start with {prefix}.

Work the task loop:

1. At session start, call {prefix}task_current. If a task is active, continue it.
2. For new work, call {prefix}recall with the goal, then {prefix}task_start with a brief and a plan.
3. For each step, call {prefix}step_update to start it, do the work, then {prefix}step_update to finish it with a summary. Log decisions and blockers with {prefix}event_log.
4. When something is worth keeping, call {prefix}learning_add.
5. Close the task with {prefix}task_close. If it is refused, supply what it lists.
