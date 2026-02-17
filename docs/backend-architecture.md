# Backend Architecture

This document describes the core backend components of OrbitMesh.

## Overview

The backend is built in Go and provides:

- **Session management** with a validated state machine
- **Provider abstraction** for different agent execution backends
- **Event streaming** for real-time status updates
- **File-based storage** with atomic writes

## Core Components

### Sessions

Sessions represent a running agent instance. Each session tracks its lifecycle through a state machine with validated transitions.

**States:**

| State | Description |
|-------|-------------|
| `created` | Session initialized, not yet started |
| `starting` | Provider is spinning up |
| `running` | Agent is actively executing |
| `paused` | Execution paused, can resume |
| `stopping` | Graceful shutdown in progress |
| `stopped` | Session terminated normally |
| `error` | Session terminated due to error |

**State Transitions:**

```
created → starting
starting → running | error
running → paused | stopping | error
paused → running | stopping
stopping → stopped | error
error → stopping
```

**Usage:**

```go
import "github.com/ricochet1k/orbitmesh/internal/domain"

// Create a new session
session := domain.NewSession("session-123", "claude", "/path/to/project")

// Transition to a new state
err := session.TransitionTo(domain.SessionStateStarting, "user requested start")
if err == domain.ErrInvalidTransition {
    // Handle invalid transition
}

// Check if session has terminated
if session.IsTerminal() {
    // Session is stopped or errored
}
```

### Events

Events provide real-time updates about session activity. All events include a timestamp and session ID.

**Event Accessors:**

To reduce type assertion boilerplate, the `Event` type provides helper methods:

```go
if data, ok := event.StatusChange(); ok {
    fmt.Printf("State changed to: %s\n", data.NewState)
}

if data, ok := event.Output(); ok {
    fmt.Printf("Agent output: %s\n", data.Content)
}
```

**Event Types:**

| Type | Description | Data |
|------|-------------|------|
| `status_change` | Session state changed | `OldState`, `NewState`, `Reason` |
| `output` | Agent produced output | `Content` |
| `metric` | Token/request metrics | `TokensIn`, `TokensOut`, `RequestCount` |
| `error` | Error occurred | `Message`, `Code` |
| `metadata` | Key-value metadata | `Key`, `Value` |

**Factory Functions:**

```go
import "github.com/ricochet1k/orbitmesh/internal/domain"

// Create events with factory functions
event := domain.NewStatusChangeEvent(sessionID, "created", "starting", "user request")
event := domain.NewOutputEvent(sessionID, "Agent output text...")
event := domain.NewMetricEvent(sessionID, tokensIn, tokensOut, requestCount)
event := domain.NewErrorEvent(sessionID, "Connection failed", "CONN_ERR")
event := domain.NewMetadataEvent(sessionID, "model", "claude-3.5-sonnet")
```

### Providers

Providers abstract the underlying agent execution backend. All providers implement a common interface for lifecycle management.

**Interface:**

```go
type Provider interface {
    Start(ctx context.Context, config Config) error
    Stop(ctx context.Context) error
    Pause(ctx context.Context) error
    Resume(ctx context.Context) error
    Kill() error
    Status() Status
    Events() <-chan domain.Event
}
```

**Configuration:**

```go
config := session.Config{
    ProviderType: "gemini",
    WorkingDir:   "/path/to/project",
    Environment:  map[string]string{"GOOGLE_API_KEY": "..."},
    SystemPrompt: "You are a helpful assistant...",
    MCPServers: []provider.MCPServerConfig{
        {
            Name:    "strandyard",
            Command: "strand",
            Args:    []string{"mcp-serve"},
        },
    },
    Custom: map[string]any{
        "model": "gemini-2.5-flash",
    },
}
```

**Status:**

```go
status := session.Status()
fmt.Printf("State: %s\n", status.State)
fmt.Printf("Current task: %s\n", status.CurrentTask)
fmt.Printf("Tokens used: %d in, %d out\n", status.Metrics.TokensIn, status.Metrics.TokensOut)
```

### ADK Provider (Native)

The ADK (Agent Development Kit) provider uses Google's official ADK for Go to run agents. It supports:

- **Gemini models** via `google.golang.org/adk/model/gemini`
- **MCP tools** via `google.golang.org/adk/tool/mcptoolset`
- **Streaming responses** with real-time token metrics
- **Pause/Resume** with conditional variable blocking

**Usage:**

```go
import "github.com/ricochet1k/orbitmesh/internal/provider/native"

p := native.NewADKProvider("session-123", native.ADKConfig{
    APIKey: os.Getenv("GOOGLE_API_KEY"),
    Model:  "gemini-2.5-flash",
})

err := p.Start(ctx, session.Config{
    WorkingDir:   "/path/to/project",
    SystemPrompt: "You are a helpful assistant.",
    MCPServers: []provider.MCPServerConfig{
        {Name: "strandyard", Command: "strand", Args: []string{"mcp-serve"}},
    },
})

// Run a prompt
err = p.RunPrompt(ctx, "What files are in this directory?")

// Pause execution
err = p.Pause(ctx)

// Resume execution
err = p.Resume(ctx)

// Stop the provider
err = p.Stop(ctx)
```

### PTY Provider

The PTY provider runs CLI-based AI agents (like `claude`) within a pseudo-terminal. This allows OrbitMesh to monitor and control tools that expect a real terminal environment.

**Key Features:**
- **Terminal Emulation**: Uses `creack/pty` to start processes with a linked TTY.
- **Status Extraction**: Implements `StatusExtractor` to parse terminal output and identify the agent's current task using regex or fixed-position matching.
- **Signal Control**: Implements `Pause` and `Resume` by sending `SIGTSTP` and `SIGCONT` signals to the child process.
- **Circuit Breaker**: Automatically enters a cooldown period after repeated process failures.

**Usage:**
```go
import "github.com/ricochet1k/orbitmesh/internal/provider/pty"

// Create a Claude-specific PTY provider
p := pty.NewClaudePTYProvider("session-123")

// Start the provider with a CLI command
err := p.Start(ctx, session.Config{
	WorkingDir: "/path/to/project",
	Custom: map[string]any{
		"command": "claude",
		"args":    []string{"--resume"},
	},
})
```

### Service Layer

The service layer coordinates session lifecycle management and event broadcasting across multiple sessions.

#### AgentExecutor

`AgentExecutor` is the central orchestrator for all agent sessions. It manages the registry of active sessions and handles high-level operations.

**Responsibilities:**
- Starting and stopping agent sessions.
- Managing session persistence via the storage layer.
- Running background monitor goroutines for each session.
- Handling graceful shutdown of all active agents.

**Usage:**
```go
import "github.com/ricochet1k/orbitmesh/internal/service"

// Create the executor
executor := service.NewAgentExecutor(providerFactory, storage, broadcaster)

// Start a new session
session, err := executor.StartSession(ctx, "session-123", config)

// Stop a session
err = executor.StopSession(ctx, "session-123")
```

#### EventBroadcaster

`EventBroadcaster` manages the fan-out of events from agent providers to multiple SSE clients.

**Responsibilities:**
- Subscribing multiple listeners to a single session's event stream.
- Thread-safe management of subscriber channels.
- Buffering events to prevent slow consumers from blocking the provider.

**Usage:**
```go
// Subscribe to events for a specific session
sub := broadcaster.Subscribe("session-123")
defer broadcaster.Unsubscribe("session-123", sub)

for event := range sub {
    // Process event
}
```

### Storage

Sessions are persisted to disk as JSON files with atomic writes to prevent corruption.

**Location:**

```
~/.orbitmesh/
└── sessions/
    ├── session-123.json
    └── session-456.json
```

**Interface:**

```go
type Storage interface {
    Save(session *domain.Session) error
    Load(id string) (*domain.Session, error)
    Delete(id string) error
    List() ([]*domain.Session, error)
}
```

**Usage:**

```go
import "github.com/ricochet1k/orbitmesh/internal/storage"

// Create storage with default location (~/.orbitmesh)
store, err := storage.NewJSONFileStorage(storage.DefaultBaseDir())

// Save a session
err = store.Save(session)

// Load a session by ID
session, err := store.Load("session-123")
if err == storage.ErrSessionNotFound {
    // Handle not found
}

// List all sessions
sessions, err := store.List()

// Delete a session
err = store.Delete("session-123")
```

**Atomic & Durable Writes:**

The storage layer ensures data integrity and durability through several mechanisms:

1. **Snapshots**: Before saving, a `Session.Snapshot()` is taken to ensure a consistent, point-in-time view of the session without holding long-lived locks.
2. **Atomic Rename**: Data is written to a unique temporary file (`session.id.*.tmp`) and then renamed to the target path.
3. **Durability**: Each write includes an `f.Sync()` call to flush data to physical storage before the rename, and the parent directory is synced after the rename to ensure it is durable.
4. **Error Resilience**: The `List()` method surfaces aggregated errors if some session files are corrupted, while still returning all successfully loaded sessions.

**Security Hardening:**

Several security measures are in place to protect session data and the host system:

- **Session ID Validation**: All session IDs are validated against a strict regex (`[A-Za-z0-9_-]{1,64}`) to prevent path traversal and injection attacks.
- **Secure Permissions**: The sessions directory is created with `0700` permissions, and session files are saved with `0600` permissions, restricting access to the owner only.
- **Symlink Protection**: The storage layer explicitly refuses to operate on symlinked files to prevent out-of-bounds access.
- **Resource Limits**: A maximum file size limit (10MB) is enforced during loading to prevent memory-based Denial of Service (DoS) attacks.

## API

The backend provides a RESTful API for session management and real-time event streaming.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/sessions` | List all sessions |
| `POST` | `/api/v1/sessions` | Create a new session |
| `GET` | `/api/v1/sessions/{id}` | Get session details |
| `DELETE` | `/api/v1/sessions/{id}` | Stop and remove a session |
| `POST` | `/api/v1/sessions/{id}/pause` | Pause a running session |
| `POST` | `/api/v1/sessions/{id}/resume` | Resume a paused session |
| `GET` | `/api/v1/sessions/{id}/events` | Stream real-time events (SSE) |

### Session Creation

**Request:**
```json
{
  "provider_type": "adk",
  "working_dir": "/path/to/project",
  "system_prompt": "You are a helpful assistant.",
  "mcp_servers": [
    { "name": "strandyard", "command": "strand", "args": ["mcp-serve"] }
  ],
  "custom": {
    "model": "gemini-2.0-flash"
  }
}
```

### Event Streaming (SSE)

Real-time updates are streamed using Server-Sent Events. Clients should listen on `/api/v1/sessions/{id}/events`.

**Event Format:**
```
event: status_change
data: {"old_state": "running", "new_state": "paused", "reason": "user requested pause"}

event: output
data: {"content": "Processing data..."}
```

## Data Flow

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│   Provider   │─────▶│   Session    │─────▶│   Storage    │
│  (execution) │      │ (state mgmt) │      │ (persistence)│
└──────────────┘      └──────────────┘      └──────────────┘
       │                     │
       ▼                     ▼
┌──────────────┐      ┌──────────────┐
│    Events    │      │ Transitions  │
│  (streaming) │      │   (history)  │
└──────────────┘      └──────────────┘
```

## Error Handling

- **Invalid state transitions** return `domain.ErrInvalidTransition`
- **Missing sessions** return `storage.ErrSessionNotFound`
- **Write failures** return `storage.ErrStorageWrite` (wrapping underlying error)

## Thread Safety

- `Session` uses a `sync.RWMutex` for concurrent access
- `JSONFileStorage` uses a `sync.RWMutex` for file operations
- Event channels are safe for concurrent reads
