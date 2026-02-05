# PTY Provider Advanced Extraction Design

## Overview
This document outlines the design for intelligent status extraction from PTY-based agent providers, including regex, position-based, and AI-assisted strategies.

## Extraction Interface
The `StatusExtractor` interface is the core abstraction for parsing terminal output.

```go
type StatusExtractor interface {
    Extract(output string) (task string, err error)
}
```

## Extraction Strategies

### 1. Position-Based
Extracts status from fixed screen coordinates (row/col). Ideal for tools with a static status bar (e.g., `amp`).

### 2. Regex-Based
Uses regular expressions with named capture groups (e.g., `(?m)^Task: (?P<task>.*)$`). Ideal for scrolling CLI tools (e.g., `claude-code`).

### 3. AI-Assisted (Advanced)
A fallback strategy that uses a small LLM to interpret the screen buffer when rules fail.
- **Dynamic Rules**: The AI can generate new regex patterns based on observed output.
- **Adaptive Detection**: Learns where status information usually appears in a specific tool's output.

## Configuration Schema
Tools are configured via a JSON-based schema:
```json
{
  "tool_name": "claude-code",
  "strategies": [
    { "type": "regex", "pattern": "^Task: (.*)$" },
    { "type": "ai_assisted", "model": "gemini-flash-tiny" }
  ]
}
```

## Screen Buffer Interception
- **Terminal State**: Maintain a virtual terminal buffer using a library like `tcell` or a custom implementation to handle ANSI escape codes.
- **Full Screen vs. Stream**: Extractors can operate on either the raw stream of output or a rendered "snapshot" of the current terminal screen.

## Fallback Strategy
1. Try Position-Based (fastest).
2. Try Regex-Based (standard).
3. Try AI-Assisted (slowest, highest accuracy).
4. If all fail, use "Unknown" and trigger an adaptive rule generation task.
