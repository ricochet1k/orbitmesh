# OrbitMesh

An agent orchestration system for managing and monitoring AI agents across multiple providers and execution environments.

## Quick Start

### Prerequisites

- Go 1.24 or later
- Node.js 20 or later
- Taskfile 3.x
- Overmind + tmux (for `task dev`)

### Setup

```bash
# Run the setup script (installs dependencies and initializes workspace)
./scripts/setup-dev.sh
```

Or manually:

```bash
# Install Taskfile (if not already installed)
go install github.com/go-task/task/v3/cmd/task@latest

# Enable PNPM
corepack enable

# Initialize backend
cd backend && go mod download && cd ..

# Install frontend dependencies
cd frontend && pnpm install && cd ..
```

### Development

```bash
# Start full development environment (backend + frontend)
task dev

# Backend runs on http://localhost:8080
# Frontend runs on http://localhost:3000
```

If you don't have Overmind installed, use `task dev:manual` for manual commands.

## Interface Views

- Task Tree: `http://localhost:3000/tasks/tree` for hierarchical task status and filtering.
- Commit History: `http://localhost:3000/history/commits` for commit list and diff viewer.
- Session Viewer: `http://localhost:3000/sessions/<session-id>` for live transcript, PTY output, and session controls.
- System Graph: selecting nodes highlights the corresponding task or commit.

## API Examples

```bash
# Fetch the full task tree
curl -s http://localhost:8080/api/v1/tasks/tree | jq

# List recent commits (default limit 25, max 200)
curl -s "http://localhost:8080/api/v1/commits?limit=50" | jq

# Fetch a specific commit diff
curl -s http://localhost:8080/api/v1/commits/<sha> | jq

# List active sessions
curl -s http://localhost:8080/api/sessions | jq

# Stream session events (SSE)
curl -N http://localhost:8080/api/sessions/<session-id>/events

# Pause or resume a session
curl -X POST http://localhost:8080/api/sessions/<session-id>/pause
curl -X POST http://localhost:8080/api/sessions/<session-id>/resume
```

## Project Structure

```
orbitmesh/
├── backend/                 # Go backend
│   ├── cmd/                # Executables
│   │   ├── orbitmesh/      # Main server
│   │   └── orbitmesh-mcp/  # MCP server
│   ├── internal/           # Internal packages
│   │   ├── agent/          # Agent execution engine
│   │   ├── provider/       # Provider implementations
│   │   ├── pty/            # PTY provider logic
│   │   ├── mcp/            # MCP server implementations
│   │   ├── metrics/        # Metrics collection
│   │   └── storage/        # File-based storage
│   ├── pkg/                # Public packages
│   │   └── api/            # Shared API types
│   └── Taskfile.yml        # Backend build tasks
│
├── frontend/               # TypeScript/SolidJS frontend
│   ├── src/
│   │   ├── components/     # UI components
│   │   ├── views/          # Page views
│   │   ├── graph/          # D3 visualizations
│   │   └── api/            # Backend API client
│   ├── public/             # Static assets
│   ├── Taskfile.yml        # Frontend build tasks
│   └── vite.config.ts      # Vite configuration
│
├── docs/                   # Documentation
│   └── backend-architecture.md  # Core backend components
├── scripts/                # Build and utility scripts
├── Taskfile.yml            # Root task orchestration
├── .editorconfig           # Editor configuration
├── .gitignore              # Git ignore rules
└── README.md               # This file
```

## Build Commands

All commands use Taskfile. View available tasks:

```bash
task --list
```

### Common Tasks

```bash
# Build everything
task build

# Run tests
task test

# Run linters
task lint

# Clean build artifacts
task clean

# Full CI pipeline (lint → test → build)
task ci
```

### Backend Tasks

```bash
# Build backend binaries
task backend:build

# Run backend tests
task backend:test

# Run fast unit tests only
task backend:test:short

# Lint backend code
task backend:lint

# Format code
task backend:fmt

# Start backend with hot reload
task backend:dev

# Download/update dependencies
task backend:mod:download
```

### Frontend Tasks

```bash
# Install/update dependencies
task frontend:install

# Build for production
task frontend:build

# Run tests
task frontend:test

# Run tests with UI
task frontend:test:ui

# Lint code
task frontend:lint
```

## Storage & Configuration

OrbitMesh uses file-based storage with no external databases:

### Session Storage
```
~/.orbitmesh/
├── sessions/           # Session transcripts (JSON)
│   └── <session-id>.json
├── agents/             # Agent state (JSON)
│   └── <agent-id>.json
└── config.json         # Configuration
```

### StrandYard Integration
Task metadata is stored in StrandYard tasks, avoiding the need for a separate database.

## Development Workflow

### Making Changes

1. Create a feature branch
2. Make changes (backend and/or frontend)
3. Run tests: `task test`
4. Run linter: `task lint`
5. Commit changes
6. Push and open a pull request

### Hot Reload

Both backend and frontend support hot reload during development:

- **Backend**: Modifying Go files triggers automatic rebuild via Air
- **Frontend**: Modifying TypeScript/CSS triggers Vite hot module replacement

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed development guidelines.

## Architecture

### Design Decisions

- **Monorepo**: Single repository with Go backend and TypeScript frontend
- **Build System**: Taskfile + Go Workspaces for simple, modern tooling
- **Storage**: File-based JSON with no external database dependencies
- **CI/CD**: GitHub Actions for automated testing and deployment

See [design-docs/](design-docs/) for detailed architecture documentation including [Frontend Architecture](design-docs/03-frontend-architecture.md) and [docs/backend-architecture.md](docs/backend-architecture.md) for core backend components.

## License

See [LICENSE](LICENSE) file for details.

## Status

This project is in development (Phase 3: PTY Provider). See [CLAUDE.md](CLAUDE.md) for the project roadmap.
