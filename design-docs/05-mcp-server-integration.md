# MCP Server Integration Design

## Overview
OrbitMesh integrates with the Model Context Protocol (MCP) to allow agents to interact with our systems (StrandYard, OrbitMesh orchestration) using a standardized protocol.

## System Architecture

### StrandYard MCP Server
Provides tools for agents to manage their own tasks and report progress.
- **Tools**:
  - `list_tasks`: Retrieve assigned tasks.
  - `update_task`: Update task status or report progress.
  - `add_subtask`: Create new sub-tasks for a goal.
- **Resources**:
  - `task_details`: Full markdown content of a task.

### OrbitMesh MCP Server
Exposes internal orchestration state and monitoring.
- **Tools**:
  - `list_sessions`: Show all active agent sessions.
  - `get_session_status`: Retrieve metrics and state for a specific session.
- **Resources**:
  - `system_metrics`: Real-time CPU/Memory and token usage.

## Protocol Specification
- **Transport**: Default to `stdio` for native execution; `SSE` or `WebSockets` for remote dashboard integration.
- **JSON-RPC**: Standard MCP JSON-RPC 2.0 messages for all tool calls and notifications.

## Security Model
- **Isolation**: Each MCP server process runs with the same permissions as the agent session it serves.
- **Validation**: Strict input validation for all tool parameters.
- **Auditing**: All MCP tool calls are logged as `MetadataEvent`s in the session transcript.

## Implementation Strategy
1. **Scaffold**: Create `backend/cmd/orbitmesh-mcp` as the entry point.
2. **Tools**: Implement tools using the `github.com/modelcontextprotocol/go-sdk/mcp` library.
3. **Integration**: Register MCP servers in the `ADKProvider` during session startup.
