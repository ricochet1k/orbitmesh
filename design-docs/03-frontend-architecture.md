# Frontend Architecture & Dashboard Design

## Overview
The OrbitMesh frontend is a modern, real-time dashboard built with SolidJS and D3.js. It provides a live visualization of the agent orchestration system, including agents, tasks, and their relationships.

## Component Hierarchy
- `App`: Main entry point, handles routing and layout.
- `Dashboard`: Principal view containing the session list and the system graph.
  - `SessionList`: Tabular view of active agent sessions.
  - `AgentGraph`: D3-powered force-directed graph visualization.

## D3 Graph Data Model
The system graph represents the state of the mesh using:
- **Nodes**:
  - `agent`: Represents a running AI agent.
  - `task`: Represents a specific task or goal.
  - `commit`: (Future) Represents a git commit or state checkpoint.
- **Links**:
  - `executing`: Connects an agent to its currently active task.
  - `depends_on`: Connects a task to its parent or dependency.

### Update Strategy
The graph uses a force-directed simulation to maintain a readable layout.
- **Initialization**: Nodes and links are fetched via the API on mount.
- **Updates**: Real-time updates are received via Server-Sent Events (SSE).
- **Transitions**: D3 transitions and the simulation tick ensure smooth movement as agents switch tasks or new tasks are created.

## Real-Time Architecture
The frontend maintains a persistent connection to the backend using SSE.
1. **Connection**: The `apiClient` establishes an SSE connection to `/api/v1/sessions/{id}/events`.
2. **Event Processing**: Events (status changes, output, metrics) are dispatched to the relevant components.
3. **State Management**: SolidJS signals and resources are updated, triggering reactive UI changes.

## UI/UX Design
The dashboard is designed for high-density information display:
- **Left Panel**: Detailed session list with status badges and action buttons.
- **Right Panel**: Large-scale system graph for high-level monitoring.
- **Interactivity**: Nodes can be dragged to explore the graph; clicking a node provides more details.
- **Guardrails**: Locked actions include inline helper text and a Request access link (no tooltip-only guidance).
