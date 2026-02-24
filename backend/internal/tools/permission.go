package tools

import "context"

// PermissionRequest carries the context for a single tool-use permission check.
type PermissionRequest struct {
	// ToolCallID is the unique identifier for this tool invocation.
	ToolCallID string

	// ToolName is the name of the tool the agent wants to call.
	ToolName string

	// Input is the (decoded) arguments the agent wants to pass to the tool.
	Input any

	// Description is a human-readable explanation of what the tool call will do.
	Description string
}

// PermissionDecision is the result of a PermissionHandler evaluation.
type PermissionDecision struct {
	// Granted is true when the tool call is allowed to proceed.
	Granted bool

	// Reason is an optional human-readable explanation of the decision.
	Reason string
}

// PermissionHandler is called before each tool invocation to decide whether
// the agent is allowed to run it.
type PermissionHandler interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionDecision, error)
}

// AutoApprovePermissionHandler is a PermissionHandler that unconditionally
// grants every request.
type AutoApprovePermissionHandler struct{}

// RequestPermission always returns a granted decision.
func (AutoApprovePermissionHandler) RequestPermission(_ context.Context, _ PermissionRequest) (PermissionDecision, error) {
	return PermissionDecision{Granted: true, Reason: "auto-approved"}, nil
}

// AutoDenyPermissionHandler is a PermissionHandler that unconditionally denies
// every request.
type AutoDenyPermissionHandler struct{}

// RequestPermission always returns a denied decision.
func (AutoDenyPermissionHandler) RequestPermission(_ context.Context, _ PermissionRequest) (PermissionDecision, error) {
	return PermissionDecision{Granted: false, Reason: "auto-denied"}, nil
}
