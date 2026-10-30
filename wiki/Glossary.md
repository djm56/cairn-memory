# Glossary

_Status as of 2026-10-30: M1 (Server-mode MCP) is current. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

The terms follow [DESIGN.md §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts) and the sections it links. Cairn says **task**, not mission (decision D1).

- **Agent.** Whoever made a call. It is `main` or a sub-agent's name, chosen by the client. [DESIGN §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts)
- **Agent note.** A learning about how an agent behaved or performed. [DESIGN §1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#1-what-cairn-is)
- **Client.** The program that connects to Cairn, such as Claude Code or OpenCode. It is recorded, but it is not an identity. [DESIGN §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts)
- **Close policy.** What a task must have before it can close. Each project sets it, and only the operator changes it. [DESIGN §3.7](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#37-the-close-policy)
- **Conformance suite.** The test suite that checks any `Service` against the contract. [DESIGN §5.1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#51-packages)
- **Directive.** A rule that agents must follow. The operator ratifies it and gives it a scope. [DESIGN §3.4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#34-learnings-and-directives)
- **Epic.** The issue that summarises one milestone: its work, its exit check and its child issues. [How we work](How-We-Work)
- **Event (journal).** One row in a task's journal: something that happened, who did it and when. Events are append-only. [DESIGN §3.3](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#33-events-and-reviews)
- **Finding.** A learning that proposes a change to a file, a rule or an agent. [DESIGN §1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#1-what-cairn-is)
- **Learning.** Something worth keeping. It has one of three kinds: finding, pattern or agent note. Agents propose learnings, and the operator decides on them. [DESIGN §3.4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#34-learnings-and-directives)
- **Milestone.** One stage of the build plan, with its work and an exit check. Plan v1 has M1 to M8 and Later. Phases 0 to 2 came before them. [PLAN §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-milestones)
- **Operator.** The person who decides. Only the operator accepts or rejects learnings and ratifies directives. [DESIGN §1.1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#11-principles)
- **Pattern.** A learning about something that worked. [DESIGN §1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#1-what-cairn-is)
- **Project.** A codebase that uses Cairn. Every task, event and learning belongs to one. [DESIGN §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts)
- **Recall.** Before a task starts, the directives, lessons and similar past tasks that apply to it. [DESIGN §4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#4-tool-surface-mcp)
- **Review.** A verdict on one step: approved or not, with counts of critical issues and warnings. [DESIGN §3.3](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#33-events-and-reviews)
- **Session.** One run of a client, when the client gives it an id. It is optional. [DESIGN §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts)
- **Step.** One planned piece of a task, with an agent, attempts and a status. [DESIGN §3.2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#32-steps)
- **Task.** A unit of work with a goal, a brief, a plan and an outcome. Bishop calls it a mission. [DESIGN §2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#2-concepts)
