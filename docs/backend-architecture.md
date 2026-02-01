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
import "github.com/orbitmesh/orbitmesh/internal/domain"

// Create a new session
session := domain.NewSession("session-123", "claude-code", "/path/to/project")

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
import "github.com/orbitmesh/orbitmesh/internal/domain"

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
config := provider.Config{
    ProviderType: "claude-code",
    WorkingDir:   "/path/to/project",
    Environment:  map[string]string{"API_KEY": "..."},
    SystemPrompt: "You are a helpful assistant...",
    MCPServers: []provider.MCPServerConfig{
        {
            Name:    "strandyard",
            Command: "strand",
            Args:    []string{"mcp-serve"},
        },
    },
    Custom: map[string]any{
        "model": "claude-3.5-sonnet",
    },
}
```

**Status:**

```go
status := provider.Status()
fmt.Printf("State: %s\n", status.State)
fmt.Printf("Current task: %s\n", status.CurrentTask)
fmt.Printf("Tokens used: %d in, %d out\n", status.Metrics.TokensIn, status.Metrics.TokensOut)
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
import "github.com/orbitmesh/orbitmesh/internal/storage"

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

**Atomic Writes:**

The storage layer uses atomic writes to prevent file corruption:

1. Write to a temporary file (`session.json.tmp`)
2. Rename temporary file to target path
3. Clean up on failure

This ensures sessions are never left in a partial/corrupted state.

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
