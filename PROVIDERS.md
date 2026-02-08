# OrbitMesh Session Providers

This document describes the available session providers and how to use them.

## Overview

Session providers are implementations that run code or commands for OrbitMesh sessions. Each provider type implements a different execution environment.

## Available Providers

### Bash Shell Provider (`bash`)

A simple shell provider for testing, development, and basic shell operations.

**Status**: ✅ Fully functional

**Features**:
- Interactive bash shell
- Direct command execution
- Standard input/output streaming
- Error reporting

**Usage**:
```bash
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <token>" \
  -d '{
    "provider_type": "bash",
    "working_dir": "/path/to/work",
    "environment": {
      "CUSTOM_VAR": "value"
    }
  }'
```

**Configuration**:
- `working_dir`: Directory where the bash shell will start (optional, defaults to git root)
- `environment`: Map of environment variables to set (optional)

**Best for**:
- Testing and development
- Simple shell commands
- Quick iteration
- Debugging

---

### ADK Provider (`adk`)

Agent Development Kit provider for running AI-powered agent sessions using Google AI models.

**Status**: ⚠️ Requires configuration

**Requirements**:
- Google API Key (from `GOOGLE_API_KEY` environment variable or request)
- Gemini 2.5 Flash model access

**Features**:
- Full LLM-powered agent execution
- Model Context Protocol (MCP) integration
- Multi-turn conversations
- Tool/function calling
- Built-in memory and session management

**Usage**:
```bash
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <token>" \
  -d '{
    "provider_type": "adk",
    "working_dir": "/path/to/work",
    "system_prompt": "You are a helpful assistant...",
    "environment": {
      "GOOGLE_API_KEY": "your-api-key-here"
    },
    "mcp_servers": [
      {
        "name": "filesystem",
        "command": "npx",
        "args": ["@modelcontextprotocol/server-filesystem"],
        "env": {}
      }
    ]
  }'
```

**Configuration**:
- `system_prompt`: Custom system prompt for the agent (optional)
- `mcp_servers`: Array of MCP server configurations (optional)
- `environment.GOOGLE_API_KEY`: API key (can be set as env var instead)

**Best for**:
- Production AI agent workloads
- Complex reasoning tasks
- Tool integration
- Long-running agent sessions

---

### PTY Provider (`pty`)

Pseudo-terminal provider for running Claude via a terminal emulator session.

**Status**: ⚠️ Requires setup

**Requirements**:
- `claude` command available in PATH
- Claude CLI properly configured
- Terminal environment support

**Features**:
- Terminal emulation (PTY)
- Full terminal UI support
- Interactive command execution
- Terminal output capture

**Usage**:
```bash
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <token>" \
  -d '{
    "provider_type": "pty",
    "working_dir": "/path/to/work"
  }'
```

**Configuration**:
- `working_dir`: Directory where the PTY will start (optional)

**Best for**:
- Terminal-based workflows
- Complex shell sessions
- Interactive debugging
- Full TUI applications

---

## Session Creation API

### Request Format

```json
{
  "provider_type": "bash|adk|pty",
  "working_dir": "/optional/path",
  "environment": {
    "VAR_NAME": "value"
  },
  "system_prompt": "Optional AI system prompt",
  "mcp_servers": [
    {
      "name": "server-name",
      "command": "command-path",
      "args": ["arg1", "arg2"],
      "env": {}
    }
  ],
  "task_id": "optional-task-id",
  "task_title": "optional-task-title"
}
```

### Response Format

```json
{
  "id": "session-id",
  "provider_type": "bash",
  "state": "starting|running|paused|stopped|error",
  "working_dir": "/path",
  "created_at": "2026-02-07T...",
  "updated_at": "2026-02-07T...",
  "current_task": "task-reference",
  "output": "accumulated output",
  "error_message": "error description if state is error"
}
```

## Session Lifecycle

### States

1. **created**: Session object created but not started
2. **starting**: Provider is initializing
3. **running**: Provider is active and ready
4. **paused**: Provider is temporarily suspended (if supported)
5. **stopped**: Provider was gracefully shut down
6. **error**: Provider encountered an error

### State Transitions

```
created → starting → running → paused/stopping
                        ↓
                      error (from any state)
```

## Error Handling

### Session Creation Errors

- **400 Bad Request**: Missing or invalid `provider_type`
- **400 Bad Request**: Unknown provider type
- **409 Conflict**: Session with same ID already exists
- **500 Internal Server Error**: Provider initialization failed

### Runtime Errors

When a provider encounters an error during execution:
1. Session state transitions to `error`
2. `error_message` field is populated with error description
3. Error event is emitted via SSE stream
4. Session can be stopped/cleaned up

### Checking for Errors

```javascript
// Via REST API
GET /api/sessions/{session-id}
// Response includes error_message if in error state

// Via Event Stream
GET /api/sessions/{session-id}/events
// Errors appear as event type "error"
```

## Usage Examples

### Create a bash session for quick testing

```bash
# 1. Get CSRF token
CSRF=$(curl -s -c cookies.txt http://localhost:8080/api/sessions | \
       grep -o 'orbitmesh-csrf-token.*' | awk '{print $NF}')

# 2. Create session
SESSION=$(curl -s -b cookies.txt -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $CSRF" \
  -d '{"provider_type": "bash", "working_dir": "/tmp"}')

SESSION_ID=$(echo $SESSION | jq -r '.id')

# 3. List sessions
curl -s http://localhost:8080/api/sessions | jq .

# 4. Get session details
curl -s http://localhost:8080/api/sessions/$SESSION_ID | jq .

# 5. Stream events
curl -N -s http://localhost:8080/api/sessions/$SESSION_ID/events

# 6. Stop session
curl -s -b cookies.txt -X DELETE http://localhost:8080/api/sessions/$SESSION_ID \
  -H "X-CSRF-Token: $CSRF"
```

### Create an AI agent session

```bash
# Create ADK provider session
curl -s -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $CSRF" \
  -d '{
    "provider_type": "adk",
    "working_dir": "/workspace",
    "system_prompt": "You are a code assistant specialized in Python.",
    "environment": {
      "GOOGLE_API_KEY": "your-key-here"
    }
  }'
```

## Best Practices

1. **Choose the right provider**:
   - Use `bash` for simple, quick tasks and testing
   - Use `adk` for complex AI-driven workflows
   - Use `pty` for terminal-based tools and interactive sessions

2. **Environment variables**:
   - Set sensitive variables via request rather than hard-coding
   - Use proper env var names for third-party tools (e.g., `GOOGLE_API_KEY`)

3. **Error handling**:
   - Monitor `error_message` field during session creation
   - Subscribe to error events via SSE for runtime errors
   - Implement retry logic for transient failures

4. **Resource management**:
   - Always stop sessions when no longer needed
   - Monitor session count to prevent resource exhaustion
   - Set appropriate working directories

## Troubleshooting

### Session stuck in "starting" state

- Check provider-specific requirements
- Review error events via SSE stream
- Check backend logs for detailed errors

### Session transitions to "error" immediately

- Review `error_message` field for details
- Check required configuration (e.g., API keys)
- Verify working directory exists and is accessible

### Cannot connect to SSE event stream

- Ensure session is in `running` or later state
- Verify CORS headers are properly configured
- Check network connectivity

### Provider-specific issues

**Bash**:
- Ensure bash is installed and accessible
- Verify working directory exists
- Check environment variables are valid

**ADK**:
- Verify `GOOGLE_API_KEY` is set correctly
- Check API key has necessary permissions
- Ensure Gemini model is available

**PTY**:
- Ensure `claude` is in PATH
- Verify Claude CLI is installed
- Check terminal environment support

## Future Providers

Planned providers for future releases:
- Docker container provider
- Remote SSH provider
- Kubernetes pod provider
- Custom plugin providers
