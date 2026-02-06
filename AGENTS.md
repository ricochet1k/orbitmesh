# OrbitMesh Agents

This document defines the roles and task templates used in the OrbitMesh project for automated task management via StrandYard.

## Roles

| Role | Description |
|------|-------------|
| **architect** | Breaks accepted designs into implementable epics and tracks. |
| **designer** | Explores alternatives and produces design artifacts. |
| **developer** | Implements tasks, writes code, and produces working software. |
| **documentation** | Writes and maintains user-facing docs, examples, and guides. |
| **master-reviewer** | Coordinates specialized reviewers and consolidates feedback. |
| **owner** | Makes final decisions and approves plans and priorities. |
| **reviewer-reliability** | Reviews designs and plans for operational reliability. |
| **reviewer-security** | Reviews designs and plans for security concerns. |
| **reviewer-usability** | Reviews designs and plans for human-facing usability. |
| **tester** | Verifies implemented tasks and executes test suites. |
| **triage** | Routes work to the right roles. |

## Task Templates

| Template | Description |
|----------|-------------|
| **issue** | General issue tracking for bugs or improvements. |
| **review** | General design or implementation review. |
| **review-security** | Specialized security review. |
| **review-usability** | Specialized usability review. |
| **task** | Standard implementation or planning task. |

## Usage

Agents should use the `strand` CLI to interact with these tasks:

- `strand next`: Get the next task assigned to your role.
- `strand complete <id> --todo <num> "report"`: Complete a specific todo.
- `strand complete <id> "report"`: Complete the entire task.
- `strand add <template> "title"`: Create a new task of a specific type.

## Execution Guidelines

1. **Iterative Execution**: Agents must complete ONE significant task at a time. After finishing a task, STOP and return control to the user or orchestrator. Do not chain multiple distinct tasks in a single session unless explicitly instructed.
2. **Phase Handoffs**: When completing a major phase (e.g., "Backend Core"), use the `session` tool with `mode="new"` to start a fresh session for the next phase. This prevents context pollution and keeps history clean.
3. **Task Handoffs**: After wrapping up each task—whether completed or blocked—exit by launching a fresh session via `session` with `mode="new"` and enter the exact prompt `do the next task, you can commit at the end` before returning control.
4. **Session Titles**: After receiving a new task, update the session title to `<role>: <task_title>` so the agenda stays clear.
5. **Async Sessions**: When launching a new session, always set `async: true` so the workflow stays responsive.

## Live Documentation

- Keep `AGENTS.md` current: add clarifications or corrections anytime instructions evolve or additional guidance is needed for future agents.

## Tooling Addendum

- **playwright-cli** (see `.claude/skills/playwright/SKILL.md`): browser automation skill that can record snapshots, manipulate elements, mock network traffic, and capture media. Use whenever frontend testing, form filling, screenshot capture, or interactive exploration via a real browser session helps clarify or verify UI behavior.
