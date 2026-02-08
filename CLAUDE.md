# OrbitMesh

An agent orchestration system for managing and monitoring AI agents across multiple providers and execution environments.

## Overview

OrbitMesh is a comprehensive agent orchestration platform consisting of:

- **Backend (Golang)**: Agent execution engine supporting multiple AI providers
- **Frontend (TypeScript + SolidJS)**: Rich, live dashboard for monitoring and controlling agents

The system integrates deeply with StrandYard for task management, allowing agents to track their work through MCP servers.

## Core Architecture

### Backend Components

#### 1. Agent Execution Engine
- Multi-provider support with pluggable provider architecture
- Session management and lifecycle control
- Real-time status tracking and reporting
- Interactive pause/resume capabilities

#### 2. Provider Types

**Native Providers (e.g., Google ADK)**
- Direct SDK integration
- Programmatic agent control
- Native event streaming

**PTY Providers (CLI/TUI tools)**
- Uses `termemu` library for PTY management
- Supports claude, codex, amp, and other CLI agents
- Handles authentication through CLI tools
- Screen buffer analysis and status extraction

#### 3. Status Extraction System (PTY)
- Position-based extraction (fixed coordinates)
- Regex-based pattern matching
- AI-assisted runtime analysis
  - Dynamic extraction rule generation
  - Screen interpretation on-the-fly
  - Adaptive status detection

#### 4. MCP Servers
- **StrandYard MCP**: Task management integration
- **OrbitMesh MCP**: Internal orchestration API

### Frontend Components

#### 1. Visualization Layer
- **Force-Directed Graph (D3.js)**
  - Live animated view of commits or tasks
  - Agents as floating arrows
  - Real-time movement as agents change tasks
  - Interactive node exploration

#### 2. Management Interfaces
- **Agent Role Editor**: Create and configure agent roles
- **Task Template Manager**: Define reusable task templates
- **Task Management**: Create, edit, and organize tasks
- **Session Controller**: Pause, resume, and interact with agent sessions

#### 3. Viewers & Inspectors
- **Agent Session Viewer**: Live and historical session transcripts
- **Task Tree View**: Hierarchical task visualization
- **Git Commit Viewer**: Standard commit history interface

#### 4. Metrics & Analytics
- Lines changed over time
- Merge conflict tracking
- Token usage and cost analysis (per model, per project)
- Code coverage metrics
- CPU and memory usage monitoring

## Technical Stack

### Backend
- **Language**: Go
- **Key Libraries**:
  - termemu (PTY management)
  - Google ADK (Gemini agent support)
  - StrandYard (task management)

### Frontend
- **Framework**: SolidJS
- **Visualization**: D3.js
- **Language**: TypeScript

## Integration Points

### StrandYard
- Agents receive access to StrandYard MCP server
- Track task progress and status
- Follow role-based workflows
- Report completion and blockers

### OrbitMesh MCP
- Internal orchestration commands
- Status reporting
- Session control

## Key Features

### Provider Flexibility
- Support for both SDK-based and CLI-based agents
- Unified interface regardless of provider type
- Easy addition of new providers

### Intelligent Status Extraction
- Multiple extraction methods (position, regex, AI-assisted)
- Runtime adaptation to different CLI tools
- Minimal configuration required

### Rich Monitoring
- Real-time visualization of agent activity
- Comprehensive metrics and analytics
- Historical session replay

### Interactive Control
- Pause and interact with running agents
- Works with both native and PTY providers
- Manual intervention when needed

## Development Workflow

This project uses StrandYard for task management. Use the `strand` CLI to:

- `strand next` - Get the next task to work on
- `strand complete <id>` - Mark tasks as completed
- `strand add task` - Create new tasks
- `strand list` - View all tasks

## Project Structure

```
orbitmesh/
├── backend/           # Go backend
│   ├── cmd/          # CLI and server entrypoints
│   ├── internal/     # Internal packages
│   │   ├── agent/    # Agent execution engine
│   │   ├── provider/ # Provider implementations
│   │   ├── pty/      # PTY provider logic
│   │   ├── mcp/      # MCP server implementations
│   │   └── metrics/  # Metrics collection
│   └── pkg/          # Public packages
├── frontend/         # TypeScript/SolidJS frontend
│   ├── src/
│   │   ├── components/ # UI components
│   │   ├── views/      # Page views
│   │   ├── graph/      # D3 visualizations
│   │   └── api/        # Backend API client
│   └── public/
├── docs/             # Documentation
└── scripts/          # Build and deployment scripts
```

## Implementation Phases

### Phase 1: Foundation
- Project structure and build system
- Core backend architecture
- Basic frontend scaffold
- StrandYard integration

### Phase 2: Agent Execution
- Provider abstraction layer
- Google ADK provider
- Basic PTY provider
- Session management

### Phase 3: PTY Advanced Features
- Status extraction (position/regex)
- AI-assisted analysis
- Interactive control

### Phase 4: Frontend Core
- SolidJS app structure
- D3 force-directed graph
- Basic agent/task views
- API integration

### Phase 5: Management UIs
- Agent role editor
- Task template manager
- Task management interface

### Phase 6: Monitoring & Metrics
- Session viewer
- Metrics dashboards
- Git commit viewer
- Task tree view

### Phase 7: Polish & Deploy
- End-to-end testing
- Documentation
- Deployment automation
- Performance optimization

## Getting Started

(To be filled in as project develops)

## Contributing

(To be filled in as project develops)


## Strand Agent Instructions

## What is strand
strand is a cli task management tool intended for helping to direct AI agents to follow a specific workflow.
This workflow is defined as instructions in role documents and pre-filled todo lists in task templates.

## Purpose
These instructions define how agents should use strand to manage tasks.

## Core rules
- Use `strand next` to select work; respect role opt-in or ignore behavior.
- When asked to work on the next thing, run `strand next`, follow the returned instructions, and report a brief task summary.
- Treat the role description returned by `strand next` as canonical for how to execute the task.
- When a task is done (including planning-only), run `strand complete <task-id> --todo <todo_number>`.
- If blocked, record blockers with `strand block`.
- Use `strand add` for new tasks or issues; avoid ad-hoc task creation outside strand.
- Get the list of roles and task templates from `strand roles` and `strand templates`, add them to AGENTS.md and keep that part up to date as needed.
- If bugs or usability or missing features are discovered in attempting to use `strand`, file issues
  directly on the "strand" project with a command like `strand add issue --project strand "Issue title" <<EOF\n # Detailed markdown description \nEOF`

## Automation Skills

- **playwright-cli** (see `.claude/skills/playwright/SKILL.md`): browser automation skill available for frontend exploration, form filling, screenshot capture, or other UI interactions. Use it when a real browser session helps verify UI behavior, reproduce issues, or test flows that require clicking, typing, or capturing visual evidence.
