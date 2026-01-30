# OrbitMesh Project Structure and Build System

**Status**: ✓ Implemented
**Date**: 2026-01-30
**Decision**: Alternative 2 - Taskfile + Go Workspaces
**Owner Approval**: Confirmed

## Overview

OrbitMesh uses a monorepo structure with Taskfile-based task orchestration, Go Workspaces for backend module management, and PNPM for frontend dependencies. This design prioritizes developer experience, simple installation, and cross-platform compatibility.

## Selected Architecture: Taskfile + Go Workspaces

### Why This Approach?

**Key Strengths**:
- **Modern YAML Syntax**: Readable Taskfile configuration with no tab-sensitivity issues
- **Checksum-Based Change Detection**: More reliable than Make's timestamps (works with Docker, git)
- **Native Cross-Platform**: Single Taskfile works identically on Mac, Linux, and Windows
- **Modular Organization**: Each module (backend, frontend) defines its own tasks
- **Low Learning Curve**: YAML is approachable for developers new to the project
- **Zero Database Dependencies**: File-based storage with JSON files
- **Reasonable Setup Time**: 3-4 days vs 5-7 for more complex alternatives

### Alternative Approaches Considered

See [alternatives analysis](00-alternatives-analysis.md) for detailed comparison of:
1. Make + Go Workspaces (traditional, lowest learning curve)
2. **Taskfile + Go Workspaces** (chosen - modern, balanced)
3. Mage (Go-first, type-safe build logic)
4. Turborepo + Taskfile (maximum optimization, highest complexity)

## Project Structure

```
orbitmesh/
├── .github/
│   └── workflows/              # GitHub Actions CI/CD
│       ├── backend.yml         # Go backend pipeline
│       └── frontend.yml        # TypeScript pipeline
├── backend/
│   ├── cmd/                    # Executables
│   │   ├── orbitmesh/          # Main server
│   │   └── orbitmesh-mcp/      # MCP server
│   ├── internal/               # Private packages
│   │   ├── agent/              # Agent execution engine
│   │   ├── provider/           # Provider abstraction
│   │   ├── pty/                # PTY provider implementation
│   │   ├── mcp/                # MCP server implementation
│   │   ├── metrics/            # Metrics collection
│   │   └── storage/            # File-based storage
│   ├── pkg/                    # Public packages
│   │   └── api/                # Shared API types
│   ├── go.mod                  # Module definition
│   └── Taskfile.yml            # Backend build tasks
├── frontend/
│   ├── src/
│   │   ├── components/         # Reusable UI components
│   │   ├── views/              # Page-level components
│   │   ├── graph/              # D3 visualizations
│   │   └── api/                # Backend API client
│   ├── public/                 # Static assets
│   ├── index.html              # HTML entry
│   ├── package.json            # Dependencies
│   ├── vite.config.ts          # Vite configuration
│   ├── vitest.config.ts        # Test configuration
│   ├── tsconfig.json           # TypeScript configuration
│   └── Taskfile.yml            # Frontend build tasks
├── design-docs/                # Architecture documentation
├── docs/                       # User documentation
├── scripts/                    # Build and utility scripts
│   └── setup-dev.sh            # One-command setup
├── Taskfile.yml                # Root task orchestration
├── .editorconfig               # Editor configuration
├── .air.toml                   # Backend hot reload config
├── .gitignore                  # Git ignore rules
├── go.work                     # Go workspace (local development)
├── pnpm-workspace.yaml         # PNPM workspace config
├── CLAUDE.md                   # Project architecture overview
├── README.md                   # Getting started guide
├── CONTRIBUTING.md             # Development guidelines
└── LICENSE                     # License file
```

## Build System: Taskfile

### Installation

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

### Main Taskfile (Root)

Orchestrates backend and frontend tasks via includes:

```yaml
includes:
  backend: ./backend/Taskfile.yml
  frontend: ./frontend/Taskfile.yml

tasks:
  dev:       # Start full development environment
  build:     # Build for production
  test:      # Run all tests
  lint:      # Lint all code
  clean:     # Clean artifacts
  ci:        # Full CI pipeline
```

### Backend Taskfile

Go-specific build tasks:

```yaml
tasks:
  build:        # Compile binaries
  test:         # Run tests with coverage
  test:short:   # Fast unit tests only
  lint:         # Format and vet
  dev:          # Hot reload with Air
  fmt:          # Format code
  mod:download: # Update dependencies
  mod:tidy:     # Clean up go.mod
```

### Frontend Taskfile

TypeScript/Vite specific tasks:

```yaml
tasks:
  install:  # pnpm install
  build:    # Production build
  dev:      # Dev server
  test:     # Run tests
  test:ui:  # Interactive test UI
  lint:     # Lint code
```

### Common Development Commands

```bash
# Start development (both backend + frontend)
task dev

# Backend only (with hot reload)
task backend:dev

# Frontend only (with Vite dev server)
task frontend:dev

# Run tests
task test

# Run linter
task lint

# Build for production
task build

# View all available tasks
task --list
```

## Storage Architecture

### No External Databases

OrbitMesh uses **file-based JSON storage** with no Postgres, Redis, or other databases:

**Benefits**:
- Zero external dependencies for users
- Simple installation (just Go + Node)
- Easy to inspect and debug (plain JSON files)
- No database setup, migrations, or maintenance
- Portable across machines

### Storage Layout

```
~/.orbitmesh/
├── sessions/
│   ├── session-1.json      # Session transcript
│   ├── session-2.json
│   └── ...
├── agents/
│   ├── agent-1.json        # Agent state
│   ├── agent-2.json
│   └── ...
└── config.json             # Global configuration
```

### Backend Implementation

`backend/internal/storage/` package handles:
- JSON file reading/writing
- Directory management
- Session lifecycle management
- State persistence

### StrandYard Integration

Task metadata is stored in **StrandYard tasks** (via MCP):
- Task definitions and status
- Agent role configurations
- Task templates
- Workflow metadata

This eliminates need for a separate task database.

## Go Workspace Management

### What is go.work?

`go.work` enables simultaneous development of multiple Go modules:

```
go.work (root - LOCAL DEVELOPMENT)
└── uses backend/go.mod
```

**Benefits**:
- Modify internal packages and see changes immediately
- No need to publish or tag dependencies during development
- Go automatically uses local versions during dev

### Important Notes

- `go.work` is in `.gitignore` (not committed)
- Each developer has their local `go.work`
- CI always builds with `GOWORK=off` (ignores go.work)
- Production builds never use go.work

### Setting Up Workspace

```bash
# Automatic (via task setup)
task setup

# Manual
go work init .
go work use ./backend
```

## Frontend Setup: PNPM + Vite

### PNPM Workspace

`pnpm-workspace.yaml` defines workspace structure:

```yaml
packages:
  - 'frontend'
```

**Benefits**:
- Monorepo-aware dependency management
- Content-addressable storage (80% disk savings vs npm)
- Faster installation and builds
- Prevents phantom dependencies

### Vite Configuration

`frontend/vite.config.ts`:
- Dev server on port 3000
- API proxy to backend on port 8080
- SolidJS plugin configuration
- Production build optimization

### Hot Module Replacement

Vite automatically reloads frontend when:
- TypeScript/TSX files change
- CSS files change
- index.html changes

No manual refresh needed during development.

## CI/CD Pipeline

### GitHub Actions

Two parallel workflows:

**backend.yml**:
- Runs on `backend/**` changes
- Tests with Go 1.24
- Lint, test, build
- Upload coverage to Codecov

**frontend.yml**:
- Runs on `frontend/**` changes
- Tests with Node 20
- Lint, test, build
- PNPM dependency caching

### Cache Strategy

- Go module cache: `actions/setup-go` with `cache-dependency-path`
- PNPM cache: `pnpm/action-setup` with Node actions

### Running Locally

```bash
# Replicate CI pipeline
task ci
```

## Development Environment Setup

### One-Command Setup

```bash
./scripts/setup-dev.sh
```

Automatically:
1. Checks prerequisites (Go, Node)
2. Installs Taskfile if needed
3. Enables PNPM via Corepack
4. Downloads Go modules
5. Installs Node dependencies
6. Prints next steps

### Manual Setup

```bash
# Install Taskfile
go install github.com/go-task/task/v3/cmd/task@latest

# Enable PNPM
corepack enable

# Backend
cd backend && go mod download && cd ..

# Frontend
cd frontend && pnpm install && cd ..
```

## IDE Integration

### VSCode

1. Install [Task](https://marketplace.visualstudio.com/items?itemName=spencerwmiles.task) extension
2. View tasks with `Ctrl+Shift+P` → "Task: Run Task"
3. Keybind common tasks for quick access

### GoLand / IntelliJ

1. Settings → Tools → Task
2. Path: `/usr/local/bin/task` (or wherever installed)
3. Right-click tasks in file explorer

### Terminal

```bash
task --list          # See all available tasks
task dev             # Run any task
```

## Migration Path to Turborepo

If CI performance becomes critical as the project grows:

1. Install Turborepo: `npm install -g turbo`
2. Create `turbo.json` with pipeline definitions
3. Wrap Taskfile commands in Turborepo cache
4. Enable remote caching (optional)

**Migration effort**: ~2-3 days, minimal refactoring

This can be done incrementally without breaking existing workflows.

## Performance Considerations

### Build Performance

**Checksum-based detection** (Taskfile):
- Only rebuilds changed files
- Faster than Make's timestamp detection
- Works reliably with Docker and git

**Incremental compilation** (Go):
- Go compiles only changed packages
- Frontend uses Vite's dependency pre-bundling
- Hot reload enabled for both stacks

### Development Experience

**Backend**:
- Air hot reload (1-2 second rebuild)
- Immediate feedback on changes
- Log output in terminal

**Frontend**:
- Vite HMR (sub-second updates)
- Preserves component state
- Error overlay for quick debugging

## Maintenance Burden

### Low Complexity

- Single tool per technology (Taskfile, Go, Node)
- Clear separation of concerns
- Minimal configuration files
- Self-documenting YAML syntax

### Common Updates

```bash
# Update Go dependencies
task backend:mod:tidy

# Update Node dependencies
cd frontend && pnpm update

# Update Taskfile itself (rarely needed)
go install github.com/go-task/task/v3/cmd/task@latest@latest
```

## Testing Strategy

### Backend Testing

```bash
# All tests with coverage
task backend:test

# Fast unit tests
task backend:test:short

# Specific test
cd backend && go test -v ./internal/agent -run TestName
```

**Coverage**: Enforced via CI pipeline

### Frontend Testing

```bash
# All tests
task frontend:test

# Watch mode
cd frontend && pnpm test --watch

# UI mode
task frontend:test:ui
```

## Scalability

This architecture scales well as the project grows:

**Adding new packages**:
1. Create new directory in `backend/internal/` or `frontend/src/`
2. No Taskfile changes needed (already structured for growth)
3. New tests automatically run in `task test`

**Adding new providers**:
1. Create `backend/internal/provider/<provider-name>/`
2. Implement provider interface
3. No build system changes

**Adding new frontend modules**:
1. Create new directory in `frontend/src/`
2. Import and use normally
3. Vite bundles automatically

## Future Considerations

### Remote Caching (Potential)

If needed, add:
- Turborepo layer for distributed cache
- Remote cache server (Vercel or self-hosted)
- Docker-based containerized builds

### Database Addition (If Needed)

Currently not needed. If complexity grows:
1. Add lightweight SQLite for local data (no server)
2. Or use file-based storage with schema

### Multi-Workspace Expansion

If adding more workspaces:
1. Update `pnpm-workspace.yaml`
2. Create subdirectories with `package.json`
3. Update root Taskfile includes

## Decisions Rationale

### Why Taskfile over Make?

- YAML readability (no tab sensitivity)
- Checksum-based detection (more reliable)
- Native Windows support (important for team)
- Better DX with colors and status messages
- Still small learning curve (vs Turborepo, Mage)

### Why No Databases?

- Simpler deployment and installation
- Easier to debug and inspect data
- StrandYard handles task metadata
- JSON files sufficient for session data
- Lower operational complexity

### Why Separate Frontend package.json?

- Allows PNPM workspace features
- Each workspace has independent versions
- Can update frontend deps without touching backend
- Standard Node.js project structure

### Why Go Workspaces?

- Seamless development of multiple modules
- No need for go.mod version gymnastics
- CI still builds against published modules (GOWORK=off)
- Scales well as backend grows

## Summary

OrbitMesh's project structure balances:
- **Simplicity**: No external dependencies, minimal configuration
- **Modern DX**: Taskfile + Vite + Go workspaces
- **Scalability**: Well-organized directories, clear module boundaries
- **Maintainability**: Few tools, clear workflows, good documentation

This foundation supports all planned features while remaining accessible to new contributors.

---

See [CLAUDE.md](../CLAUDE.md) for the complete project architecture and phased implementation plan.
