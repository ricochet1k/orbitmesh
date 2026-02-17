# ACP Implementation Analysis

## Missing ACP Features

### 1. SessionUpdate Variants (Not Fully Exposed)

**Currently Handled:**
- ✅ `AgentMessageChunk` - Streaming text output
- ✅ `ToolCall` - Tool initiation
- ✅ `ToolCallUpdate` - Tool status changes

**Not Yet Handled:**
- ❌ `UserMessageChunk` - Echo of user's message (useful for confirmation)
- ❌ `AgentThoughtChunk` - Internal reasoning/thinking process
- ❌ `Plan` - Agent's execution plan for complex tasks
- ❌ `AvailableCommandsUpdate` - Dynamic command discovery
- ❌ `CurrentModeUpdate` - Session mode changes

**Impact:** These are rich sources of insight into agent behavior. Missing them means:
- No visibility into agent's reasoning process (thoughts)
- No access to structured planning information
- Can't track mode changes (e.g., normal → planning → execution)
- Can't discover agent capabilities dynamically

**Recommendation:** Add new OrbitMesh event types or enhance metadata structure:
```go
type EventType int
const (
    EventTypeStatusChange EventType = iota
    EventTypeOutput
    EventTypeMetric
    EventTypeError
    EventTypeMetadata
    // New types for richer ACP data:
    EventTypeThought      // Agent reasoning
    EventTypePlan         // Execution plans
    EventTypeToolCall     // Structured tool info
)
```

### 2. Advanced Content Blocks

**Currently Handled:**
- ✅ Text
- ✅ Image (as metadata)

**Not Yet Handled:**
- ❌ ResourceLink - References to external resources
- ❌ ToolResult - Detailed tool execution results
- ❌ Other specialized content types

**Impact:** Can't properly represent rich, multi-modal agent responses.

### 3. Session Management Features

**Not Implemented:**
- ❌ `LoadSession` - Resume from saved state
- ❌ `SetSessionMode` - Switch between modes (normal, planning, etc.)
- ❌ `SetSessionModel` - Change underlying model
- ❌ `Cancel` - Abort in-progress operations
- ❌ `Authenticate` - Handle auth flows

**Impact:** Can't persist sessions, change agent behavior dynamically, or handle auth.

### 4. MCP Server Integration

**Current State:** Empty array in NewSessionRequest

**Missing:**
```go
McpServers: []acpsdk.McpServer{
    {
        Name: "filesystem",
        Transport: /* ... */,
    },
}
```

**Impact:** Can't leverage MCP servers for extended capabilities.

### 5. Token Usage Tracking

**Issue:** We saw `update.Usage` in the example but didn't add handling for it.

**Missing:**
```go
case update.Usage != nil:
    // Track InputTokens, OutputTokens
```

**Impact:** No real-time token tracking during streaming responses.

### 6. Terminal Support (Partial)

**Current State:** Stub implementations return dummy data

**Missing:**
- Actual terminal creation and management
- Command execution
- Output streaming
- Exit code handling

**Impact:** Can't leverage ACP's terminal capabilities for interactive commands.

---

## Reusable Components Across Providers

### 1. Process Management Pattern (HIGH VALUE) ⭐⭐⭐

**Currently Duplicated in:**
- `acp/session.go` (539 lines)
- `claude/claudecode.go` (457 lines)
- `pty/pty.go` (likely similar)

**Common Pattern:**
```go
// Start process
cmd := exec.Command(...)
stdin, _ := cmd.StdinPipe()
stdout, _ := cmd.StdoutPipe()
stderr, _ := cmd.StderrPipe()
cmd.Start()

// Graceful shutdown
cmd.Process.Signal(syscall.SIGTERM)
select {
case <-time.After(5 * time.Second):
    cmd.Process.Kill()
case <-done:
}
```

**Proposed Extraction:**
```go
package provider

type ProcessManager struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.ReadCloser
    stderr io.ReadCloser
    ctx    context.Context
    cancel context.CancelFunc
}

func NewProcessManager(config ProcessConfig) *ProcessManager
func (pm *ProcessManager) Start() error
func (pm *ProcessManager) Stop(timeout time.Duration) error
func (pm *ProcessManager) Kill() error
func (pm *ProcessManager) Stdin() io.WriteCloser
func (pm *ProcessManager) Stdout() io.ReadCloser
func (pm *ProcessManager) Stderr() io.ReadCloser
```

**Benefits:**
- ~100 lines saved per provider
- Consistent shutdown behavior
- Centralized timeout/kill logic
- Easier testing

### 2. Circuit Breaker Pattern (MEDIUM VALUE) ⭐⭐

**Currently Duplicated:**
```go
failureCount    int
cooldownUntil   time.Time

func handleFailure(err error) {
    p.failureCount++
    if p.failureCount >= 3 {
        p.cooldownUntil = time.Now().Add(30 * time.Second)
        p.failureCount = 0
    }
}
```

**Proposed Extraction:**
```go
package provider

type CircuitBreaker struct {
    threshold      int
    cooldown       time.Duration
    failureCount   int
    cooldownUntil  time.Time
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker
func (cb *CircuitBreaker) RecordFailure() bool // returns true if should enter cooldown
func (cb *CircuitBreaker) IsInCooldown() bool
func (cb *CircuitBreaker) Reset()
```

### 3. Pause/Resume via Input Buffering (MEDIUM VALUE) ⭐⭐

**Currently Implemented in:** ACP provider

**Pattern:**
```go
paused        bool
pausedInputs  []string
inputQueue    chan string

func Pause() {
    s.paused = true
}

func Resume() {
    s.paused = false
    for _, input := range s.pausedInputs {
        s.inputQueue <- input
    }
    s.pausedInputs = nil
}

func SendInput(input string) {
    if s.paused {
        s.pausedInputs = append(s.pausedInputs, input)
    } else {
        s.inputQueue <- input
    }
}
```

**Proposed:**
```go
package provider

type InputBuffer struct {
    queue   chan string
    paused  atomic.Bool
    buffer  []string
    mu      sync.Mutex
}

func (ib *InputBuffer) Send(input string)
func (ib *InputBuffer) Pause()
func (ib *InputBuffer) Resume()
func (ib *InputBuffer) Receive() <-chan string
```

**Could be added to:** Claude provider, PTY providers

### 4. File Operations Helper (LOW VALUE) ⭐

**Currently in:** ACP adapter

**Pattern:**
- Resolve relative paths to absolute
- Create parent directories
- Handle line ranges for file reading

**Proposed:**
```go
package provider

type FileHelper struct {
    workingDir string
}

func (fh *FileHelper) ResolvePath(path string) (string, error)
func (fh *FileHelper) ReadFile(path string, lineStart, lineLimit *int) (string, error)
func (fh *FileHelper) WriteFile(path string, content string) error
```

**Benefit:** Other providers might need file operations (e.g., Claude Code)

### 5. Event Adapter Enhancements (MEDIUM VALUE) ⭐⭐

**Currently:** `native.EventAdapter` is already shared ✅

**Potential Enhancement:** Add convenience methods for common patterns:

```go
// Current
a.events.EmitMetadata("tool_call", map[string]any{
    "id":     id,
    "name":   name,
    "status": status,
})

// Proposed
a.events.EmitToolCall(ToolCallEvent{
    ID:     id,
    Name:   name,
    Status: status,
})
```

**Benefits:**
- Type safety
- Consistent metadata keys
- Easier to query/filter events

### 6. Goroutine Lifecycle Management (LOW VALUE) ⭐

**Pattern:**
```go
wg     sync.WaitGroup

s.wg.Add(3)
go s.processStdout()
go s.processStderr()
go s.processInput()

// Later
s.wg.Wait()
```

**Already pretty simple, but could be:**
```go
type GoroutineManager struct {
    wg  sync.WaitGroup
    ctx context.Context
}

func (gm *GoroutineManager) Go(fn func())
func (gm *GoroutineManager) Wait()
```

---

## Recommendations by Priority

### High Priority (Do First)

1. **Extract ProcessManager** - Immediate code reuse, affects all process-based providers
2. **Add SessionUpdate variants** - Unlock ACP's full potential
3. **Implement token usage tracking** - Critical for cost monitoring

### Medium Priority (Do Soon)

4. **Extract InputBuffer** - Enable pause/resume in Claude provider
5. **Extract CircuitBreaker** - Consistent failure handling
6. **Add structured event types** - Better than generic metadata

### Low Priority (Nice to Have)

7. **FileHelper utility** - Only if multiple providers need it
8. **GoroutineManager** - Current pattern is fine
9. **Terminal support** - Wait for real use case

### Future Work

10. **MCP server configuration** - When OrbitMesh integrates with MCP
11. **Session persistence** - When needed for long-running tasks
12. **Authentication flows** - Provider-specific, complex

---

## Data Mapping Issues

### Current Limitation: Metadata is a Catch-All

All rich ACP data goes into generic `EventTypeMetadata`:
- Tool calls
- Thoughts
- Plans
- Images
- Permissions

**Problem:** Hard to query, type-unsafe, loses structure

**Solution Options:**

**Option A: Add Event Types**
```go
const (
    EventTypeToolCall
    EventTypeThought
    EventTypePlan
    EventTypeImage
    EventTypePermission
)
```

**Option B: Structured Metadata**
```go
type MetadataData struct {
    Kind  string // "tool_call", "thought", "plan"
    Data  any    // Strongly typed based on Kind
}
```

**Option C: Hybrid**
- Keep metadata for truly generic data
- Add specific types for important categories

**Recommendation:** Option C - Add `EventTypeToolCall` and `EventTypeThought` as they're universally useful, keep rest in metadata for now.

---

## Summary

**Missing Features Impact:**
- 🔴 **Critical:** Token usage tracking, SessionUpdate variants
- 🟡 **Important:** Session persistence, MCP integration, terminal support
- 🟢 **Nice-to-have:** Mode switching, authentication flows

**Reusability Opportunities:**
- 🔴 **High Value:** ProcessManager, SessionUpdate handling
- 🟡 **Medium Value:** InputBuffer, CircuitBreaker, Event types
- 🟢 **Low Value:** FileHelper, GoroutineManager

**Biggest Win:** Extract ProcessManager to `internal/provider/process/` and refactor all three providers to use it. Would eliminate ~300 lines of duplication and standardize behavior.
