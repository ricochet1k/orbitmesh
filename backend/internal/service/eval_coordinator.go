package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

// ─────────────────────────────────────────────────────────────────────────────
// DispatchOptions
// ─────────────────────────────────────────────────────────────────────────────

// DispatchOptions carries the parameters required to dispatch a new tool call eval.
type DispatchOptions struct {
	ToolName  string
	Input     json.RawMessage
	SessionID string
	AttemptID string
	// ProviderToolCallID is the tool call ID assigned by the provider (e.g. "call_abc123").
	// It is stored on the Eval and echoed back in tool_result messages.
	ProviderToolCallID string
}

// ─────────────────────────────────────────────────────────────────────────────
// EvalCoordinator manages the lifecycle of tool call evals using
// entity.ActiveStore so each eval has a run body managed by store lifecycle
// primitives (create/load/restart/drain).
type EvalCoordinator struct {
	evals *entity.ActiveStore[*toolcall.Eval, toolcall.EvalSnapshot]
	tools tools.Registry
	ctx   context.Context // executor's long-lived cancellable context

	// onSessionDone is called when all evals for a session have completed.
	// Set once at construction; never changed afterwards.
	onSessionDone func(sessionID string, results []session.ToolResult)
}

// NewEvalCoordinator constructs a ready-to-use EvalCoordinator.
func NewEvalCoordinator(
	ctx context.Context,
	toolRegistry tools.Registry,
	evalStorage entity.TypedStorage[toolcall.EvalSnapshot],
	onSessionDone func(string, []session.ToolResult),
) *EvalCoordinator {
	c := &EvalCoordinator{
		tools:         toolRegistry,
		ctx:           ctx,
		onSessionDone: onSessionDone,
	}
	c.evals = entity.NewActiveStore[*toolcall.Eval, toolcall.EvalSnapshot](
		evalStorage,
		nil, // no event bus needed for evals
		func(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) func(context.Context) {
			return func(runCtx context.Context) {
				c.runEvalLifecycle(runCtx, h)
			}
		},
		entity.StoreOptions[*toolcall.Eval, toolcall.EvalSnapshot]{
			Kind:           "eval",
			FromSnapshot:   func(s toolcall.EvalSnapshot) *toolcall.Eval { e := s; return &e },
			IDFromSnapshot: func(s toolcall.EvalSnapshot) string { return s.ID },
			IsDone: func(s toolcall.EvalSnapshot) bool {
				return s.State == toolcall.EvalStateDone || s.State == toolcall.EvalStateError
			},
			OnDone: func(id string) { c.onEvalDone(id) },
		},
	)
	return c
}

// runEvalLifecycle executes one eval's lifecycle inside the ActiveStore run body.
// It runs tool handler code, waits for dependencies while suspended, and resumes
// the handler when deps complete.
func (c *EvalCoordinator) runEvalLifecycle(ctx context.Context, h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) {
	for {
		state, _, _, deps := c.evalRuntimeState(h)
		switch state {
		case toolcall.EvalStateDone, toolcall.EvalStateError:
			return
		case toolcall.EvalStateSuspended:
			if len(deps) > 0 {
				if err := h.Watch(toEntityDeps(deps)); err != nil {
					newEvalHandle(h, c).Fail(err)
					return
				}
				if !c.checkDepsDone(h) {
					select {
					case <-h.WakeC():
					case <-ctx.Done():
						return
					}
				}
			}
		}

		err := c.invokeToolHandler(ctx, h, state == toolcall.EvalStateSuspended)
		if err != nil {
			newEvalHandle(h, c).Fail(err)
		}

		state, _, _, _ = c.evalRuntimeState(h)
		if state == toolcall.EvalStateSuspended {
			continue
		}
		if state == toolcall.EvalStateDone || state == toolcall.EvalStateError {
			return
		}
		// Handler returned without resolving or suspending; treat as externally
		// managed completion and end this run body.
		return
	}
}

func (c *EvalCoordinator) evalRuntimeState(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) (toolcall.EvalState, string, json.RawMessage, []toolcall.Dependency) {
	state := toolcall.EvalStatePending
	toolName := ""
	var input json.RawMessage
	var deps []toolcall.Dependency
	h.Read(func(e **toolcall.Eval) {
		state = (*e).State
		toolName = (*e).ToolName
		input = (*e).Input
		deps = append(deps[:0], (*e).DepsWaiting...)
	})
	return state, toolName, input, deps
}

func (c *EvalCoordinator) invokeToolHandler(ctx context.Context, h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], isSuspended bool) error {
	var toolName string
	var input json.RawMessage
	h.Read(func(e **toolcall.Eval) {
		toolName = (*e).ToolName
		input = (*e).Input
	})

	def, ok := c.tools.Lookup(toolName)
	if !ok {
		_ = h.Mutate(func(e **toolcall.Eval) {
			(*e).State = toolcall.EvalStateError
			(*e).Error = fmt.Sprintf("tool %q not found", toolName)
		})
		h.MarkDone()
		return nil
	}
	asyncHandler := tools.WrapAtRegistration(def)
	if asyncHandler == nil {
		_ = h.Mutate(func(e **toolcall.Eval) {
			(*e).State = toolcall.EvalStateError
			(*e).Error = fmt.Sprintf("tool %q has no handler configured", toolName)
		})
		h.MarkDone()
		return nil
	}
	handle := newEvalHandle(h, c)
	if isSuspended && asyncHandler.ResumeFunc != nil {
		return asyncHandler.ResumeFunc(ctx, handle)
	}
	return asyncHandler.Run(ctx, input, handle)
}

// onEvalDone is called by Store.OnDone when an eval becomes terminal.
func (c *EvalCoordinator) onEvalDone(id string) {
	h, err := c.evals.Get(id)
	if err != nil {
		return
	}

	var state toolcall.EvalState
	var sessionID string
	h.Read(func(e **toolcall.Eval) {
		state = (*e).State
		sessionID = (*e).SessionID
	})

	if state == toolcall.EvalStateDone || state == toolcall.EvalStateError {
		// The eval is terminal (done or error). Check whether the owning session
		// is now fully unblocked.
		c.maybeWakeSession(sessionID)
	}
}

// maybeWakeSession collects results and fires the session resume callback if
// all evals for sessionID are terminal.
func (c *EvalCoordinator) maybeWakeSession(sessionID string) {
	if sessionID == "" || c.onSessionDone == nil {
		return
	}

	metas, err := c.evals.ListIDs()
	if err != nil {
		return
	}

	var results []session.ToolResult
	for _, m := range metas {
		h, err := c.evals.Get(m.ID)
		if err != nil {
			continue
		}
		var e toolcall.Eval
		h.Read(func(ep **toolcall.Eval) { e = **ep })

		if e.SessionID != sessionID {
			continue
		}
		if e.State != toolcall.EvalStateDone && e.State != toolcall.EvalStateError {
			// At least one eval for this session is still running or suspended.
			return
		}
		toolCallID := e.ProviderToolCallID
		if toolCallID == "" {
			toolCallID = e.ID
		}
		tr := session.ToolResult{
			ToolCallID: toolCallID,
			IsError:    e.State == toolcall.EvalStateError,
		}
		if e.State == toolcall.EvalStateError {
			tr.Result = e.Error
		} else {
			tr.Result = e.Result
		}
		results = append(results, tr)
	}

	// All evals for this session are terminal.
	c.onSessionDone(sessionID, results)
}

// DispatchBatch creates evals for all calls, then starts goroutines.
// The caller suspends the session after this returns.
//
// Why no explicit session watch registration? With goroutine-free suspension,
// there is no WakeC channel to register. The session watch is implicit:
// maybeWakeSession is called every time any eval for the session becomes
// terminal, and it checks whether all evals for that session are done before
// firing onSessionDone. No pre-registration required.
func (c *EvalCoordinator) DispatchBatch(
	sessionID string,
	calls []DispatchOptions,
) ([]toolcall.Dependency, error) {

	now := time.Now().UTC()
	deps := make([]toolcall.Dependency, len(calls))

	// Create all evals. ActiveStore starts each eval run immediately.
	for i, opts := range calls {
		id := newEvalID()
		e := &toolcall.Eval{
			ID: id, ToolName: opts.ToolName, Input: opts.Input,
			SessionID: opts.SessionID, AttemptID: opts.AttemptID,
			ProviderToolCallID: opts.ProviderToolCallID,
			State:              toolcall.EvalStateRunning,
			CreatedAt:          now, UpdatedAt: now,
		}
		_, err := c.evals.Create(id, e)
		if err != nil {
			return nil, err
		}
		deps[i] = toolcall.Dependency{Kind: "eval", ID: id}
	}

	return deps, nil
}

// CancelEvalsForSession marks all non-terminal evals for the given session as
// cancelled. Uses MutateWhen for idempotency — already-terminal evals are skipped.
func (c *EvalCoordinator) CancelEvalsForSession(sessionID string) {
	metas, _ := c.evals.ListIDs()
	for _, m := range metas {
		h, err := c.evals.Get(m.ID)
		if err != nil {
			continue
		}
		var sid string
		var done bool
		h.Read(func(e **toolcall.Eval) {
			sid = (*e).SessionID
			done = (*e).IsDone()
		})
		if sid != sessionID || done {
			continue
		}
		_, _ = h.MutateWhen(func(e **toolcall.Eval) (bool, error) {
			if (*e).IsDone() {
				return false, nil // already terminal; skip
			}
			(*e).State = toolcall.EvalStateError
			(*e).Error = "cancelled"
			return true, nil
		})
		h.MarkDone()
		if rh, err := c.evals.Load(m.ID); err == nil {
			rh.Stop()
		}
	}
}

// OnRestart implements crash recovery for EvalCoordinator.
// It is called once at service startup after persisted state has been loaded.
// For each persisted eval it:
//   - marks Running evals as Error (they were interrupted mid-flight),
//   - re-checks Suspended evals and reschedules any whose deps are already done,
//   - leaves Done/Error evals untouched.
func (c *EvalCoordinator) OnRestart(ctx context.Context) error {
	return c.evals.OnRestart(ctx, func(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) error {
		var state toolcall.EvalState
		h.Read(func(e **toolcall.Eval) { state = (*e).State })

		switch state {
		case toolcall.EvalStateRunning:
			// Was interrupted mid-flight. Mark as error so the session can wake.
			_ = h.Mutate(func(e **toolcall.Eval) {
				(*e).State = toolcall.EvalStateError
				(*e).Error = "interrupted by restart"
			})
			h.MarkDone()
		}

		return nil
	})
}

func (c *EvalCoordinator) BeginDrain(ctx context.Context) {
	c.evals.BeginDrain(ctx)
}

func (c *EvalCoordinator) AwaitDrain(ctx context.Context) error {
	return c.evals.AwaitDrain(ctx)
}

// checkDepsDone reports whether all DepsWaiting for h are currently terminal.
func (c *EvalCoordinator) checkDepsDone(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) bool {
	var deps []toolcall.Dependency
	h.Read(func(e **toolcall.Eval) { deps = (*e).DepsWaiting })
	for _, dep := range deps {
		if dep.Kind != "eval" {
			continue
		}
		dh, err := c.evals.Get(dep.ID)
		if err != nil {
			return false
		}
		var done bool
		dh.Read(func(e **toolcall.Eval) { done = (*e).IsDone() })
		if !done {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// coordEvalHandle — implements tools.EvalHandle, bridging to entity.Handle
// ─────────────────────────────────────────────────────────────────────────────

// coordEvalHandle is the bridge between an AsyncHandler and an entity.Handle.
// It implements tools.EvalHandle using entity.Handle.Mutate and entity.Handle.MarkDone.
type coordEvalHandle struct {
	h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]
	c *EvalCoordinator

	// done ensures Complete/Fail/Suspend are idempotent (at most one terminal call).
	done atomic.Bool
}

// newEvalHandle constructs a coordEvalHandle for the given handle.
func newEvalHandle(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], c *EvalCoordinator) *coordEvalHandle {
	return &coordEvalHandle{h: h, c: c}
}

// EvalID returns the stable eval ID.
func (h *coordEvalHandle) EvalID() string { return h.h.ID() }

// State returns the persisted HandlerState blob for this eval, or nil.
func (h *coordEvalHandle) State() json.RawMessage {
	var state json.RawMessage
	h.h.Read(func(e **toolcall.Eval) {
		state = (*e).HandlerState
	})
	return state
}

// Complete resolves the eval successfully. Idempotent after the first call.
func (h *coordEvalHandle) Complete(result string) {
	if !h.done.CompareAndSwap(false, true) {
		return
	}
	_ = h.h.Mutate(func(e **toolcall.Eval) {
		(*e).State = toolcall.EvalStateDone
		(*e).Result = result
		(*e).UpdatedAt = time.Now().UTC()
	})
	h.h.MarkDone()
}

// Fail marks the eval as failed. Idempotent after the first call.
func (h *coordEvalHandle) Fail(err error) {
	if !h.done.CompareAndSwap(false, true) {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = h.h.Mutate(func(e **toolcall.Eval) {
		(*e).State = toolcall.EvalStateError
		(*e).Error = msg
		(*e).UpdatedAt = time.Now().UTC()
	})
	h.h.MarkDone()
}

// Suspend checkpoints the eval with optional handler state and dependencies.
// The goroutine returns after Suspend returns; onEvalDone reschedules it when
// all deps fire.
func (h *coordEvalHandle) Suspend(handlerState json.RawMessage, deps []toolcall.Dependency) error {
	// Suspension does not consume the done token — the goroutine may be
	// rescheduled later and will call Complete/Fail when it finishes.
	if err := h.h.Mutate(func(e **toolcall.Eval) {
		(*e).State = toolcall.EvalStateSuspended
		(*e).HandlerState = handlerState
		(*e).DepsWaiting = deps
		(*e).UpdatedAt = time.Now().UTC()
	}); err != nil {
		return err
	}
	if len(deps) == 0 {
		return nil
	}
	return h.h.Watch(toEntityDeps(deps))
}

func toEntityDeps(deps []toolcall.Dependency) []entity.Dep {
	out := make([]entity.Dep, len(deps))
	for i, dep := range deps {
		out[i] = entity.Dep{Kind: dep.Kind, ID: dep.ID}
	}
	return out
}

// OnCancel registers a function to be called if the eval is cancelled
// externally. Only the most recently registered function is kept.
// NOTE: with the EvalCoordinator model, cancellation is done via CancelEvalsForSession
// which mutates state directly. OnCancel is kept for interface compatibility
// but the callback is not invoked by the coordinator.
func (h *coordEvalHandle) OnCancel(_ func()) {
	// No-op in the EvalCoordinator model. Cancellation happens via
	// CancelEvalsForSession which sets eval state directly.
}

// ─────────────────────────────────────────────────────────────────────────────
// newEvalID — generates a random, URL-safe eval identifier.
// ─────────────────────────────────────────────────────────────────────────────

// newEvalID generates a random, URL-safe eval identifier.
func newEvalID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "eval_" + hex.EncodeToString(b)
}
