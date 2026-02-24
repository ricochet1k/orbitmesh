// Package tools provides the tool definition registry, filtering utilities,
// provider wire-format renderers, and permission handler interfaces for the
// OrbitMesh agent tool system.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ricochet1k/orbitmesh/internal/toolcall"
)

// Handler is the synchronous easy-mode tool handler — a plain Go function.
// The framework runs it in a goroutine, waits for it, and automatically
// resolves the ToolCallEval and wakes the waiting session.
type Handler func(ctx context.Context, input json.RawMessage) (string, error)

// AsyncHandler is for long-running or cross-session tools.
// Run receives an EvalHandle to suspend, complete, or fail the eval from any goroutine.
// Returning nil means "I'm running in the background; don't auto-resolve."
// Returning an error immediately fails the eval.
// ResumeFunc is optional; if nil, Run is called again on resume with persisted HandlerState.
type AsyncHandler struct {
	Run        func(ctx context.Context, input json.RawMessage, h EvalHandle) error
	ResumeFunc func(ctx context.Context, h EvalHandle) error // optional
}

// EvalHandle is the interface passed to AsyncHandler.Run and ResumeFunc.
// All methods are safe to call from any goroutine, including after the
// original Run call has returned.
type EvalHandle interface {
	// EvalID returns the stable eval ID (== resume token).
	EvalID() string

	// State returns the HandlerState blob that was passed to Suspend,
	// or nil if this is a fresh invocation.
	State() json.RawMessage

	// Complete resolves the eval successfully. Idempotent after the first call.
	Complete(result string)

	// Fail marks the eval as failed. Idempotent after the first call.
	Fail(err error)

	// Suspend checkpoints the eval with optional handler state and dependencies.
	// The eval transitions to EvalStateSuspended. The framework will call
	// ResumeFunc (or Run again if ResumeFunc is nil) when all deps resolve.
	Suspend(handlerState json.RawMessage, deps []toolcall.Dependency) error

	// OnCancel registers a function to call if the eval is cancelled externally.
	// Only the most recently registered function is kept.
	OnCancel(fn func())
}

// ToolDef describes a single tool that an agent can invoke.
// Set exactly one of Handler or AsyncHandler; setting both is an error.
type ToolDef struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage // JSON Schema object describing the tool's input
	Handler      Handler         // sync easy-mode; set Handler OR AsyncHandler, not both
	AsyncHandler *AsyncHandler
}

// Registry stores tool definitions and allows lookup by name.
type Registry interface {
	// Register adds a ToolDef to the registry. Returns an error if a tool with
	// the same name is already registered.
	Register(def ToolDef) error

	// Lookup returns the ToolDef for the given name, or false if not found.
	Lookup(name string) (ToolDef, bool)

	// List returns all registered tool definitions in registration order.
	List() []ToolDef
}

// inMemoryRegistry is a thread-safe in-memory implementation of Registry.
type inMemoryRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDef
	order []string
}

// NewRegistry returns a fresh, empty in-memory Registry.
func NewRegistry() Registry {
	return &inMemoryRegistry{
		tools: make(map[string]ToolDef),
	}
}

// Register adds def to the registry. Returns an error if a tool with the same
// Name is already registered.
func (r *inMemoryRegistry) Register(def ToolDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tools: tool %q is already registered", def.Name)
	}
	r.tools[def.Name] = def
	r.order = append(r.order, def.Name)
	return nil
}

// Lookup returns the ToolDef for name. The second return value is false when
// no tool with that name is registered.
func (r *inMemoryRegistry) Lookup(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.tools[name]
	return def, ok
}

// List returns all registered ToolDefs in insertion order.
func (r *inMemoryRegistry) List() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ToolDef, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// global is the process-level singleton registry, initialised once.
var (
	globalOnce     sync.Once
	globalRegistry Registry
)

// Global returns the process-level singleton Registry. The same instance is
// returned on every call.
func Global() Registry {
	globalOnce.Do(func() {
		globalRegistry = NewRegistry()
	})
	return globalRegistry
}
