package claudews

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// Wire types for the Claude Code WebSocket SDK protocol.
// Reference: WEBSOCKET_PROTOCOL_REVERSED.md
// ─────────────────────────────────────────────────────────────────────────────

// RawMessage is an incoming message with its type pre-parsed so we can
// dispatch without double-decoding.
type RawMessage struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Raw     json.RawMessage `json:"-"` // original bytes
}

// unmarshalRaw partially decodes msg so we can dispatch on Type/Subtype.
func unmarshalRaw(data []byte) (RawMessage, error) {
	var rm RawMessage
	if err := json.Unmarshal(data, &rm); err != nil {
		return rm, err
	}
	rm.Raw = data
	return rm, nil
}

// ── Server → CLI ─────────────────────────────────────────────────────────────

// UserMessage sends a prompt to Claude.
type UserMessage struct {
	Type            string         `json:"type"` // "user"
	Message         UserMsgContent `json:"message"`
	ParentToolUseID *string        `json:"parent_tool_use_id"`
	SessionID       string         `json:"session_id"`
}

type UserMsgContent struct {
	Role    string `json:"role"` // "user"
	Content string `json:"content"`
}

func NewUserMessage(content, sessionID string) UserMessage {
	return UserMessage{
		Type:            "user",
		Message:         UserMsgContent{Role: "user", Content: content},
		ParentToolUseID: nil,
		SessionID:       sessionID,
	}
}

// KeepAlive is a no-op heartbeat.
type KeepAlive struct {
	Type string `json:"type"` // "keep_alive"
}

// ── CLI → Server ─────────────────────────────────────────────────────────────

// SystemInitMessage is the first message sent by the CLI after connecting.
type SystemInitMessage struct {
	Type              string          `json:"type"`    // "system"
	Subtype           string          `json:"subtype"` // "init"
	SessionID         string          `json:"session_id"`
	CWD               string          `json:"cwd"`
	Model             string          `json:"model"`
	ClaudeCodeVersion string          `json:"claude_code_version"`
	PermissionMode    string          `json:"permissionMode"`
	APIKeySource      string          `json:"apiKeySource"`
	Tools             []string        `json:"tools"`
	MCPServers        []MCPServerInfo `json:"mcp_servers"`
}

type MCPServerInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// AssistantMessage is a full LLM response.
type AssistantMessage struct {
	Type            string          `json:"type"` // "assistant"
	Message         json.RawMessage `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Error           string          `json:"error,omitempty"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
}

// StreamEvent wraps a streaming delta event (sent when --verbose / --include-partial-messages).
type StreamEvent struct {
	Type            string          `json:"type"`  // "stream_event"
	Event           json.RawMessage `json:"event"` // inner Anthropic streaming event
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
}

// ResultMessage signals the end of a query turn.
type ResultMessage struct {
	Type          string      `json:"type"`    // "result"
	Subtype       string      `json:"subtype"` // "success" | "error_*"
	IsError       bool        `json:"is_error"`
	Result        string      `json:"result,omitempty"`
	Errors        []string    `json:"errors,omitempty"`
	DurationMS    float64     `json:"duration_ms"`
	DurationAPIMS float64     `json:"duration_api_ms"`
	NumTurns      int         `json:"num_turns"`
	TotalCostUSD  float64     `json:"total_cost_usd"`
	StopReason    *string     `json:"stop_reason"`
	Usage         ResultUsage `json:"usage"`
	UUID          string      `json:"uuid"`
	SessionID     string      `json:"session_id"`
}

type ResultUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// SystemStatusMessage signals a status change (e.g. compacting).
type SystemStatusMessage struct {
	Type      string  `json:"type"`    // "system"
	Subtype   string  `json:"subtype"` // "status"
	Status    *string `json:"status"`  // "compacting" | null
	UUID      string  `json:"uuid"`
	SessionID string  `json:"session_id"`
}

// ToolProgressMessage is a heartbeat during tool execution.
type ToolProgressMessage struct {
	Type               string  `json:"type"` // "tool_progress"
	ToolUseID          string  `json:"tool_use_id"`
	ToolName           string  `json:"tool_name"`
	ParentToolUseID    *string `json:"parent_tool_use_id"`
	ElapsedTimeSeconds float64 `json:"elapsed_time_seconds"`
	UUID               string  `json:"uuid"`
	SessionID          string  `json:"session_id"`
}

// ── Control protocol ─────────────────────────────────────────────────────────

// ControlRequest is sent from the CLI when it needs a permission decision or
// wants to invoke a registered hook callback.
type ControlRequest struct {
	Type      string          `json:"type"` // "control_request"
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"` // discriminated by "subtype"
}

// CanUseToolRequest is the payload for subtype "can_use_tool".
type CanUseToolRequest struct {
	Subtype     string         `json:"subtype"` // "can_use_tool"
	ToolName    string         `json:"tool_name"`
	Input       map[string]any `json:"input"`
	ToolUseID   string         `json:"tool_use_id"`
	Description string         `json:"description,omitempty"`
}

// ControlResponse is sent from the server back to the CLI.
type ControlResponse struct {
	Type     string                 `json:"type"` // "control_response"
	Response ControlResponsePayload `json:"response"`
}

type ControlResponsePayload struct {
	Subtype   string         `json:"subtype"` // "success" | "error"
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ToolPermissionBehavior is the allow/deny decision for can_use_tool.
type ToolPermissionBehavior string

const (
	BehaviorAllow ToolPermissionBehavior = "allow"
	BehaviorDeny  ToolPermissionBehavior = "deny"
)

// AllowResponse builds a control_response that permits the tool with
// (optionally modified) input.
func AllowResponse(requestID string, updatedInput map[string]any) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response: map[string]any{
				"behavior":     "allow",
				"updatedInput": updatedInput,
			},
		},
	}
}

// DenyResponse builds a control_response that blocks the tool.
func DenyResponse(requestID, reason string) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response: map[string]any{
				"behavior": "deny",
				"message":  reason,
			},
		},
	}
}

// InterruptRequest is a control_request sent FROM the server to abort the
// current turn.
type InterruptRequest struct {
	Type      string           `json:"type"` // "control_request"
	RequestID string           `json:"request_id"`
	Request   InterruptPayload `json:"request"`
}

type InterruptPayload struct {
	Subtype string `json:"subtype"` // "interrupt"
}
