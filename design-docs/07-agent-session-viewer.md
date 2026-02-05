# Agent Session Viewer & Controller Design

## Overview
This document outlines the design for the Agent Session Viewer and Controller, providing users with real-time visibility and control over agent activities.

## Interface Components

### Live Session Transcript
- **View**: `/sessions/{id}`
- **Features**:
  - Real-time streaming of agent output via SSE.
  - Automatic scrolling with manual override.
  - Distinct message types (agent, user, system, error).
  - Code block highlighting.

### Historical Replay
- **Features**:
  - Load and play back previous session transcripts.
  - Scrubbing timeline to jump to specific points.
  - Export session as JSON or Markdown.

### PTY Terminal Emulation
- **Component**: `TerminalView`
- **Library**: `xterm.js` for robust terminal emulation.
- **Integration**: Bi-directional communication with the PTY provider via WebSockets or specialized SSE events.

### Session Controls
- **Actions**:
  - `Pause/Resume`: Direct control over the agent lifecycle.
  - `Kill`: Immediate termination of the session.
  - `Interactive Mode`: Allow users to send prompts directly to the agent.

## Interaction Protocol
- **Input**: User messages are sent via `POST /api/v1/sessions/{id}/input`.
- **Output**: Agent responses are received via the established SSE stream.
- **PTY Data**: Raw terminal data is streamed as base64-encoded strings within metadata events.

## User Experience (UX)
- **High Information Density**: Prioritize readability of the transcript.
- **Contextual Actions**: Action buttons (Pause, Kill) clearly visible but protected from accidental clicks.
- **Search**: Search within the current transcript or across historical sessions.
