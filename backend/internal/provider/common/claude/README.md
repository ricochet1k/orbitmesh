# Claude Programmatic Provider

This package implements OrbitMesh's provider for the Claude Code CLI tool running in programmatic mode (`claude -p`).

## Architecture

The provider manages a Claude CLI subprocess and translates its streaming NDJSON output into OrbitMesh domain events.

### Components

1. **ClaudeCodeProvider** (`claudecode.go`)
   - Main provider implementation
   - Manages Claude subprocess lifecycle
   - Handles I/O streams and event translation

2. **Stream Parser** (`stream_parser.go`)
   - Parses Claude CLI NDJSON output
   - Unwraps stream event envelopes
   - Extracts message data (usage, content blocks, errors)

3. **Event Translation** (`events.go`)
   - Converts Claude messages to OrbitMesh events
   - Handles: output, metrics, metadata, errors, status changes

4. **Configuration** (`config.go`)
   - Builds Claude CLI command arguments
   - Handles MCP server configuration
   - Manages system prompts and permissions

## NDJSON Format

The Claude CLI outputs NDJSON with this structure:

```json
{
  "type": "stream_event",
  "event": {
    "type": "content_block_delta",
    "delta": {"text": "..."}
  }
}
```

The parser unwraps the outer `stream_event` envelope to extract the inner event for processing.

## Event Flow

```
Claude CLI subprocess
  ↓ (stdout)
NDJSON lines
  ↓
ParseMessage() - unwrap envelope
  ↓
TranslateToOrbitMeshEvent() - convert to domain events
  ↓
OrbitMesh event channel
```

## Supported Events

### Input (from OrbitMesh)
- User messages via `SendInput()`
- Pause/resume commands
- Stop/kill signals

### Output (to OrbitMesh)
- **Output events**: Streaming text from Claude
- **Metric events**: Token usage (input/output tokens, request count)
- **Metadata events**: Tool use start/stop, message lifecycle
- **Error events**: Claude errors and failures
- **Status change events**: State transitions

## Testing

Run tests:
```bash
go test -v .
```

Test with real NDJSON output:
```bash
# Generate test data
claude -p <prompt> > test_output.ndjson

# Parse with test tool
./cmd/test_parser/test_parser test_output.ndjson
```

## Circuit Breaker

The provider includes a circuit breaker that triggers cooldown after 3 consecutive failures, preventing cascade failures.

## Pause/Resume

The provider supports pausing and resuming execution by buffering input when paused. This allows interactive control of agent execution.
