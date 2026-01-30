# Backend Agent Execution Engine - Design Document

**Status**: ✓ Approved
**Date**: 2026-01-30
**Owner Decision**: Alternative 1 - Layered Architecture
**Target Timeline**: 5 weeks (Urgent)

## Architecture Overview

Layered architecture for multi-provider agent orchestration with real-time monitoring and interactive control.

### Architecture Diagram

```
┌──────────────────────────────────────────────────────┐
│                   Frontend Dashboard                 │
│          (Real-time agent status + controls)         │
└─────────────────────────┬──────────────────────────┬─┘
                          │ SSE                HTTP  │
                          ▼                         ▼
┌──────────────────────────────────────────────────────┐
│                    API Layer                         │
│  ┌────────────────────────────────────────────────┐  │
│  │ HTTP Handlers: Sessions CRUD                  │  │
│  │ SSE Handler: Real-time event streaming       │  │
│  │ WebSocket Handler: Interactive control (opt) │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│                  Service Layer                       │
│  ┌────────────────────────────────────────────────┐  │
│  │ AgentExecutor                                 │  │
│  │  - Session lifecycle management              │  │
│  │  - Provider orchestration                    │  │
│  │  - Health checks & fault tolerance           │  │
│  └────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────┐  │
│  │ EventBroadcaster                              │  │
│  │  - Collects events from providers            │  │
│  │  - Fans out to multiple SSE clients          │  │
│  └────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────┐  │
│  │ SessionManager                                │  │
│  │  - Session registry (map[id]*Session)       │  │
│  │  - State transitions with validation         │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────┬───────────────────────────────┘
                       │
         ┌─────────────┼─────────────┐
         ▼             ▼             ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Native SDK   │  │ PTY Provider │  │   Storage    │
│  - Gemini    │  │  - claude    │  │   - JSON     │
│  - Others    │  │  - codex     │  │   - File I/O │
│              │  │  - amp       │  │              │
└──────────────┘  └──────────────┘  └──────────────┘
        │              │                    │
        └──────────────┴────────────────────┘
                       │
        ~/.orbitmesh/
         ├── sessions/
         ├── agents/
         └── config.json
```

## Core Interfaces

### Provider Interface

All providers (native SDK or PTY-based) implement this interface:

```go
// Provider represents any agent execution provider
type Provider interface {
    // Lifecycle management
    Start(ctx context.Context, config ProviderConfig) error
    Stop(ctx context.Context) error
    Pause(ctx context.Context) error
    Resume(ctx context.Context) error
    Kill() error

    // State access
    Status() ProviderStatus

    // Event stream
    Events() <-chan Event
}

// Configuration
type ProviderConfig struct {
    ProviderType string                 // "gemini", "claude-code", "codex"
    WorkingDir   string
    Environment  map[string]string
    SystemPrompt string                 // Injected/augmented system prompt
    MCPServers   []MCPServerConfig      // StrandYard, OrbitMesh
    Custom       map[string]interface{} // Provider-specific settings
}

// Status
type ProviderStatus struct {
    State       ProviderState  // Starting, Running, Paused, Stopped, Error
    CurrentTask string         // From StrandYard if available
    Output      string         // Latest output snippet
    Error       error
    Metrics     ProviderMetrics
}

// Events
type Event struct {
    Type      EventType     // StatusChange, Output, Metric, Error
    Timestamp time.Time
    SessionID string
    Data      interface{}
}
```

### Session State Machine

```
Created
  ↓
Starting → Error
  ↓        ↑
Running ← ┘
  ↕
Paused
  ↓
Stopping → Error
  ↓        ↑
Stopped ← ┘
```

**State Definitions**:

| State | Meaning | Transitions To |
|-------|---------|----------------|
| Created | Session initialized, not started | Starting |
| Starting | Provider initialization in progress | Running, Error |
| Running | Provider actively executing agent | Paused, Stopping, Error |
| Paused | Provider paused, waiting to resume | Running, Stopping |
| Stopping | Provider shutdown in progress | Stopped, Error |
| Stopped | Terminal state, provider stopped | (none) |
| Error | Terminal state, unrecoverable error | Stopping |

**Validation**:
- State transitions enforced by state machine
- Only allowed transitions permitted
- Each transition logged with timestamp and reason

### Event Types

```go
type EventType int

const (
    EventTypeStatusChange EventType = iota  // Provider state changed
    EventTypeOutput                         // Provider produced output
    EventTypeMetric                         // Metrics (token usage, etc.)
    EventTypeError                          // Error occurred
    EventTypeMetadata                       // Session metadata changed
)
```

## Service Layer Architecture

### AgentExecutor

Orchestrates session lifecycle and provider management:

```go
type AgentExecutor struct {
    sessions    map[string]*Session    // Active sessions
    storage     SessionStorage         // Persistence layer
    broadcaster EventBroadcaster       // Event fan-out
    mu          sync.RWMutex
    ctx         context.Context
}

// Primary methods
func (e *AgentExecutor) StartSession(ctx context.Context, req StartSessionRequest) (string, error)
func (e *AgentExecutor) StopSession(ctx context.Context, sessionID string) error
func (e *AgentExecutor) PauseSession(ctx context.Context, sessionID string) error
func (e *AgentExecutor) ResumeSession(ctx context.Context, sessionID string) error
func (e *AgentExecutor) GetSession(ctx context.Context, sessionID string) (*Session, error)
func (e *AgentExecutor) ListSessions(ctx context.Context) ([]*Session, error)
```

**Responsibilities**:
- Create/destroy sessions
- Manage provider lifecycle
- Handle state transitions
- Coordinate with storage
- Emit events to broadcaster
- Health checks and fault tolerance

### EventBroadcaster

Manages real-time event delivery to connected clients:

```go
type EventBroadcaster struct {
    subscribers map[string]map[*Client]struct{}  // sessionID -> clients
    events      chan Event
    mu          sync.RWMutex
}

func (eb *EventBroadcaster) Subscribe(sessionID string) <-chan Event
func (eb *EventBroadcaster) Unsubscribe(sessionID string, client *Client)
func (eb *EventBroadcaster) Broadcast(event Event)
```

**Responsibilities**:
- Collect events from all providers
- Maintain subscription list per session
- Fan-out events to subscribed clients
- Clean up disconnected clients
- Buffer events to prevent loss

## Package Structure

```
backend/internal/
├── api/
│   ├── handler.go        # REST API handlers
│   ├── sse.go           # Server-Sent Events
│   └── types.go         # API request/response types
├── domain/
│   ├── session.go       # Session & state machine
│   ├── event.go         # Event definitions
│   └── errors.go        # Domain error types
├── provider/
│   ├── provider.go      # Interface definition
│   ├── factory.go       # Provider factory/registry
│   ├── native/
│   │   ├── gemini.go    # Google ADK provider
│   │   └── adapter.go   # Native provider utilities
│   └── pty/
│       ├── pty.go       # PTY provider base
│       ├── claude.go    # claude-code specific
│       ├── codex.go     # codex specific
│       ├── extractor.go # Status extraction
│       └── amp.go       # amp specific
├── service/
│   ├── executor.go      # AgentExecutor
│   ├── events.go        # EventBroadcaster
│   └── health.go        # Health checks
├── storage/
│   ├── storage.go       # Storage interface
│   └── session.go       # Session persistence
└── metrics/
    └── collector.go     # Metrics collection
```

## File-Based Storage Schema

### Session File Structure

`~/.orbitmesh/sessions/<session-id>.json`

```json
{
  "id": "session-abc123",
  "state": "running",
  "provider_type": "claude-code",
  "config": {
    "working_dir": "/path/to/project",
    "environment": {},
    "system_prompt": "..."
  },
  "created_at": "2026-01-30T14:30:00Z",
  "updated_at": "2026-01-30T14:35:45Z",
  "history": [
    {
      "from": "created",
      "to": "starting",
      "timestamp": "2026-01-30T14:30:01Z",
      "reason": "user initiated"
    },
    {
      "from": "starting",
      "to": "running",
      "timestamp": "2026-01-30T14:30:05Z",
      "reason": "provider started"
    }
  ],
  "metrics": {
    "tokens_used": 1500,
    "cost_usd": 0.045
  }
}
```

### Atomic Write Pattern

To prevent corruption with concurrent writes:

1. Write to `<file>.tmp`
2. Rename `.tmp` → `<file>` (atomic on POSIX)
3. Delete `.tmp` if rename fails

## Fault Tolerance Strategy

### Simple but Effective

**What We Include**:

1. **Context Timeouts**: All provider operations have 5-minute timeout
2. **Panic Recovery**: Goroutines recover from panics, log error, mark session as error state
3. **Health Checks**: Periodic polling of provider.Status() every 30 seconds
4. **Circuit Breaker**: PTY providers stop retrying after 3 failures for 30 seconds
5. **Graceful Shutdown**: SIGINT/SIGTERM triggers context cancellation
6. **Atomic Writes**: Session storage uses temp file + rename pattern

**What We Avoid**:

1. ❌ Full supervision trees (too complex)
2. ❌ Exponential backoff (simple fail-fast approach)
3. ❌ Distributed tracing (structured logging instead)
4. ❌ Complex event replay
5. ❌ Process isolation (all in-process for now)

### Health Check Implementation

```go
func (e *AgentExecutor) runHealthChecks(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        e.mu.RLock()
        for id, session := range e.sessions {
            status := session.Provider.Status()
            if status.State == ProviderStateError {
                // Mark session as error, stop attempting
                e.mu.RUnlock()
                e.StopSession(ctx, id)
                e.mu.RLock()
            }
        }
        e.mu.RUnlock()
    }
}
```

### Panic Recovery in Session Goroutine

```go
func (e *AgentExecutor) monitorSession(session *Session) {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("Provider panic: %v", r)
            session.TransitionTo(SessionStateError, "provider panic")
            e.broadcaster.Broadcast(Event{
                Type: EventTypeError,
                Data: fmt.Sprintf("Provider panic: %v", r),
            })
        }
    }()

    // ... rest of monitoring logic
}
```

## Event Flow

### Session Creation

```
API Request
  ↓
Handler validates input
  ↓
Executor.StartSession()
  ↓
Create Session (Created state)
  ↓
Save to storage
  ↓
Transition to Starting state
  ↓
Emit StatusChange event
  ↓
Provider.Start()
  ↓
Provider transitions to Running (after setup)
  ↓
Emit StatusChange event
  ↓
EventBroadcaster fans out to SSE clients
  ↓
Frontend updates dashboard in real-time
```

### Provider Output

```
Provider.Events() channel
  ↓
Service detects EventTypeOutput
  ↓
Emit to EventBroadcaster
  ↓
Fan-out to subscribed SSE clients
  ↓
Frontend displays in session viewer
```

## PTY Provider Status Extraction

Three strategies for determining CLI tool status from terminal output:

### 1. Position-Based Extraction

Fixed coordinates for known tools:

```go
type PositionExtractor struct {
    StatusRow int  // Terminal row (0-based)
    StatusCol int  // Terminal column (0-based)
    Width     int  // Status text width
}
```

**Example**: claude-code shows status at row 0, col 50, width 30

### 2. Regex-Based Extraction

Pattern matching for known tool outputs:

```go
type RegexExtractor struct {
    Patterns map[ProviderState]*regexp.Regexp
}

// Example patterns
var claudePatterns = map[ProviderState]*regexp.Regexp{
    ProviderStateRunning: regexp.MustCompile(`\[running\]`),
    ProviderStatePaused:  regexp.MustCompile(`\[paused\]`),
    ProviderStateStopped: regexp.MustCompile(`\[stopped\]`),
}
```

### 3. AI-Assisted Extraction (Fallback)

LLM analyzes screen buffer for unknown tools:

```go
type AIExtractor struct {
    client *llm.Client  // Small, fast model
    cache  map[string]ProviderStatus
}

// Sends screen buffer to LLM with prompt:
// "Extract agent status from this terminal output..."
```

**Preference Order**: Position → Regex → AI

## API Endpoints

### Session Management

```
POST   /api/sessions
GET    /api/sessions/:id
DELETE /api/sessions/:id
POST   /api/sessions/:id/pause
POST   /api/sessions/:id/resume
GET    /api/sessions/:id/events    (SSE)
```

### Response Types

```go
type SessionResponse struct {
    ID          string
    State       string
    ProviderType string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CurrentTask string
    Output      string
}

type EventResponse struct {
    Type      string
    Timestamp time.Time
    SessionID string
    Data      interface{}
}
```

## Testing Strategy

### Unit Tests

- Provider interface compliance
- Session state machine transitions
- Storage read/write
- Event broadcaster logic

### Integration Tests

- Full session lifecycle (create → run → pause → resume → stop)
- Multiple concurrent sessions
- Provider switching

### PTY Tests

- Mock terminal output
- Status extraction (position, regex, AI)
- CLI tool-specific behaviors

### Load Tests

- 10 concurrent agents
- Verify no resource leaks
- Measure latency

## Scalability Path to 100+ Agents

When load exceeds <10 concurrent agents, migrate to Event-Driven Architecture (Alternative 2):

1. **Extract Event Bus**: Central channel-based message router
2. **Add Worker Pool**: Pool of worker goroutines processing events
3. **Remove Synchronous Calls**: Replace direct function calls with channel sends
4. **Maintain Provider Interface**: No changes to provider.Provider

**Migration Effort**: 2-3 weeks with minimal API changes

## Dependencies

### Go Libraries

- `github.com/creack/pty` - PTY management (MIT license)
- `github.com/google/generative-ai-go` - Gemini SDK
- Standard library only for core functionality

### No External Database

- All state in JSON files
- StrandYard handles task metadata via MCP
- No Postgres, Redis, or other services

## Success Criteria

- ✅ 4-5 week development timeline (all phases)
- ✅ Support <10 concurrent agents smoothly
- ✅ >80% test coverage
- ✅ Provider interface extensible for future additions
- ✅ Simple fault tolerance (no complexity)
- ✅ Real-time SSE updates to frontend
- ✅ Session pause/resume working
- ✅ Google ADK + PTY providers functional

---

## Implementation Phases

See [Implementation Plan](#implementation-plan) in design document or refer to strand subtasks for detailed weekly breakdown.

**Next**: Create strand subtasks for each implementation phase
