# ACP Provider

Provider implementation for the Agent Client Protocol (ACP) using the Coder ACP Go SDK.

## Overview

The ACP provider enables OrbitMesh to integrate with any agent that speaks the Agent Client Protocol, including:

- Google Gemini with `--experimental-acp` flag
- Other ACP-compatible agents

## Architecture

The implementation consists of:

- **Provider**: Factory for creating ACP sessions
- **Session**: Manages the lifecycle of an ACP agent process
- **Client Adapter**: Implements the ACP `Client` interface to handle requests from the agent

## Features

### Implemented

- ✅ Process management (start/stop/kill)
- ✅ ACP protocol initialization
- ✅ Session creation and management
- ✅ Prompt/response handling with streaming
- ✅ File read/write operations
- ✅ Permission request handling (auto-approve first option)
- ✅ Event translation to OrbitMesh domain events
- ✅ Pause/resume support (via input buffering)
- ✅ Circuit breaker pattern for failure handling

### Pending

- ⏳ Terminal support (stubs implemented)
- ⏳ Interactive permission approval UI
- ⏳ Session persistence/loading
- ⏳ MCP server configuration
- ⏳ Output capture for test replay

## Usage

### Configuration

```go
config := acp.Config{
    Command: "gemini",
    Args: []string{"--experimental-acp"},
    WorkingDir: "/path/to/project",
    Environment: map[string]string{
        "GOOGLE_API_KEY": "your-key",
    },
}

provider := acp.NewProvider(config)
```

### Creating a Session

```go
sessionConfig := session.Config{
    WorkingDir: "/path/to/project",
    SystemPrompt: "You are a helpful coding assistant",
}

sess, err := provider.CreateSession("session-123", sessionConfig)
if err != nil {
    log.Fatal(err)
}

// Start the session
if err := sess.Start(ctx, sessionConfig); err != nil {
    log.Fatal(err)
}

// Send input
if err := sess.SendInput(ctx, "Hello, agent!"); err != nil {
    log.Fatal(err)
}

// Listen to events
for event := range sess.Events() {
    fmt.Printf("Event: %+v\n", event)
}
```

## Event Flow

1. **Client → Agent** (via stdin):
   - Initialize request
   - NewSession request
   - Prompt requests (user messages)

2. **Agent → Client** (via stdout):
   - SessionUpdate notifications (streaming responses, tool calls, etc.)
   - File read/write requests
   - Permission requests

3. **OrbitMesh Events**:
   - `output` - Text output from the agent
   - `metric` - Token usage statistics
   - `metadata` - Tool calls, permissions, images, etc.
   - `status_change` - Session state transitions
   - `error` - Error conditions

## Testing with Gemini

```bash
# Set API key
export GOOGLE_API_KEY="your-key"

# The provider will run:
# gemini --experimental-acp
```

## File Structure

- `config.go` - Configuration types
- `provider.go` - Provider implementation
- `session.go` - Session lifecycle management
- `adapter.go` - ACP Client interface implementation
- `README.md` - This file

## Future Enhancements

1. **Output Recording**: Capture ACP JSON-RPC messages for test replay
2. **Terminal Integration**: Full terminal support for interactive commands
3. **Permission UI**: Interactive permission approval system
4. **Session Persistence**: Save/load session state
5. **MCP Integration**: Configure and attach MCP servers to sessions
