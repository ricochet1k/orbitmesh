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

Agents should use the `strand` CLI to interact with tasks. Choose the command based on how the task is specified:

- `strand next`: Get the next task assigned to your role (only when the user asks for ???the next task???).
- `strand next --claim`: Get and immediately claim the next task assigned to your role (only when explicitly asked to claim the next task).
- When the user provides a specific task ID, do not call `strand next`. Use `strand show <id>` to load the task, then `strand claim <id>` before starting work unless the user explicitly asks you not to claim it.
- `strand complete <id> --todo <num> "report"`: Complete a specific todo.
- `strand complete <id> "report"`: Complete the entire task.
- `strand add <template> "title"`: Create a new task of a specific type.

### Strand CLI guidance (from `strand agents`)

- Use `strand next --claim` to select and claim work; respect role opt-in or ignore behavior.
- When asked to work on the next thing, run `strand next --claim`, follow the returned instructions, and report a brief task summary.
- Treat the role description returned by `strand next --claim` as canonical for how to execute the task.
- When a task is done (including planning-only), run `strand complete <task-id> "report of what was done"`.
- If blocked, record blockers with `strand block`.
- Use `strand add` for new tasks or issues; avoid ad-hoc task creation outside strand.
- Get the list of roles and task templates from `strand roles` and `strand templates`, add them to `AGENTS.md` and keep that part up to date as needed.
- If bugs, usability issues, or missing features are found in `strand`, file an issue in the `strand` project via `strand add issue --project strand "Issue title"` with a detailed markdown body.

## Execution Guidelines

1. **Iterative Execution**: Agents must complete ONE significant task at a time. After finishing a task, STOP and return control to the user or orchestrator. Do not chain multiple distinct tasks in a single session unless explicitly instructed.
2. **Task Handoffs**: When you are asked to handoff to a new session, after wrapping up each task???whether completed or blocked???exit by launching a fresh session via `session` with `mode="new"` and enter the exact prompt `do the next task, you can commit at the end and then start a new session as in Task Handoffs` before returning control.
3. **Session Titles**: After receiving a new task, update the session title to `<role>: <task_title>` so the agenda stays clear.
4. **Tests Required**: When code changes are made, run relevant tests before finishing the task and record whether they passed in your report.

## Live Documentation

- Keep `AGENTS.md` current: add clarifications or corrections anytime instructions evolve or additional guidance is needed for future agents.
- Keep `TESTING.md` aligned with the intended testing strategy and test layout as tooling and practices evolve.

## Tooling Addendum

- **playwright-cli** (see `.claude/skills/playwright/SKILL.md`): browser automation skill that can record snapshots, manipulate elements, mock network traffic, and capture media. Use whenever frontend testing, form filling, screenshot capture, or interactive exploration via a real browser session helps clarify or verify UI behavior.

## Codebase navigation

This project uses `roam` for codebase comprehension. Always prefer roam over Glob/Grep/Read exploration.

Before modifying any code:
1. First time in the repo: `roam understand` then `roam tour`
2. Find a symbol: `roam search <pattern>`
3. Before changing a symbol: `roam preflight <name>` (blast radius + tests + fitness)
4. Need files to read: `roam context <name>` (files + line ranges, prioritized)
5. Debugging a failure: `roam diagnose <name>` (root cause ranking)
6. After making changes: `roam diff` (blast radius of uncommitted changes)

Additional: `roam health` (0-100 score), `roam impact <name>` (what breaks),
`roam pr-risk` (PR risk), `roam file <path>` (file skeleton).

Run `roam --help` for all commands. Use `roam --json <cmd>` for structured output.
