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
