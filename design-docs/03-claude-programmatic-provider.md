# Claude Programmatic Mode Provider

## Overview

This document describes the implementation of a provider for the `claude` CLI tool using its programmatic mode (`-p` flag). This provider enables OrbitMesh to orchestrate Claude agents with full access to Claude's streaming JSON API, system prompt customization, MCP server configuration, and all programmatic features.

## Motivation

The existing PTY provider works well for CLI tools but requires screen scraping and lacks structured access to:
- Token usage and cost metrics
- Detailed tool use information
- Streaming status updates
- Native MCP server configuration

Claude's `-p` mode with `--output-format=stream-json` provides all of this natively through a structured JSON stream.

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    OrbitMesh Executor                        │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            │ session.Session interface
                            │
┌───────────────────────────▼─────────────────────────────────┐
│              ClaudeCodeProvider                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Process Management (Start/Stop/Pause/Resume/Kill)   │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Command Builder (flags from config.Custom)          │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ JSON Stream Parser (stdin/stdout)                   │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Event Translator (Claude JSON → domain.Event)       │   │
│  └─────────────────────────────────────────────────────┘   │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            │ claude -p --output-format=stream-json
                            │           --input-format=stream-json
                            │
┌───────────────────────────▼─────────────────────────────────┐
│                    Claude CLI Process                        │
└─────────────────────────────────────────────────────────────┘
```

### Key Components

#### 1. ClaudeCodeProvider

Main session implementation that manages the lifecycle of a Claude programmatic session.

**Responsibilities:**
- Launch and manage `claude -p` process
- Build command-line arguments from configuration
- Parse bidirectional JSON streams
- Translate Claude events to OrbitMesh domain events
- Handle process lifecycle (start, stop, pause, resume, kill)

**State Management:**
- Uses `native.ProviderState` for thread-safe state tracking
- Uses `native.EventAdapter` for event emission
- Maintains process handle and I/O streams

#### 2. Stream Parser

Handles parsing of newline-delimited JSON from Claude's stdout.

**Features:**
- Line-by-line JSON parsing
- Partial message buffering
- Error recovery
- Type discrimination for different message types

**Message Types (from Claude):**
- `{"type": "message_start", ...}` - Session initialization
- `{"type": "content_block_start", ...}` - Tool use or text block
- `{"type": "content_block_delta", ...}` - Streaming content
- `{"type": "message_delta", ...}` - Token usage updates
- `{"type": "message_stop", ...}` - Session completion
- `{"type": "error", ...}` - Error messages

#### 3. Configuration Builder

Translates `session.Config.Custom` map to command-line arguments.

**Supported Configuration:**

| Config Key | CLI Flag | Type | Description |
|------------|----------|------|-------------|
| `system_prompt` | `--system-prompt` | string | Replace default system prompt |
| `append_system_prompt` | `--append-system-prompt` | string | Append to system prompt |
| `mcp_config` | `--mcp-config` | []string or string | MCP server configuration |
| `strict_mcp` | `--strict-mcp-config` | bool | Only use specified MCP servers |
| `model` | `--model` | string | Model to use (e.g., "opus", "sonnet") |
| `max_budget_usd` | `--max-budget-usd` | float64 | Maximum spend limit |
| `allowed_tools` | `--allowed-tools` | []string | Whitelist of allowed tools |
| `disallowed_tools` | `--disallowed-tools` | []string | Blacklist of disallowed tools |
| `permission_mode` | `--permission-mode` | string | Permission mode (plan, default, etc.) |
| `json_schema` | `--json-schema` | object | JSON schema for structured output |
| `no_session_persistence` | `--no-session-persistence` | bool | Disable session saving |
| `fallback_model` | `--fallback-model` | string | Fallback model on overload |
| `effort` | `--effort` | string | Effort level (low, medium, high) |
| `agents` | `--agents` | object | Custom agent definitions |

**Additional flags always set:**
- `--output-format=stream-json` - Enable streaming JSON output
- `--input-format=stream-json` - Enable streaming JSON input
- `--include-partial-messages` - Get partial message chunks
- Environment variable `CLAUDECODE` unset to allow nested execution

#### 4. Event Translator

Maps Claude's streaming JSON to OrbitMesh domain events.

**Translation Rules:**

| Claude Event | OrbitMesh Event | Notes |
|--------------|-----------------|-------|
| `message_start` | StatusChange (StateStarting → StateRunning) | Session initialized |
| `content_block_start` (tool_use) | Metadata | Tool invocation details |
| `content_block_delta` (text) | Output | Streaming text content |
| `message_delta` (usage) | Metric | Token counts and costs |
| `message_stop` | StatusChange (StateRunning → StateStopped) | Clean completion |
| `error` | Error + StatusChange (→ StateError) | Error details |

## Implementation Plan

### Phase 1: Core Provider Structure

**Files to create:**
1. `backend/internal/provider/common/claude/provider.go`
   - `ClaudeCodeProvider` struct
   - `NewClaudeCodeProvider()` constructor
   - Session interface methods (Start, Stop, Pause, Resume, Kill, Status, Events, SendInput)

2. `backend/internal/provider/common/claude/config.go`
   - `buildCommandArgs(config session.Config) ([]string, error)`
   - Configuration validation
   - Type conversion helpers

### Phase 2: JSON Stream Processing

**Files to create:**
3. `backend/internal/provider/common/claude/stream_parser.go`
   - `StreamParser` struct
   - `ParseMessage(line []byte) (Message, error)`
   - Message type definitions
   - Error handling

4. `backend/internal/provider/common/claude/events.go`
   - `translateToOrbitMeshEvent(claudeMsg Message) (domain.Event, bool)`
   - Event type mapping
   - Metrics extraction

### Phase 3: Process Management

**Implementation in provider.go:**
- Start: Launch process with proper environment
- Stop: Graceful SIGTERM, wait for completion
- Pause: Send pause signal (if supported) or buffer input
- Resume: Continue from paused state
- Kill: Immediate SIGKILL

**I/O Handling:**
- Goroutine for stdout reading and parsing
- Goroutine for stdin writing from queue
- Proper cleanup on shutdown

### Phase 4: Integration

**Files to modify:**
5. `backend/cmd/orbitmesh/main.go`
   - Register "claude" provider in factory
   - Add configuration helper function

### Phase 5: Testing

**Files to create:**
6. `backend/internal/provider/common/claude/provider_test.go`
   - Unit tests for provider lifecycle
   - Mock subprocess execution
   - State transition tests

7. `backend/internal/provider/common/claude/config_test.go`
   - Configuration parsing tests
   - Command building tests
   - Edge cases and validation

8. `backend/internal/provider/common/claude/stream_parser_test.go`
   - JSON parsing tests
   - Partial message handling
   - Error recovery

## Detailed Design

### Process Lifecycle

```
StateCreated
    │
    ├─> Start() called
    │   ├─> Build command args from config
    │   ├─> Unset CLAUDECODE env var
    │   ├─> Launch: claude -p --output-format=stream-json ...
    │   ├─> Set up stdin/stdout pipes
    │   ├─> Start stream parser goroutine
    │   └─> Transition to StateStarting
    │
StateStarting
    │
    ├─> Receive message_start from Claude
    │   └─> Transition to StateRunning
    │
StateRunning
    │
    ├─> Normal operation
    │   ├─> Parse streaming JSON messages
    │   ├─> Emit domain events
    │   ├─> Handle SendInput() calls
    │   └─> Update metrics
    │
    ├─> Pause() called
    │   └─> Transition to StatePaused
    │
    ├─> Stop() called
    │   ├─> Send SIGTERM to process
    │   ├─> Transition to StateStopping
    │   └─> Wait for clean shutdown
    │
    ├─> Process exits normally
    │   └─> Transition to StateStopped
    │
    └─> Error occurs
        └─> Transition to StateError
```

### Stream Format Examples

**Input (to Claude):**
```json
{"type": "user_message", "content": "Hello, please help me"}
```

**Output (from Claude):**
```json
{"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5-20250929","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}
{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}
{"type":"content_block_stop","index":0}
{"type":"message_delta","delta":{"stop_reason":"end_turn","usage":{"output_tokens":2}}}
{"type":"message_stop"}
```

### Error Handling

**Process Errors:**
- Process fails to start → StateError, emit error event
- Process crashes during execution → StateError, emit error event
- JSON parsing error → Log warning, continue (resilient parsing)

**Circuit Breaker:**
- Track failure count (like PTY provider)
- After 3 failures, enter cooldown period (30 seconds)
- Prevents rapid restart loops

**Stream Errors:**
- Invalid JSON → Skip line, log warning
- Unknown message type → Log info, continue
- Partial message at EOF → Buffer and wait

## Usage Examples

### Basic Usage

```go
config := session.Config{
    ProviderType: "claude",
    WorkingDir:   "/path/to/project",
    SystemPrompt: "You are a helpful coding assistant",
    Custom: map[string]any{
        "model": "sonnet",
        "permission_mode": "plan",
    },
}

sess, err := factory.CreateSession("claude", "session-123", config)
if err != nil {
    return err
}

ctx := context.Background()
if err := sess.Start(ctx, config); err != nil {
    return err
}

// Listen for events
for event := range sess.Events() {
    switch event.Type {
    case domain.EventTypeOutput:
        fmt.Println("Output:", event.Data)
    case domain.EventTypeMetric:
        fmt.Println("Metrics:", event.Data)
    }
}
```

### Advanced Configuration

```go
config := session.Config{
    ProviderType: "claude",
    WorkingDir:   "/path/to/project",
    Custom: map[string]any{
        "system_prompt": "You are an expert Go developer",
        "append_system_prompt": "Always write tests for new functions",
        "model": "opus",
        "max_budget_usd": 5.0,
        "mcp_config": []string{
            `{"mcpServers": {"strandyard": {"command": "strand", "args": ["mcp"]}}}`,
        },
        "strict_mcp": true,
        "allowed_tools": []string{"Bash", "Edit", "Read", "Write"},
        "permission_mode": "plan",
        "json_schema": map[string]any{
            "type": "object",
            "properties": map[string]any{
                "summary": map[string]any{"type": "string"},
                "changes": map[string]any{
                    "type": "array",
                    "items": map[string]any{"type": "string"},
                },
            },
        },
    },
}
```

### MCP Server Override

```go
// Override default MCP servers with custom configuration
config := session.Config{
    ProviderType: "claude",
    Custom: map[string]any{
        "mcp_config": []string{
            `{"mcpServers": {
                "strandyard": {
                    "command": "strand",
                    "args": ["mcp"],
                    "env": {"STRAND_PROJECT": "orbitmesh"}
                },
                "custom-tool": {
                    "command": "/path/to/custom/mcp-server"
                }
            }}`,
        },
        "strict_mcp": true, // Only use the MCP servers defined above
    },
}
```

## Benefits

### Over PTY Provider
1. **No Screen Scraping**: Direct access to structured data
2. **Better Observability**: Full visibility into tool calls, reasoning, and decisions
3. **Accurate Metrics**: Native token counting and cost tracking
4. **Cleaner State**: Explicit state transitions instead of inference
5. **Rich Events**: Detailed event stream with all agent activity

### Over Direct API
1. **Session Management**: Built-in conversation persistence (unless disabled)
2. **Tool Integration**: Automatic tool setup and permission handling
3. **MCP Support**: Native MCP server lifecycle management
4. **Development Tools**: Built-in debugging, hooks, and IDE integration

## Future Enhancements

1. **Pause/Resume Support**: If Claude adds programmatic pause/resume
2. **Budget Monitoring**: Emit warnings as budget approaches limit
3. **Custom Agents**: Support for `--agents` flag with role definitions
4. **Session Forking**: Use `--fork-session` for branching conversations
5. **File Resources**: Support `--file` flag for initial file uploads
6. **Remote Sessions**: Integration with `--from-pr` and remote sessions

## Security Considerations

1. **Environment Isolation**: Unset `CLAUDECODE` to avoid nested session conflicts
2. **Working Directory**: Respect `WorkingDir` from config
3. **Tool Restrictions**: Honor `allowed_tools` and `disallowed_tools`
4. **Budget Limits**: Respect `max_budget_usd` to prevent runaway costs
5. **Permission Mode**: Support `permission_mode` for different trust levels

## Testing Strategy

1. **Unit Tests**: Test individual components in isolation
2. **Integration Tests**: Test full provider lifecycle with mock subprocess
3. **Stream Parser Tests**: Comprehensive JSON parsing with real examples
4. **Config Builder Tests**: Validate all configuration permutations
5. **Error Handling Tests**: Verify graceful degradation and recovery

## Acceptance Criteria

- [ ] Provider successfully starts and stops Claude processes
- [ ] Streaming JSON is parsed correctly into domain events
- [ ] All configuration options are properly translated to CLI flags
- [ ] MCP server configuration can be overridden
- [ ] System prompt can be set and appended
- [ ] Token metrics are accurately reported
- [ ] Errors are handled gracefully with circuit breaker
- [ ] Input can be sent to running sessions
- [ ] Unit tests achieve >80% coverage
- [ ] Integration tests verify end-to-end functionality

## References

- Claude CLI help: `claude -h`
- Claude programmatic mode: `--output-format=stream-json`
- OrbitMesh provider interface: `backend/internal/session/session.go`
- PTY provider reference: `backend/internal/provider/pty/pty.go`
- Native provider utilities: `backend/internal/provider/native/adapter.go`
