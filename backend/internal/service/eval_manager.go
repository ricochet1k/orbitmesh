package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

// ErrCyclicDependency is returned by WakeupRegistry.Watch when registering
// the watch would create a dependency cycle.
var ErrCyclicDependency = errors.New("cyclic dependency detected")

// WakeupRegistry tracks which sessions and evals are waiting on which
// dependencies, and triggers resumption when all dependencies reach a
// terminal state.
type WakeupRegistry interface {
	// Watch registers that waiter (a session or eval identified by waiterKind
	// and waiterID) is blocked on deps. Returns ErrCyclicDependency if the
	// watch would create a cycle.
	Watch(waiterKind, waiterID string, deps []toolcall.Dependency) error

	// Notify is called when dep reaches a terminal state. It checks all
	// waiters blocked on dep and triggers resumption for any whose complete
	// dependency set is now satisfied.
	Notify(dep toolcall.Dependency)

	// Cancel removes all watches for the given waiter.
	Cancel(waiterKind, waiterID string)
}

// inMemoryWakeupRegistry is a thread-safe, in-memory WakeupRegistry.
type inMemoryWakeupRegistry struct {
	mu sync.Mutex

	// watches maps waiterKey (kind+":"+id) → slice of deps it is waiting on.
	watches map[string][]toolcall.Dependency

	// terminalDeps tracks dependency keys (kind+":"+id) that have reached a
	// terminal state (done or error).
	terminalDeps map[string]bool

	// isTerminal is called to check whether a dep is currently in a terminal
	// state. It is set during wiring.
	isTerminal func(dep toolcall.Dependency) bool

	// onWake is called when all of a waiter's dependencies are terminal.
	onWake func(waiterKind, waiterID string)
}

func newInMemoryWakeupRegistry(
	isTerminal func(dep toolcall.Dependency) bool,
	onWake func(waiterKind, waiterID string),
) *inMemoryWakeupRegistry {
	return &inMemoryWakeupRegistry{
		watches:      make(map[string][]toolcall.Dependency),
		terminalDeps: make(map[string]bool),
		isTerminal:   isTerminal,
		onWake:       onWake,
	}
}

// waiterKey returns the map key for the given waiter.
func waiterKey(kind, id string) string { return kind + ":" + id }

// depKey returns the map key for a Dependency.
func depKey(d toolcall.Dependency) string { return d.Kind + ":" + d.ID }

// Watch registers a watch for waiterKind/waiterID on deps and performs DFS
// cycle detection. Returns ErrCyclicDependency if a cycle would be created.
func (r *inMemoryWakeupRegistry) Watch(waiterKind, waiterID string, deps []toolcall.Dependency) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Printf("wakeup.Watch %v %v %v", waiterKind, waiterID, deps)

	wk := waiterKey(waiterKind, waiterID)

	// Cycle detection: the new waiter must not itself (transitively) be a
	// dependency of any of the nodes it is about to depend on.
	if r.wouldCycle(wk, deps) {
		return ErrCyclicDependency
	}

	r.watches[wk] = deps
	return nil
}

// wouldCycle returns true if adding an edge from wk → deps introduces a cycle.
// It performs a DFS starting at each dep's waiterKey, looking for wk.
// Caller must hold r.mu.
func (r *inMemoryWakeupRegistry) wouldCycle(wk string, deps []toolcall.Dependency) bool {
	visited := make(map[string]bool)
	var dfs func(node string) bool
	dfs = func(node string) bool {
		if node == wk {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, dep := range r.watches[node] {
			if dfs(depKey(dep)) {
				return true
			}
		}
		return false
	}
	for _, dep := range deps {
		if dfs(depKey(dep)) {
			return true
		}
	}
	return false
}

// Notify marks dep as terminal and checks every waiter that depends on dep.
// For any waiter whose entire dep set is now terminal, onWake is called.
func (r *inMemoryWakeupRegistry) Notify(dep toolcall.Dependency) {
	r.mu.Lock()
	dk := depKey(dep)
	r.terminalDeps[dk] = true

	// Collect waiters that mention this dep.
	type wakeTarget struct{ kind, id string }
	var toWake []wakeTarget

	for wk, watchedDeps := range r.watches {
		mentionsDep := false
		for _, d := range watchedDeps {
			if depKey(d) == dk {
				mentionsDep = true
				break
			}
		}
		if !mentionsDep {
			continue
		}

		// Check whether ALL deps are terminal.
		allTerminal := true
		for _, d := range watchedDeps {
			if !r.terminalDeps[depKey(d)] {
				// Also ask the live isTerminal oracle in case we missed a
				// Notify (e.g. after a restart).
				if r.isTerminal != nil && r.isTerminal(d) {
					r.terminalDeps[depKey(d)] = true
				} else {
					allTerminal = false
					break
				}
			}
		}
		if allTerminal {
			// Parse waiterKey back into kind and id.
			kind, id := splitWaiterKey(wk)
			toWake = append(toWake, wakeTarget{kind, id})
			delete(r.watches, wk)
		}
	}
	r.mu.Unlock()

	log.Printf("wakup.Notify %v toWake: %v", dep, toWake)
	if len(toWake) == 0 {
		log.Printf("wakup.Notify %v had no dependents to wake!", dep)
	}

	for _, t := range toWake {
		if r.onWake != nil {
			r.onWake(t.kind, t.id)
		}
	}
}

// Cancel removes all watches registered for the given waiter.
func (r *inMemoryWakeupRegistry) Cancel(waiterKind, waiterID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.watches, waiterKey(waiterKind, waiterID))
}

// splitWaiterKey splits a waiterKey back into (kind, id).
// Format is "kind:id" where id may itself contain colons.
func splitWaiterKey(wk string) (kind, id string) {
	for i := 0; i < len(wk); i++ {
		if wk[i] == ':' {
			return wk[:i], wk[i+1:]
		}
	}
	return wk, ""
}

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
// EvalManager
// ─────────────────────────────────────────────────────────────────────────────

// EvalManager manages the lifecycle of tool call evals: dispatching,
// suspending, completing, failing, and cancelling them. It wires up the
// WakeupRegistry so that sessions and evals are resumed automatically when
// their dependencies reach a terminal state.
type EvalManager struct {
	tools   tools.Registry
	storage storage.EvalStorage
	wakeup  WakeupRegistry
	mu      sync.RWMutex

	// evals is the in-memory eval cache; authoritative for live evals.
	evals map[string]*toolcall.Eval

	// sessionDeps tracks which eval deps a session is currently waiting on.
	// Set by WatchSession and cleared when the session wakes.
	sessionDeps map[string][]toolcall.Dependency

	// OnSessionWake is called when all of a session's tool-call dependencies
	// have resolved. The caller (typically AgentExecutor) should resume the
	// session and inject the provided tool results.
	// It is safe to set this field before any Dispatch calls are made.
	OnSessionWake func(sessionID string, results []session.ToolResult)
}

// NewEvalManager creates a ready-to-use EvalManager. The tools Registry and
// EvalStorage must not be nil.
func NewEvalManager(toolRegistry tools.Registry, evalStorage storage.EvalStorage) *EvalManager {
	m := &EvalManager{
		tools:       toolRegistry,
		storage:     evalStorage,
		evals:       make(map[string]*toolcall.Eval),
		sessionDeps: make(map[string][]toolcall.Dependency),
	}

	m.wakeup = newInMemoryWakeupRegistry(
		func(dep toolcall.Dependency) bool {
			return m.isTerminalDep(dep)
		},
		func(kind, id string) {
			m.handleWake(kind, id)
		},
	)

	return m
}

// isTerminalDep returns true when dep has reached a terminal state (done or error).
func (m *EvalManager) isTerminalDep(dep toolcall.Dependency) bool {
	if dep.Kind != "eval" {
		// Sessions are terminal when they are no longer suspended/running —
		// the session layer notifies us; default to false if unknown.
		return false
	}
	m.mu.RLock()
	e, ok := m.evals[dep.ID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return e.State == toolcall.EvalStateDone || e.State == toolcall.EvalStateError
}

// newEvalID generates a random, URL-safe eval identifier.
func newEvalID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "eval_" + hex.EncodeToString(b)
}

// Dispatch creates a new Eval for the named tool, persists it, and launches
// the handler goroutine. It returns the new eval's ID.
func (m *EvalManager) Dispatch(ctx context.Context, opts DispatchOptions) (string, error) {
	def, ok := m.tools.Lookup(opts.ToolName)
	if !ok {
		return "", fmt.Errorf("tool %q not found in registry", opts.ToolName)
	}

	asyncHandler := tools.WrapAtRegistration(def)
	if asyncHandler == nil {
		return "", fmt.Errorf("tool %q has no handler configured", opts.ToolName)
	}

	evalID := newEvalID()
	now := time.Now().UTC()
	e := &toolcall.Eval{
		ID:                 evalID,
		ToolName:           opts.ToolName,
		Input:              opts.Input,
		SessionID:          opts.SessionID,
		AttemptID:          opts.AttemptID,
		ProviderToolCallID: opts.ProviderToolCallID,
		State:              toolcall.EvalStateRunning,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := m.storage.SaveEval(e); err != nil {
		return "", fmt.Errorf("failed to persist eval %s: %w", evalID, err)
	}

	m.mu.Lock()
	m.evals[evalID] = e
	m.mu.Unlock()

	handle := &evalHandle{
		evalID:  evalID,
		manager: m,
	}

	go func() {
		if err := asyncHandler.Run(ctx, opts.Input, handle); err != nil {
			log.Printf("TOOL start failure %v: %v", opts.ToolName, err)
			handle.Fail(err)
		}
	}()

	return evalID, nil
}

// Complete marks the eval as done and notifies the wakeup registry.
func (m *EvalManager) Complete(evalID, result string) error {
	return m.updateState(evalID, func(e *toolcall.Eval) {
		e.State = toolcall.EvalStateDone
		e.Result = result
	})
}

// Fail marks the eval as errored and notifies the wakeup registry.
func (m *EvalManager) Fail(evalID string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return m.updateState(evalID, func(e *toolcall.Eval) {
		e.State = toolcall.EvalStateError
		e.Error = msg
	})
}

// Suspend transitions the eval to suspended state, persists handler state and
// deps, and registers a watch with the wakeup registry.
//
// The watch is registered while holding m.mu so that a concurrent Complete or
// Fail that calls Notify cannot slip in between the state mutation and the
// Watch registration, which would leave the eval permanently un-wakeable.
// m.mu and wakeup.mu (an internal lock in the registry) are independent, so
// calling Watch under m.mu is safe and does not introduce a deadlock.
func (m *EvalManager) Suspend(evalID string, handlerState json.RawMessage, deps []toolcall.Dependency) error {
	m.mu.Lock()
	e, ok := m.evals[evalID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("eval %s not found", evalID)
	}
	now := time.Now().UTC()
	e.State = toolcall.EvalStateSuspended
	e.HandlerState = handlerState
	e.DepsWaiting = deps
	e.UpdatedAt = now
	// Take a value snapshot for storage write before releasing the lock.
	snapshot := *e
	// Register the watch while still holding m.mu so that any concurrent
	// Complete/Fail that tries to set a terminal state cannot call Notify
	// before our watch is registered.
	watchErr := m.wakeup.Watch("eval", evalID, deps)
	if watchErr != nil {
		// Revert to running state so the caller knows the suspend failed.
		e.State = toolcall.EvalStateRunning
		e.HandlerState = nil
		e.DepsWaiting = nil
	}
	m.mu.Unlock()

	if watchErr != nil {
		return fmt.Errorf("wakeup watch failed for eval %s: %w", evalID, watchErr)
	}

	if err := m.storage.SaveEval(&snapshot); err != nil {
		return fmt.Errorf("failed to persist suspended eval %s: %w", evalID, err)
	}

	return nil
}

// Cancel immediately marks the eval as cancelled (error state), calls any
// registered OnCancel handler, removes its wakeup watch, and notifies waiters.
func (m *EvalManager) Cancel(evalID string) error {
	m.mu.Lock()
	e, ok := m.evals[evalID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("eval %s not found", evalID)
	}
	now := time.Now().UTC()
	e.State = toolcall.EvalStateError
	e.Error = "cancelled"
	e.UpdatedAt = now
	// Take a value snapshot while holding the lock so the storage write uses a
	// stable snapshot that cannot be modified by a concurrent mutation.
	snapshot := *e
	m.mu.Unlock()

	if err := m.storage.SaveEval(&snapshot); err != nil {
		return fmt.Errorf("failed to persist cancelled eval %s: %w", evalID, err)
	}

	m.wakeup.Cancel("eval", evalID)
	// Notify only after the storage write completes so readers see the terminal
	// state in storage before any wake callback fires.
	m.wakeup.Notify(toolcall.Dependency{Kind: "eval", ID: evalID})
	return nil
}

// Get returns the current in-memory Eval for the given ID.
func (m *EvalManager) Get(evalID string) (*toolcall.Eval, error) {
	m.mu.RLock()
	e, ok := m.evals[evalID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("eval %s not found", evalID)
	}
	return e, nil
}

// updateState applies mut to the in-memory eval, persists it, and fires a
// wakeup notification if the new state is terminal.
//
// Ordering guarantee: storage write always completes before Notify fires.
// We take a value snapshot under the lock so that concurrent Complete/Fail
// calls cannot corrupt what gets written to storage.
func (m *EvalManager) updateState(evalID string, mut func(*toolcall.Eval)) error {
	m.mu.Lock()
	e, ok := m.evals[evalID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("eval %s not found", evalID)
	}
	mut(e)
	e.UpdatedAt = time.Now().UTC()
	// Take a value copy while holding the lock so the storage write uses a
	// stable snapshot that cannot be modified by a concurrent mutation.
	snapshot := *e
	m.mu.Unlock()

	log.Printf("EvalManager updateState %v %v", evalID, snapshot)

	if err := m.storage.SaveEval(&snapshot); err != nil {
		return fmt.Errorf("failed to persist eval %s: %w", evalID, err)
	}

	// Notify only after the storage write that put the eval into a terminal
	// state has completed, ensuring readers can observe the terminal state
	// from persistent storage before any wake callback fires.
	if snapshot.State == toolcall.EvalStateDone || snapshot.State == toolcall.EvalStateError {
		m.wakeup.Notify(toolcall.Dependency{Kind: "eval", ID: evalID})
	}

	return nil
}

// resumeEval reconstructs an evalHandle for a suspended eval and calls the
// handler's ResumeFunc (or Run again if ResumeFunc is nil).
func (m *EvalManager) resumeEval(evalID string) {
	m.mu.RLock()
	e, ok := m.evals[evalID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	def, found := m.tools.Lookup(e.ToolName)
	if !found {
		_ = m.Fail(evalID, fmt.Errorf("tool %q not found on resume", e.ToolName))
		return
	}

	asyncHandler := tools.WrapAtRegistration(def)
	if asyncHandler == nil {
		_ = m.Fail(evalID, fmt.Errorf("tool %q has no handler on resume", e.ToolName))
		return
	}

	handle := &evalHandle{
		evalID:  evalID,
		manager: m,
	}

	ctx := context.Background()

	go func() {
		var err error
		if asyncHandler.ResumeFunc != nil {
			err = asyncHandler.ResumeFunc(ctx, handle)
		} else {
			err = asyncHandler.Run(ctx, e.Input, handle)
		}
		if err != nil {
			handle.Fail(err)
		}
	}()
}

// handleWake is the callback invoked by the WakeupRegistry when all of a
// waiter's dependencies have become terminal.
func (m *EvalManager) handleWake(kind, id string) {
	switch kind {
	case "session":
		m.handleSessionWake(id)
	case "eval":
		m.resumeEval(id)
	}
}

// handleSessionWakeWithDeps collects ToolResults for the given eval dep IDs and
// calls OnSessionWake. deps is the dependency list that was satisfied.
func (m *EvalManager) handleSessionWakeWithDeps(sessionID string, deps []toolcall.Dependency) {
	if m.OnSessionWake == nil {
		return
	}

	m.mu.RLock()
	var results []session.ToolResult
	for _, dep := range deps {
		if dep.Kind != "eval" {
			continue
		}
		e, ok := m.evals[dep.ID]
		if !ok {
			continue
		}
		// Use the provider-assigned tool call ID so the provider can match
		// this result to the outstanding tool_use / tool_call request.
		toolCallID := e.ProviderToolCallID
		if toolCallID == "" {
			toolCallID = e.ID
		}
		tr := session.ToolResult{
			ToolCallID: toolCallID,
			Result:     e.Result,
			IsError:    e.State == toolcall.EvalStateError,
		}
		if e.State == toolcall.EvalStateError {
			tr.Result = e.Error
		}
		results = append(results, tr)
	}
	m.mu.RUnlock()

	m.OnSessionWake(sessionID, results)
}

// handleSessionWake is called by the wakeup registry when a session's deps are all terminal.
func (m *EvalManager) handleSessionWake(sessionID string) {
	if m.OnSessionWake == nil {
		return
	}

	m.mu.Lock()
	depsCopy := make([]toolcall.Dependency, len(m.sessionDeps[sessionID]))
	copy(depsCopy, m.sessionDeps[sessionID])
	delete(m.sessionDeps, sessionID)
	m.mu.Unlock()

	m.handleSessionWakeWithDeps(sessionID, depsCopy)
}

// WatchSession registers that sessionID is waiting on deps and stores the dep
// list for use when the session wakes. It is a convenience wrapper around
// wakeup.Watch that also records the deps on the manager.
func (m *EvalManager) WatchSession(sessionID string, deps []toolcall.Dependency) error {
	m.mu.Lock()
	m.sessionDeps[sessionID] = deps
	m.mu.Unlock()
	return m.wakeup.Watch("session", sessionID, deps)
}

// pendingDispatches is an opaque token returned by PrepareDispatches.
// Call Launch() on it after the session is suspended to start the handler
// goroutines. This two-phase design ensures the session suspension context is
// fully set before any handler can complete and trigger the wake callback.
type pendingDispatches struct {
	ctx     context.Context
	manager *EvalManager
	items   []pendingDispatchItem
}

type pendingDispatchItem struct {
	eval    *toolcall.Eval
	handler *tools.AsyncHandler
	input   json.RawMessage
}

// Launch starts all handler goroutines. Call this after suspendSession has
// stored the SuspensionContext so that a fast-completing handler cannot call
// OnSessionWake before the context exists.
func (p *pendingDispatches) Launch() {
	for _, item := range p.items {
		item := item
		handle := &evalHandle{evalID: item.eval.ID, manager: p.manager}
		go func() {
			if err := item.handler.Run(p.ctx, item.input, handle); err != nil {
				log.Printf("TOOL start failure %v: %v", item.eval.ToolName, err)
				handle.Fail(err)
			}
		}()
	}
}

// PrepareDispatches creates evals for all calls, registers the session watch,
// and returns a pendingDispatches token. The caller MUST call suspendSession
// (or otherwise set the session's SuspensionContext) before calling Launch,
// so that a fast handler cannot trigger OnSessionWake before the context exists.
//
// Also returns the dependency slice ready to pass to suspendSession.
func (m *EvalManager) PrepareDispatches(ctx context.Context, sessionID string, calls []DispatchOptions) (*pendingDispatches, []toolcall.Dependency, error) {
	if len(calls) == 0 {
		return nil, nil, fmt.Errorf("PrepareDispatches: no calls provided")
	}

	// Resolve handlers and build evals before taking the lock so that
	// tool-registry lookups and storage I/O don't hold it.
	items := make([]pendingDispatchItem, 0, len(calls))
	now := time.Now().UTC()

	for _, opts := range calls {
		def, ok := m.tools.Lookup(opts.ToolName)
		if !ok {
			return nil, nil, fmt.Errorf("tool %q not found in registry", opts.ToolName)
		}
		asyncHandler := tools.WrapAtRegistration(def)
		if asyncHandler == nil {
			return nil, nil, fmt.Errorf("tool %q has no handler configured", opts.ToolName)
		}

		evalID := newEvalID()
		e := &toolcall.Eval{
			ID:                 evalID,
			ToolName:           opts.ToolName,
			Input:              opts.Input,
			SessionID:          opts.SessionID,
			AttemptID:          opts.AttemptID,
			ProviderToolCallID: opts.ProviderToolCallID,
			State:              toolcall.EvalStateRunning,
			CreatedAt:          now,
			UpdatedAt:          now,
		}

		if err := m.storage.SaveEval(e); err != nil {
			return nil, nil, fmt.Errorf("failed to persist eval %s: %w", evalID, err)
		}

		items = append(items, pendingDispatchItem{eval: e, handler: asyncHandler, input: opts.Input})
	}

	// Build dep list.
	deps := make([]toolcall.Dependency, len(items))
	for i, item := range items {
		deps[i] = toolcall.Dependency{Kind: "eval", ID: item.eval.ID}
	}

	// Atomically: store all evals + register the session watch.
	// No goroutines are started yet, so Notify cannot fire until Launch() is called.
	m.mu.Lock()
	for _, item := range items {
		m.evals[item.eval.ID] = item.eval
	}
	m.sessionDeps[sessionID] = deps
	watchErr := m.wakeup.Watch("session", sessionID, deps)
	m.mu.Unlock()

	if watchErr != nil {
		for _, item := range items {
			_ = m.Fail(item.eval.ID, fmt.Errorf("wakeup watch failed: %w", watchErr))
		}
		return nil, nil, fmt.Errorf("wakeup watch failed for session %s: %w", sessionID, watchErr)
	}

	return &pendingDispatches{ctx: ctx, manager: m, items: items}, deps, nil
}

// NotifySessionTerminal is called by the session layer when a session reaches
// a terminal state so the wakeup registry can wake any eval waiting on it.
func (m *EvalManager) NotifySessionTerminal(sessionID string) {
	m.wakeup.Notify(toolcall.Dependency{Kind: "session", ID: sessionID})
}

// ─────────────────────────────────────────────────────────────────────────────
// evalHandle — implements tools.EvalHandle
// ─────────────────────────────────────────────────────────────────────────────

type evalHandle struct {
	evalID  string
	manager *EvalManager

	stateMu  sync.Mutex
	cancelFn func()

	// done ensures Complete/Fail are idempotent.
	done atomic.Bool
}

// EvalID returns the stable eval ID.
func (h *evalHandle) EvalID() string { return h.evalID }

// State returns the persisted HandlerState blob for this eval, or nil.
func (h *evalHandle) State() json.RawMessage {
	e, err := h.manager.Get(h.evalID)
	if err != nil {
		return nil
	}
	return e.HandlerState
}

// Complete resolves the eval successfully. Safe to call from any goroutine;
// idempotent after the first call.
func (h *evalHandle) Complete(result string) {
	if !h.done.CompareAndSwap(false, true) {
		return
	}
	_ = h.manager.Complete(h.evalID, result)
}

// Fail marks the eval as failed. Safe to call from any goroutine; idempotent
// after the first call.
func (h *evalHandle) Fail(err error) {
	if !h.done.CompareAndSwap(false, true) {
		return
	}
	_ = h.manager.Fail(h.evalID, err)
}

// Suspend checkpoints the eval with optional handler state and declared deps.
func (h *evalHandle) Suspend(handlerState json.RawMessage, deps []toolcall.Dependency) error {
	return h.manager.Suspend(h.evalID, handlerState, deps)
}

// OnCancel registers a function to be called if the eval is cancelled
// externally. Only the most recently registered function is kept.
func (h *evalHandle) OnCancel(fn func()) {
	h.stateMu.Lock()
	h.cancelFn = fn
	h.stateMu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// Context helpers
// ─────────────────────────────────────────────────────────────────────────────

type evalManagerContextKey struct{}

// WithManager stores an EvalManager in ctx for use by tool handlers.
func WithManager(ctx context.Context, m *EvalManager) context.Context {
	return context.WithValue(ctx, evalManagerContextKey{}, m)
}

// ManagerFromContext retrieves the EvalManager stored by WithManager, or nil.
func ManagerFromContext(ctx context.Context) *EvalManager {
	m, _ := ctx.Value(evalManagerContextKey{}).(*EvalManager)
	return m
}
