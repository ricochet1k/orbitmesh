# Session Persistence Implementation Plan

## Overview

Implement session persistence to allow resuming long-running ACP agent sessions across restarts, process crashes, or planned downtime. This is critical for multi-day coding projects or maintaining conversation context.

## Use Cases

1. **Long-Running Projects**
   - Agent working on complex refactoring over multiple days
   - Maintain conversation history and context
   - Resume after OrbitMesh restart

2. **Crash Recovery**
   - Recover from OrbitMesh crashes
   - Recover from agent crashes
   - Minimize lost work

3. **Planned Maintenance**
   - Save state before deployment
   - Resume sessions after updates
   - Zero session loss during restarts

## Current State

- Sessions are ephemeral (lost on restart)
- No `LoadSession` support in ACP provider
- ACP SDK supports `LoadSession` via `AgentLoader` interface

## Architecture

### Components

```
┌──────────────┐
│   Storage    │ (Filesystem, DB, or Object Store)
└──────┬───────┘
       │
┌──────▼───────┐
│   Snapshot   │ (Session state serialization)
│   Manager    │
└──────┬───────┘
       │
┌──────▼───────┐
│ ACP Provider │ (Load/Save hooks)
└──────────────┘
```

### Data Model

**Session Snapshot:**
```go
type SessionSnapshot struct {
    // Metadata
    SessionID    string
    ProviderType string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Version      int  // Schema version

    // Configuration
    Config       session.Config

    // ACP State
    ACPSessionID string  // The agent's session ID
    Messages     []Message  // Conversation history

    // Provider State
    CurrentTask  string
    Metrics      session.Metrics

    // Optional: Tool state, file modifications, etc.
    Metadata     map[string]any
}

type Message struct {
    Role      string  // "user" or "assistant"
    Content   []ContentBlock
    Timestamp time.Time
}
```

## Implementation Steps

### Phase 1: Snapshot Storage

**File:** `internal/session/storage/snapshot.go`

```go
package storage

type SnapshotStore interface {
    Save(snapshot *SessionSnapshot) error
    Load(sessionID string) (*SessionSnapshot, error)
    Delete(sessionID string) error
    List() ([]SessionSnapshot, error)
}

// Filesystem implementation
type FilesystemStore struct {
    baseDir string
}

func NewFilesystemStore(baseDir string) *FilesystemStore
func (fs *FilesystemStore) Save(snapshot *SessionSnapshot) error
func (fs *FilesystemStore) Load(sessionID string) (*SessionSnapshot, error)
```

**Storage Location:**
```
~/.orbitmesh/sessions/
  ├── session-abc123.json      # Session snapshot
  ├── session-def456.json
  └── index.json                # Metadata index
```

**Serialization:**
- JSON for human readability
- Gzip compression for large snapshots
- Schema versioning for migrations

### Phase 2: Snapshot Manager

**File:** `internal/session/snapshot_manager.go`

```go
package session

type SnapshotManager struct {
    store    storage.SnapshotStore
    interval time.Duration  // Auto-save interval
    mu       sync.RWMutex
}

func NewSnapshotManager(store storage.SnapshotStore) *SnapshotManager

// Manual snapshot
func (sm *SnapshotManager) Snapshot(session Session) error

// Auto-snapshot (background goroutine)
func (sm *SnapshotManager) StartAutoSnapshot(session Session, interval time.Duration)
func (sm *SnapshotManager) StopAutoSnapshot(sessionID string)

// Restore
func (sm *SnapshotManager) Restore(sessionID string) (*SessionSnapshot, error)
```

### Phase 3: ACP Provider Integration

**File:** `session.go` (modifications)

**Save State:**

```go
// Add to Session struct
type Session struct {
    // ... existing fields ...

    messageHistory []Message  // Track conversation
    snapshotMgr    *session.SnapshotManager
}

// Implement Snapshottable interface
func (s *Session) Snapshot() (*session.SessionSnapshot, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    return &session.SessionSnapshot{
        SessionID:    s.sessionID,
        ProviderType: "acp",
        CreatedAt:    s.createdAt,
        UpdatedAt:    time.Now(),
        Version:      1,
        Config:       s.sessionConfig,
        ACPSessionID: *s.acpSessionID,
        Messages:     s.messageHistory,
        CurrentTask:  s.state.Status().CurrentTask,
        Metrics:      s.state.Status().Metrics,
    }, nil
}

// Track messages
func (s *Session) SendInput(ctx context.Context, input string) error {
    // ... existing code ...

    // Track user message
    s.messageHistory = append(s.messageHistory, Message{
        Role:      "user",
        Content:   []ContentBlock{{Text: &ContentBlockText{Text: input}}},
        Timestamp: time.Now(),
    })

    // Auto-snapshot after significant events
    if s.snapshotMgr != nil {
        go s.snapshotMgr.Snapshot(s)
    }

    return s.inputBuffer.Send(ctx, input)
}

func (s *Session) handlePromptResponse(resp acpsdk.PromptResponse) {
    // ... existing code ...

    // Track assistant message (would need response content)
    s.messageHistory = append(s.messageHistory, Message{
        Role:      "assistant",
        Content:   /* extract from response */,
        Timestamp: time.Now(),
    })
}
```

**Load State (New Function):**

```go
// LoadSession restores a session from snapshot
func LoadSession(sessionID string, providerConfig Config, snapshotMgr *session.SnapshotManager) (*Session, error) {
    // Load snapshot
    snapshot, err := snapshotMgr.Restore(sessionID)
    if err != nil {
        return nil, err
    }

    // Create session from snapshot
    sess := &Session{
        sessionID:      sessionID,
        state:          native.NewProviderState(),
        events:         native.NewEventAdapter(sessionID, 100),
        providerConfig: providerConfig,
        sessionConfig:  snapshot.Config,
        inputBuffer:    buffer.NewInputBuffer(10),
        circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
        messageHistory: snapshot.Messages,
        snapshotMgr:    snapshotMgr,
    }

    // Restore state
    sess.state.SetCurrentTask(snapshot.CurrentTask)
    // ... restore metrics, etc ...

    return sess, nil
}

// Start session and resume ACP session
func (s *Session) StartWithResume(ctx context.Context, snapshot *session.SessionSnapshot) error {
    // Start process normally
    if err := s.Start(ctx, snapshot.Config); err != nil {
        return err
    }

    // Tell ACP agent to load the session
    loadReq := acpsdk.LoadSessionRequest{
        SessionId: acpsdk.SessionId(snapshot.ACPSessionID),
    }

    _, err := s.conn.LoadSession(s.ctx, loadReq)
    return err
}
```

### Phase 4: Provider Factory Integration

**File:** `internal/provider/provider.go` (or factory)

```go
type ResumableProvider interface {
    Provider
    LoadSession(sessionID string, snapshot *session.SessionSnapshot) (session.Session, error)
}

// Factory method
func CreateOrResumeSession(providerType, sessionID string, config session.Config, snapshotMgr *session.SnapshotManager) (session.Session, error) {
    // Check if snapshot exists
    if snapshot, err := snapshotMgr.Restore(sessionID); err == nil {
        // Resume existing session
        if provider, ok := getProvider(providerType).(ResumableProvider); ok {
            return provider.LoadSession(sessionID, snapshot)
        }
    }

    // Create new session
    return getProvider(providerType).CreateSession(sessionID, config)
}
```

### Phase 5: Auto-Snapshot Configuration

**Configuration:**

```go
type Config struct {
    // ... existing fields ...

    // Persistence configuration
    EnablePersistence      bool          // Default: false
    SnapshotInterval       time.Duration // Default: 5 minutes
    SnapshotOnExit         bool          // Default: true
    SnapshotStoragePath    string        // Default: ~/.orbitmesh/sessions
    MaxSnapshotsPerSession int           // Default: 10 (keep last N)
}
```

**Auto-Snapshot:**

```go
func (s *Session) Start(ctx context.Context, config session.Config) error {
    // ... existing code ...

    // Start auto-snapshot if enabled
    if config.Custom["enable_persistence"] == true {
        interval := config.Custom["snapshot_interval"].(time.Duration)
        s.snapshotMgr.StartAutoSnapshot(s, interval)
    }

    return nil
}

func (s *Session) Stop(ctx context.Context) error {
    // Snapshot on exit if configured
    if s.snapshotMgr != nil {
        _ = s.snapshotMgr.Snapshot(s)
        s.snapshotMgr.StopAutoSnapshot(s.sessionID)
    }

    // ... existing code ...
}
```

## ACP Protocol Considerations

**Agent Support Required:**

The agent must implement `AgentLoader` interface:

```go
type AgentLoader interface {
    LoadSession(ctx context.Context, params LoadSessionRequest) (LoadSessionResponse, error)
}
```

**LoadSessionRequest:**
```go
type LoadSessionRequest struct {
    SessionId SessionId  // The session ID to resume
}
```

**Not all agents support this!**
- Check agent capabilities during `Initialize()`
- Fall back to new session if not supported
- Emit warning if persistence requested but not available

## Error Handling

```go
var (
    ErrSnapshotNotFound       = errors.New("snapshot not found")
    ErrInvalidSnapshotVersion = errors.New("snapshot version incompatible")
    ErrAgentNoLoadSupport     = errors.New("agent does not support session loading")
    ErrSnapshotCorrupted      = errors.New("snapshot data corrupted")
)
```

## Testing

**Unit Tests:**
- Snapshot serialization/deserialization
- Snapshot storage (save/load/delete)
- Version compatibility

**Integration Tests:**
- Create session → snapshot → restore
- Auto-snapshot intervals
- Snapshot on crash recovery
- Agent compatibility checks

**Test with `gemini --experimental-acp`:**
- May or may not support LoadSession (check capabilities)

## Security & Privacy

1. **Data Sensitivity**
   - Snapshots contain conversation history
   - May include API keys in environment
   - File permissions: 0600 (owner read/write only)

2. **Encryption** (Optional Enhancement)
   - Encrypt snapshots at rest
   - Use user-provided key or system keychain

3. **Retention Policy**
   - Auto-delete old snapshots
   - Configurable retention period

## Migration & Compatibility

**Schema Versioning:**

```go
type SnapshotV1 struct {
    Version int `json:"version"`  // Always 1
    // ... V1 fields ...
}

type SnapshotV2 struct {
    Version int `json:"version"`  // Always 2
    // ... V2 fields ...
}

func Migrate(data []byte) (*SessionSnapshot, error) {
    var version struct {
        Version int `json:"version"`
    }
    json.Unmarshal(data, &version)

    switch version.Version {
    case 1:
        return migrateV1ToV2(data)
    case 2:
        return loadV2(data)
    default:
        return nil, ErrInvalidSnapshotVersion
    }
}
```

## Monitoring & Observability

**Events:**
```go
// Snapshot created
events.EmitMetadata("snapshot_created", map[string]any{
    "session_id": sessionID,
    "size_bytes": size,
})

// Session restored
events.EmitMetadata("session_restored", map[string]any{
    "session_id": sessionID,
    "age":        age,
})

// Snapshot error
events.EmitError("snapshot_failed", "SNAPSHOT_ERROR")
```

**Metrics:**
- Snapshot success/failure rate
- Snapshot size distribution
- Restore success rate
- Time to snapshot
- Time to restore

## Estimated Effort

- **Snapshot Storage**: 3-4 hours
- **Snapshot Manager**: 3-4 hours
- **ACP Provider Integration**: 4-5 hours
- **Testing**: 3-4 hours
- **Documentation**: 1-2 hours

**Total**: ~14-19 hours

## Success Criteria

- [ ] Can save session snapshot to disk
- [ ] Can restore session from snapshot
- [ ] Auto-snapshot works on interval
- [ ] Snapshot on graceful shutdown
- [ ] Message history preserved
- [ ] Metrics and state restored correctly
- [ ] Agent LoadSession integration works
- [ ] Handles agents without LoadSession support
- [ ] Schema versioning works
- [ ] All tests passing

## Future Enhancements

- **Cloud Storage**: S3, GCS for multi-machine access
- **Incremental Snapshots**: Only save deltas
- **Compression**: Reduce snapshot size
- **Encryption**: Protect sensitive data
- **Snapshot Browser**: UI to view/manage snapshots
- **Cross-Provider**: Share snapshots across provider types
