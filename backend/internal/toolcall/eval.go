// Package toolcall defines the core types for the async-first tool call evaluation system.
// An Eval tracks the lifecycle of a single tool invocation from dispatch through completion.
package toolcall

import (
	"encoding/json"
	"time"
)

// EvalState represents the lifecycle state of a tool call evaluation.
type EvalState string

const (
	// EvalStatePending means the eval has been created but not yet started.
	EvalStatePending EvalState = "pending"
	// EvalStateRunning means the handler goroutine is actively executing.
	EvalStateRunning EvalState = "running"
	// EvalStateSuspended means the eval is waiting on one or more dependencies.
	EvalStateSuspended EvalState = "suspended"
	// EvalStateDone means the eval completed successfully; Result is populated.
	EvalStateDone EvalState = "done"
	// EvalStateError means the eval failed; Error is populated.
	EvalStateError EvalState = "error"
)

// Dependency identifies another eval or session that an Eval or Session is waiting on.
// Kind is "eval" or "session"; ID is the corresponding entity's stable identifier.
type Dependency struct {
	// Kind is the type of the depended-on entity: "eval" or "session".
	Kind string `json:"kind"`
	// ID is the stable opaque identifier of the depended-on entity.
	ID string `json:"id"`
}

// Eval tracks a single tool-call invocation from dispatch to completion.
// It is the first-class unit of async tool execution; its ID doubles as the resume/completion token.
type Eval struct {
	// ID is the stable opaque identifier for this eval, used as the resume/completion token.
	ID string `json:"id"`

	// ToolName is the registered name of the tool being invoked.
	ToolName string `json:"tool_name"`

	// Input is the raw JSON arguments passed to the tool.
	Input json.RawMessage `json:"input"`

	// SessionID is the session that originated this tool call.
	SessionID string `json:"session_id"`

	// AttemptID is the run attempt within the session that originated this call.
	AttemptID string `json:"attempt_id"`

	// ProviderToolCallID is the tool call ID assigned by the provider (e.g. "call_abc123").
	// This must be echoed back in tool_result messages to the provider.
	ProviderToolCallID string `json:"provider_tool_call_id,omitempty"`

	// State is the current lifecycle state of this eval.
	State EvalState `json:"state"`

	// Result holds the final output string when State is EvalStateDone.
	Result string `json:"result,omitempty"`

	// Error holds the error message when State is EvalStateError.
	Error string `json:"error,omitempty"`

	// DepsWaiting lists the evals or sessions this eval is currently waiting on
	// when State is EvalStateSuspended.
	DepsWaiting []Dependency `json:"deps_waiting,omitempty"`

	// HandlerState is an opaque blob persisted by the handler when it suspends.
	// The framework stores it and passes it back to the handler on resume.
	// Handlers that do not suspend may leave this nil.
	HandlerState json.RawMessage `json:"handler_state,omitempty"`

	// CreatedAt is the time this eval was first created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the time this eval was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}
