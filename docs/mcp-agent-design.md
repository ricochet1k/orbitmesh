# MCP Agent Component Library and Agent Box Design

## Summary
This document defines the MVP design for a SolidJS component library that exposes
high-level MCP actions/data (no direct DOM control), plus a minimized-by-default
agent box that uses existing provider/session rendering code. It also outlines
low-priority follow-ups.

## Goals
- Provide MCP-visible components (Button/Input/Read-only text) via a component
  library with consistent metadata and action registration.
- Ensure MCP-triggered actions animate (scroll into view, pulse) before firing
  standard events and returning results.
- Allow multi-field edits via a single MCP action.
- Restrict MCP to high-level exposed actions/data only; no general page
  interaction or arbitrary DOM control.
- Use existing provider and session rendering code where possible.
- Keep navigation within the same server.
- Ship an always-visible agent box in the existing blank position, minimized by
  default and user-toggleable.

## Non-Goals
- No arbitrary DOM querying, clicking, or scripting.
- No cross-origin or cross-server navigation.
- No full redesign of the agent box layout (beyond the minimized/default state).
- No advanced permission flows or auditing in MVP.

## Constraints
- SolidJS (prefer create-style APIs rather than React-style hooks).
- Components should be the primary MCP action surface (not ad-hoc DOM binding).
- Actions must return structured success/failure responses to MCP.

## High-Level Architecture

### Core Pieces
- MCP Component Library: SolidJS components that accept MCP metadata and
  register actions.
- MCP Registry/Dispatch: in-memory registry of components and actions plus a
  dispatcher to run them.
- MCP Bridge: transport/auth layer between the MCP server and the frontend.
- Agent Box: a minimized-by-default UI container that displays status and
  recent action results.

### Data Flow (MVP)
1) Component mounts with MCP metadata -> registers actions in registry.
2) MCP server requests action -> bridge -> dispatcher.
3) Dispatcher locates component, runs animation (scroll + pulse).
4) Component fires standard event (click/input/change/etc.).
5) Component returns structured result to MCP.

## Component Library Design

### Common MCP Metadata Props
All MCP-enabled components accept:
- mcpName: string (unique-ish human-friendly identifier)
- mcpDescription: string (agent-facing description)
- mcpActions: optional overrides or custom actions
- mcpId: optional stable ID for deterministic lookups

### Action Return Shape
All actions return:
```
{
  ok: boolean,
  error?: string,
  data?: unknown
}
```

### Standard Action Types
- click
- edit
- focus
- select
- toggle
- read (for read-only components)

### MVP Components
- McpButton
  - wraps existing button
  - actions: click
  - animation: scroll-into-view + pulse
  - event: onClick

- McpInputField
  - wraps existing input
  - actions: edit, focus
  - animation: scroll-into-view + pulse
  - event: onInput/onChange

- McpDataText (read-only)
  - actions: read
  - returns current value/text

### Animation Behavior
- Always scroll target into view before executing MCP action.
- Pulse visual on the component (or provided wrapper) when action executes.
- Animation should occur before firing the standard event.

## MCP Registry and Dispatch

### Registry
- createMcpRegistry
  - register component instances with metadata and action handlers
  - unregister on unmount

### Dispatch
- createMcpDispatch
  - invoke action by component ID + action type
  - run scroll/pulse animation
  - invoke component event handler
  - return standardized result

## Multi-Field Edit Action

### API
- Accept one of:
  - array: [{ fieldId, value }]
  - map: { [fieldId]: value }

### Behavior
- For each field:
  - find target component
  - run scroll + pulse
  - fire edit event
  - capture per-field result

### Return Shape
```
{
  ok: boolean,
  results: {
    [fieldId]: { ok: boolean, error?: string }
  }
}
```

## Agent Box (MVP)

### Requirements
- Use existing blank position.
- Minimized by default; toggle to expand.
- Show status + last action result(s).
- Integrate with existing provider/session rendering where possible.

## Navigation Constraints
- Only allow same-server navigation (relative routes or same-origin URLs).
- Provide a guard in MCP action handler to reject invalid routes.

## MCP Bridge
- MCP requests are routed to the dispatcher.
- Bridge is responsible for auth/session identity.
- Bridge does not expose raw DOM access.

## Testing (MVP)
- Action dispatch triggers animation before event.
- Standard event fires correctly and returns result.
- Multi-field edit returns per-field results.
- Same-server route guard enforced.
- MCP disconnect/reconnect behavior safe.

## Low-Priority Follow-Ups
- Additional MCP components (select, toggle, checkbox, list items, modal action).
- Permissions and user confirmation UX.
- Design alternatives for agent box.
- Observability: audit logs and action metrics.
- Reliability: retry, throttling, offline mode.

## Open Questions
- Final naming: McpDataText vs McpSpan (choose during implementation).
- Where to store MCP component IDs (if deterministic mapping is needed).
