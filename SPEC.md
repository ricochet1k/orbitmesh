# OrbitMesh Project Specification & Vision

OrbitMesh is an agentic platform designed for automated task management and execution. This document serves as the top-level vision and spec for the project. We operate on a **Spec-Driven Development** model: no feature is built without a corresponding feature spec linked from this document.

For details on the development process, please refer to [feature-spec-driven-development.md](feature-spec-driven-development.md).

## Project Vision
OrbitMesh aims to provide a robust, reliable, and observable environment for executing autonomous agents and managing complex developer workflows. It integrates deeply with LLM providers (like Claude and Codex), manages execution lifecycle (sessions, terminals, tasks), and provides a rich UI for exploring agent behavior, code flow, and task hierarchies.

## Architectural Overview
OrbitMesh consists of a Go-based backend providing the execution engine, database integration, and provider abstractions, along with a SolidJS-based frontend providing the user interfaces, visualization canvases (like CodeFlow), and management dashboards.

## Features To Be Written (TBW)
The following is a comprehensive list of features that have been identified, either implemented without a formal spec, or planned for the future. Every feature listed here must have its own detailed spec file in the `specs/features/` or `specs/future/` directories before its development can be finalized.

### Core Engine & Infrastructure
- [x] **Backend Agent Execution Engine**: [specs/features/core/backend-agent-execution-engine.md](specs/features/core/backend-agent-execution-engine.md)
- [ ] **Task Scheduler**: [specs/features/core/task-scheduler.md](specs/features/core/task-scheduler.md)
- [ ] **Backend Architecture**: [specs/features/core/backend-architecture.md](specs/features/core/backend-architecture.md)
- [ ] **Global Session State Stream**: [specs/features/core/global-session-state-stream.md](specs/features/core/global-session-state-stream.md)
- [ ] **Resumable Session Recovery**: [specs/features/core/resumable-session-recovery.md](specs/features/core/resumable-session-recovery.md)
- [ ] **Session Lifecycle Design**: [specs/features/core/session-lifecycle.md](specs/features/core/session-lifecycle.md)
- [x] **Generic Session Events**: [specs/features/core/generic-session-events.md](specs/features/core/generic-session-events.md)
- [ ] **Entity Storage Architecture**: [specs/features/core/entity-storage-architecture.md](specs/features/core/entity-storage-architecture.md)
- [ ] **Session Event Types**: [specs/features/core/session-event-types.md](specs/features/core/session-event-types.md)
- [ ] **Realtime Websocket Feed**: [specs/features/core/realtime-websocket.md](specs/features/core/realtime-websocket.md)
- [x] **TermEmu PTY Websocket Activity Feed**: [specs/features/core/termemu-pty-websocket-activity-feed.md](specs/features/core/termemu-pty-websocket-activity-feed.md)
- [ ] **Terminal Connection Writer Lock UX**: [specs/features/core/terminal-connection-writer-lock.md](specs/features/core/terminal-connection-writer-lock.md)
- [ ] **PTY Advanced Extraction**: [specs/features/core/pty-advanced-extraction.md](specs/features/core/pty-advanced-extraction.md)
- [ ] **Monitoring and Metrics**: [specs/features/core/monitoring-and-metrics.md](specs/features/core/monitoring-and-metrics.md)
- [x] **Multiple Projects Design**: [specs/features/core/design-multiple-projects.md](specs/features/core/design-multiple-projects.md)
- [ ] **Session Streaming Storage Audit**: [specs/features/core/session-streaming-storage.md](specs/features/core/session-streaming-storage.md)

### Frontend & User Interface
- [x] **Frontend Architecture**: [specs/features/ui/frontend-architecture.md](specs/features/ui/frontend-architecture.md)
- [ ] **UI Flows**: [specs/features/ui/ui-flows.md](specs/features/ui/ui-flows.md)
- [ ] **Management Interfaces**: [specs/features/ui/management-interfaces.md](specs/features/ui/management-interfaces.md)
- [x] **Agent Session Viewer**: [specs/features/ui/agent-session-viewer.md](specs/features/ui/agent-session-viewer.md)
- [ ] **Session Terminal Dashboard**: [specs/features/ui/session-terminal-dashboard.md](specs/features/ui/session-terminal-dashboard.md)
- [ ] **Task Tree and Git Viewers**: [specs/features/ui/task-tree-and-git-viewers.md](specs/features/ui/task-tree-and-git-viewers.md)
- [ ] **Transcript Paging Contract**: [specs/features/ui/transcript-paging-contract.md](specs/features/ui/transcript-paging-contract.md)

### CodeFlow Explorer (Visualization)
- [ ] **Read-After-Write Detection**: [specs/features/codeflow/read-after-write-detection.md](specs/features/codeflow/read-after-write-detection.md)
- [x] **CodeFlow Explorer Backend Design**: [specs/features/codeflow/codeflow-explorer-backend-design.md](specs/features/codeflow/codeflow-explorer-backend-design.md)
- [ ] **CodeFlow Node Graph Canvas Interface**: [specs/features/codeflow/node-graph-canvas-interface.md](specs/features/codeflow/node-graph-canvas-interface.md)
- [ ] **CodeFlow Explorer Data Flow Type Lineage**: [specs/features/codeflow/data-flow-type-lineage.md](specs/features/codeflow/data-flow-type-lineage.md)
- [ ] **CodeFlow Explorer Execution Flow Interface**: [specs/features/codeflow/execution-flow-interface.md](specs/features/codeflow/execution-flow-interface.md)
- [ ] **CodeFlow Explorer Flat Interface**: [specs/features/codeflow/flat-interface.md](specs/features/codeflow/flat-interface.md)

### Integrations & Providers
- [ ] **Claude Programmatic Provider**: [specs/features/providers/claude-programmatic-provider.md](specs/features/providers/claude-programmatic-provider.md)
- [ ] **Claude Fidelity Integration**: [specs/features/providers/claude-fidelity-integration.md](specs/features/providers/claude-fidelity-integration.md)
- [ ] **Provider Conformance**: [specs/features/providers/provider-conformance.md](specs/features/providers/provider-conformance.md)
- [ ] **Codex Provider**: [specs/future/providers/codex-provider.md](specs/future/providers/codex-provider.md)
- [ ] **MCP Server Integration**: [specs/features/integrations/mcp-server-integration.md](specs/features/integrations/mcp-server-integration.md)
- [ ] **MCP Agent Design**: [specs/features/integrations/mcp-agent-design.md](specs/features/integrations/mcp-agent-design.md)
- [ ] **Strand Task Integration**: [specs/features/integrations/strand-task-integration.md](specs/features/integrations/strand-task-integration.md)

### Security & Access Control
- [ ] **Authz Management Interfaces**: [specs/features/security/authz-management-interfaces.md](specs/features/security/authz-management-interfaces.md)
- [ ] **Management Interface Threat Model**: [specs/features/security/management-interface-threat-model.md](specs/features/security/management-interface-threat-model.md)
