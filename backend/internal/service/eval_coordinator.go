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
// EvalCoordinator manages the lifecycle of tool call evals using entity.Store.
// It replaces EvalManager, eliminating the wake-before-suspend race, the
// context.Background() cancellation gap, and the circular callback dependency.
//
// No mu, no evals map, no sessionDeps map, no WakeupRegistry, no ActiveStore,
// no parked goroutines during suspension.
type EvalCoordinator struct {
	evals *entity.Store[*toolcall.Eval, toolcall.EvalSnapshot]
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
	c.evals = entity.NewStore[*toolcall.Eval, toolcall.EvalSnapshot](
		evalStorage,
		nil, // no event bus needed for evals
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

// scheduleRun spawns a goroutine for one eval. The goroutine calls Run (first
// dispatch) or ResumeFunc (after deps fire), then exits regardless of whether
// the handler calls Complete, Fail, or Suspend.
//
// Key difference from the old design: there is no for { select { case <-WakeC() } }
// loop. The goroutine simply returns. Suspension is encoded entirely in the
// persisted Eval.State + Eval.DepsWaiting, not in any in-memory channel.
func (c *EvalCoordinator) scheduleRun(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], isSuspended bool) {
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
		return
	}
	asyncHandler := tools.WrapAtRegistration(def)
	if asyncHandler == nil {
		_ = h.Mutate(func(e **toolcall.Eval) {
			(*e).State = toolcall.EvalStateError
			(*e).Error = fmt.Sprintf("tool %q has no handler configured", toolName)
		})
		h.MarkDone()
		return
	}
	handle := newEvalHandle(h, c)

	go func() {
		var err error
		if isSuspended && asyncHandler.ResumeFunc != nil {
			err = asyncHandler.ResumeFunc(c.ctx, handle)
		} else {
			err = asyncHandler.Run(c.ctx, input, handle)
		}
		// If the handler returned an error without calling Complete/Fail/Suspend,
		// fail the eval now. If it already called one of those, this is a no-op
		// because coordEvalHandle.done is already set.
		if err != nil {
			handle.Fail(err)
		}
		// Goroutine exits here regardless of whether the handler suspended.
		// If it suspended, onEvalDone will reschedule when deps fire.
	}()
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

	// If this eval itself is suspended (waiting on sub-deps), this callback
	// fires when those sub-deps are done. Re-schedule its resume goroutine.
	if state == toolcall.EvalStateSuspended {
		c.scheduleRun(h, true)
		return
	}

	// The eval is terminal (done or error). Check whether the owning session
	// is now fully unblocked.
	c.maybeWakeSession(sessionID)
}

// maybeWakeSession collects results and fires the session resume callback if
// all evals for sessionID are terminal.
func (c *EvalCoordinator) maybeWakeSession(sessionID string) {
	if sessionID == "" || c.onSessionDone == nil {
		return
	}

	ids, err := c.evals.ListIDs()
	if err != nil {
		return
	}

	var results []session.ToolResult
	for _, id := range ids {
		h, err := c.evals.Get(id)
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
	handles := make([]entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], len(calls))
	deps := make([]toolcall.Dependency, len(calls))

	// Create all evals (persisted) before starting any goroutine.
	for i, opts := range calls {
		id := newEvalID()
		e := &toolcall.Eval{
			ID: id, ToolName: opts.ToolName, Input: opts.Input,
			SessionID: opts.SessionID, AttemptID: opts.AttemptID,
			ProviderToolCallID: opts.ProviderToolCallID,
			State:              toolcall.EvalStateRunning,
			CreatedAt:          now, UpdatedAt: now,
		}
		h, err := c.evals.Create(id, e)
		if err != nil {
			return nil, err
		}
		handles[i] = h
		deps[i] = toolcall.Dependency{Kind: "eval", ID: id}
	}

	// Now start goroutines. Even if one completes instantly and calls
	// onEvalDone → maybeWakeSession, the session is not yet suspended so
	// onSessionDone will check and find nothing to do (session still running).
	// suspendSession is called by the caller after this returns.
	for _, h := range handles {
		c.scheduleRun(h, false)
	}

	return deps, nil
}

// CancelEvalsForSession marks all non-terminal evals for the given session as
// cancelled. Uses MutateWhen for idempotency — already-terminal evals are skipped.
func (c *EvalCoordinator) CancelEvalsForSession(sessionID string) {
	ids, _ := c.evals.ListIDs()
	for _, id := range ids {
		h, err := c.evals.Get(id)
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

		case toolcall.EvalStateSuspended:
			// Deps are re-registered automatically by Store.Get via DepSource.Deps().
			// Check now whether all deps are already terminal.
			var alreadyDone bool
			h.Read(func(e **toolcall.Eval) {
				alreadyDone = (*e).IsDone()
			})
			if !alreadyDone {
				depsDone := c.checkDepsDone(h)
				if depsDone {
					c.scheduleRun(h, true)
				}
			}

		case toolcall.EvalStateDone, toolcall.EvalStateError:
			// Already terminal — no action needed.
		}

		return nil
	})
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
	return h.h.Mutate(func(e **toolcall.Eval) {
		(*e).State = toolcall.EvalStateSuspended
		(*e).HandlerState = handlerState
		(*e).DepsWaiting = deps
		(*e).UpdatedAt = time.Now().UTC()
	})
}

// OnCancel registers a function to be called if the eval is cancelled
// externally. Only the most recently registered function is kept.
// NOTE: with the entity.Store model, cancellation is done via CancelEvalsForSession
// which mutates state directly. OnCancel is kept for interface compatibility
// but the callback is not invoked by the coordinator.
func (h *coordEvalHandle) OnCancel(_ func()) {
	// No-op in the EvalCoordinator model. Cancellation happens via
	// CancelEvalsForSession which sets the eval state directly; there is no
	// in-flight goroutine for suspended evals to notify.
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
