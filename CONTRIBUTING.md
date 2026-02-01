# Contributing to OrbitMesh

Thank you for contributing! This guide explains our development workflow and standards.

## Setup

See [README.md](README.md) for initial setup instructions.

## Development Workflow

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Make Your Changes

Work on backend (`backend/`), frontend (`frontend/`), or both.

### 3. Test Your Changes

```bash
# Run all tests
task test

# Run only backend or frontend tests
task backend:test
task frontend:test

# Run tests in watch mode (frontend)
cd frontend
pnpm test --watch
```

### 4. Check Code Quality

```bash
# Lint all code
task lint

# Format code
task backend:fmt
task frontend:fmt  # if prettier is configured
```

### 5. Build for Production

```bash
task build
```

### 6. Commit and Push

```bash
git add .
git commit -m "Descriptive commit message"
git push origin feature/your-feature-name
```

### 7. Open a Pull Request

Create a pull request on GitHub with:
- Clear title describing the change
- Description of what changed and why
- Reference to any related issues

## Code Standards

### General

- Follow existing code style in the codebase
- Use `.editorconfig` for consistent formatting
- Write descriptive commit messages

### Go Backend

- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `go fmt` for formatting (enforced by `task backend:fmt`)
- Use `go vet` for static analysis (run by `task backend:lint`)
- Write tests for new functionality
- Use `internal/` for private packages, `pkg/` for public APIs

### TypeScript Frontend

- Use TypeScript strictly (no `any` types without justification)
- Use Solid.js idioms (reactivity primitives, not hooks)
- Write tests for components and utilities
- Use meaningful variable and function names
- Follow the existing component structure

## Testing

### Backend

```bash
# Run all tests with coverage
task backend:test

# Run specific test
cd backend
go test -v ./internal/agent -run TestAgentExecute

# Run with verbose output
go test -v ./...
```

### Frontend

```bash
# Run all tests
task frontend:test

# Run in watch mode
cd frontend
pnpm test --watch

# Run specific test file
pnpm test AgentCard.test.tsx

# Generate coverage report
pnpm test --coverage
```

## Building and Deploying

### Local Build

```bash
task build
```

Outputs:
- Backend: `backend/dist/orbitmesh`, `backend/dist/orbitmesh-mcp`
- Frontend: `frontend/dist/`

### CI/CD Pipeline

GitHub Actions automatically runs on:
- Push to `main` or `develop` branches
- Pull requests to `main` or `develop`

Workflow:
1. Lint code
2. Run tests
3. Build artifacts

Check `.github/workflows/` for detailed pipeline configuration.

## Project Structure Philosophy

### File Organization

**Backend**:
- `cmd/` - Executable entry points only
- `internal/` - Application business logic (not importable by external packages)
- `pkg/` - Public API packages (safe to import externally)

**Frontend**:
- `components/` - Reusable UI components
- `views/` - Page-level components
- `graph/` - D3 visualization logic
- `api/` - Backend API client

### Storage

No databases (Postgres, Redis, etc.) are used:
- Session data: `~/.orbitmesh/sessions/<id>.json`
- Agent state: `~/.orbitmesh/agents/<id>.json`
- Task metadata: StrandYard MCP integration

This keeps OrbitMesh lightweight and easy to deploy.

## Common Issues

### Taskfile not found

Install with:
```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

### `go.work` conflicts

The `go.work` file is local development only (in `.gitignore`). Don't commit it.

### Frontend dependencies not updating

```bash
cd frontend
rm -rf node_modules pnpm-lock.yaml
pnpm install
```

### Build fails on fresh checkout

```bash
task setup
task build
```

## Questions?

- Check existing [issues](https://github.com/ricochet1k/orbitmesh/issues)
- Review [CLAUDE.md](CLAUDE.md) for architecture details
- See [design-docs/](design-docs/) for design decisions

## Code of Conduct

Please treat all contributors with respect. We maintain a welcoming and inclusive community.
