package entity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ActiveStoreLifecycleMode tracks the store's run-start lifecycle state.
type ActiveStoreLifecycleMode string

const (
	ActiveStoreModeRunning  ActiveStoreLifecycleMode = "running"
	ActiveStoreModeDraining ActiveStoreLifecycleMode = "draining"
	ActiveStoreModeStopping ActiveStoreLifecycleMode = "stopping"
)

// ActiveStoreStartOp identifies the call path attempting to start a run.
type ActiveStoreStartOp string

const (
	ActiveStoreStartCreate  ActiveStoreStartOp = "create"
	ActiveStoreStartLoad    ActiveStoreStartOp = "load"
	ActiveStoreStartRestart ActiveStoreStartOp = "restart"
)

// DrainStartPolicyDecision controls what ActiveStore does with new starts while draining.
type DrainStartPolicyDecision int

const (
	DrainStartReject DrainStartPolicyDecision = iota
	DrainStartDefer
)

// DrainStartPolicy decides how to treat new start attempts while draining.
type DrainStartPolicy func(op ActiveStoreStartOp, id string) DrainStartPolicyDecision

var (
	// ErrActiveStoreDraining indicates new runs are rejected while draining.
	ErrActiveStoreDraining = errors.New("entity active store is draining")
	// ErrActiveStoreStartDeferred indicates callers should defer a start attempt.
	ErrActiveStoreStartDeferred = errors.New("entity active store start deferred")
)

func defaultDrainStartPolicy(ActiveStoreStartOp, string) DrainStartPolicyDecision {
	return DrainStartReject
}

// ActiveStoreOption configures ActiveStore behavior beyond StoreOptions.
type ActiveStoreOption[T Snapshotter[S], S any] func(*ActiveStore[T, S])

// WithDrainStartPolicy sets behavior for new starts while the store is draining.
func WithDrainStartPolicy[T Snapshotter[S], S any](policy DrainStartPolicy) ActiveStoreOption[T, S] {
	return func(s *ActiveStore[T, S]) {
		if policy != nil {
			s.drainStartPolicy = policy
		}
	}
}

// WithDeferredOpStorage enables persisted deferred-op queue behavior.
func WithDeferredOpStorage[T Snapshotter[S], S any](storage TypedStorage[DeferredOpEnvelope]) ActiveStoreOption[T, S] {
	return func(s *ActiveStore[T, S]) {
		s.deferredStorage = storage
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ActiveStore
// ────────────────────────────────────────────────────────────────────────────

// ActiveStore is a Store whose entities each own a goroutine.
// Use this instead of Store when the entity's lifecycle requires a long-running
// background process (provider I/O, event loop, etc.).
//
// The goroutine body factory is registered once at construction time.  There is
// no separate "start" step: Create and Load both start the goroutine immediately,
// so a RunHandle is never in existence before its goroutine is running.
type ActiveStore[T Snapshotter[S], S any] struct {
	*Store[T, S]
	makeBody func(h Handle[T, S]) func(ctx context.Context)

	runsMu sync.Mutex
	runs   map[string]*run // goroutine bookkeeping per entity id

	mode             ActiveStoreLifecycleMode
	drainStartPolicy DrainStartPolicy
	drained          chan struct{}

	deferredStorage TypedStorage[DeferredOpEnvelope]
	deferredMu      sync.Mutex
	deferredIndex   deferredIndex
}

// run holds the goroutine lifecycle state for one entity.
type run struct {
	cancel context.CancelFunc
	done   chan struct{} // closed when the goroutine returns
}

// NewActiveStore constructs an ActiveStore.  makeBody is called once per entity
// at creation (and once on recovery at restart) to produce the goroutine body
// for that entity.
func NewActiveStore[T Snapshotter[S], S any](
	storage TypedStorage[S],
	bus EventBus,
	makeBody func(h Handle[T, S]) func(ctx context.Context),
	opts StoreOptions[T, S],
	activeOpts ...ActiveStoreOption[T, S],
) *ActiveStore[T, S] {
	s := &ActiveStore[T, S]{
		Store:            NewStore(storage, bus, opts),
		makeBody:         makeBody,
		runs:             make(map[string]*run),
		mode:             ActiveStoreModeRunning,
		drainStartPolicy: defaultDrainStartPolicy,
		drained:          make(chan struct{}),
	}
	close(s.drained)
	for _, opt := range activeOpts {
		opt(s)
	}
	return s
}

// Create inserts a new entity and immediately starts its goroutine.
// Returns a RunHandle whose goroutine is already running.
func (s *ActiveStore[T, S]) Create(id string, initial T) (RunHandle[T, S], error) {
	h, err := s.Store.Create(id, initial)
	if err != nil {
		return RunHandle[T, S]{}, err
	}
	rh, err := s.startRun(ActiveStoreStartCreate, h)
	if err != nil {
		if isStartDeferredErr(err) && s.deferredStorage != nil {
			payload, marshalErr := json.Marshal(deferredCreatePayload[S]{Snapshot: initial.Snapshot()})
			if marshalErr != nil {
				_ = h.Delete()
				return RunHandle[T, S]{}, fmt.Errorf("entity.ActiveStore.Create: encode deferred payload %s: %w", id, marshalErr)
			}
			if enqueueErr := s.enqueueDeferredStart(ActiveStoreStartCreate, id, payload); enqueueErr != nil {
				_ = h.Delete()
				return RunHandle[T, S]{}, enqueueErr
			}
			_ = h.Delete()
			return RunHandle[T, S]{}, fmt.Errorf("%w: %s %s", ErrActiveStoreStartDeferred, ActiveStoreStartCreate, id)
		}
		_ = h.Delete()
		return RunHandle[T, S]{}, err
	}
	return rh, nil
}

// Load retrieves an entity from storage (or the in-memory cache) and
// starts its goroutine if it is not already running.
func (s *ActiveStore[T, S]) Load(id string) (RunHandle[T, S], error) {
	h, err := s.Store.Get(id)
	if err != nil {
		return RunHandle[T, S]{}, err
	}
	rh, err := s.startRun(ActiveStoreStartLoad, h)
	if err != nil {
		if isStartDeferredErr(err) && s.deferredStorage != nil {
			if enqueueErr := s.enqueueDeferredStart(ActiveStoreStartLoad, id, nil); enqueueErr != nil {
				return RunHandle[T, S]{}, enqueueErr
			}
			return RunHandle[T, S]{}, fmt.Errorf("%w: %s %s", ErrActiveStoreStartDeferred, ActiveStoreStartLoad, id)
		}
		return RunHandle[T, S]{}, err
	}
	return rh, nil
}

// OnRestart iterates all persisted entities, calls hook for each one, and then
// starts each entity's goroutine (if not already running).
func (s *ActiveStore[T, S]) OnRestart(ctx context.Context, hook RestartHook[T, S]) error {
	if err := s.Store.OnRestart(ctx, hook); err != nil {
		return err
	}
	// Start goroutines for all entities now in the cache.
	handles, err := s.Store.List()
	if err != nil {
		return fmt.Errorf("entity.ActiveStore.OnRestart: list: %w", err)
	}
	for _, h := range handles {
		if _, err := s.startRun(ActiveStoreStartRestart, h); err != nil {
			if isStartDeferredErr(err) {
				continue
			}
			return fmt.Errorf("entity.ActiveStore.OnRestart: start run %s: %w", h.ID(), err)
		}
	}
	if err := s.replayDeferredOps(); err != nil {
		return err
	}
	return nil
}

// LifecycleMode returns the store's current lifecycle mode.
func (s *ActiveStore[T, S]) LifecycleMode() ActiveStoreLifecycleMode {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	return s.mode
}

// BeginDrain transitions the store into draining mode.
// Existing runs are allowed to continue; new starts are policy-controlled.
func (s *ActiveStore[T, S]) BeginDrain(context.Context) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.mode == ActiveStoreModeStopping {
		return
	}
	s.mode = ActiveStoreModeDraining
}

// AwaitDrain waits for all currently running work to complete.
func (s *ActiveStore[T, S]) AwaitDrain(ctx context.Context) error {
	s.runsMu.Lock()
	drained := s.drained
	s.runsMu.Unlock()

	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startRun starts a goroutine for the entity if one is not already running.
// It is idempotent: calling it twice for the same id is safe.
func (s *ActiveStore[T, S]) startRun(op ActiveStoreStartOp, h Handle[T, S]) (RunHandle[T, S], error) {
	s.runsMu.Lock()
	if err := s.startDeniedErrLocked(op, h.id); err != nil {
		s.runsMu.Unlock()
		return RunHandle[T, S]{}, err
	}
	if r, ok := s.runs[h.id]; ok {
		s.runsMu.Unlock()
		// Already running — return a RunHandle backed by the existing run.
		return RunHandle[T, S]{
			Handle: h,
			store:  s,
			r:      r,
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if len(s.runs) == 0 {
		s.drained = make(chan struct{})
	}
	s.runs[h.id] = r
	s.runsMu.Unlock()

	body := s.makeBody(h)
	go func() {
		defer close(r.done)
		defer s.cleanRun(h.id, r)
		body(ctx)
	}()

	return RunHandle[T, S]{
		Handle: h,
		store:  s,
		r:      r,
	}, nil
}

// cleanRun removes the run record when the goroutine exits naturally.
// It does not remove runs that were explicitly stopped (those stay until GC'd).
func (s *ActiveStore[T, S]) cleanRun(id string, r *run) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.runs[id] == r {
		delete(s.runs, id)
		if len(s.runs) == 0 {
			close(s.drained)
		}
	}
}

func (s *ActiveStore[T, S]) checkStartAllowed(op ActiveStoreStartOp, id string) error {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	return s.startDeniedErrLocked(op, id)
}

func (s *ActiveStore[T, S]) startDeniedErrLocked(op ActiveStoreStartOp, id string) error {
	if s.mode != ActiveStoreModeDraining {
		return nil
	}
	decision := DrainStartReject
	if s.drainStartPolicy != nil {
		decision = s.drainStartPolicy(op, id)
	}
	switch decision {
	case DrainStartDefer:
		return fmt.Errorf("%w: %s %s", ErrActiveStoreStartDeferred, op, id)
	default:
		return fmt.Errorf("%w: %s %s", ErrActiveStoreDraining, op, id)
	}
}

func isStartDeferredErr(err error) bool {
	return errors.Is(err, ErrActiveStoreStartDeferred) || errors.Is(err, ErrActiveStoreDraining)
}

// stopForwarding cancels the goroutine for id and all reverse-dep watchers of it.
func (s *ActiveStore[T, S]) stopForwarding(id string) {
	key := s.opts.Kind + ":" + id

	s.depMu.RLock()
	watchers := make([]string, len(s.revdeps[key]))
	copy(watchers, s.revdeps[key])
	s.depMu.RUnlock()

	for _, watcherID := range watchers {
		s.cancelRun(watcherID)
	}
	s.cancelRun(id)
}

// cancelRun cancels the goroutine for the entity with the given id.
func (s *ActiveStore[T, S]) cancelRun(id string) {
	s.runsMu.Lock()
	r := s.runs[id]
	s.runsMu.Unlock()
	if r != nil {
		r.cancel()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RunHandle
// ────────────────────────────────────────────────────────────────────────────

// RunHandle is an opaque reference to an entity whose goroutine is running.
// Embed or hold this instead of Handle when you need lifecycle control.
// The underlying Handle is accessible via RunHandle.Handle for reads/mutations.
type RunHandle[T Snapshotter[S], S any] struct {
	Handle[T, S]
	store *ActiveStore[T, S]
	r     *run
}

// Stop cancels the entity's context, causing the goroutine's ctx.Done() to fire.
// It also walks the dep revdep index: any handle that declared a dep on this
// handle is also stopped.
func (h RunHandle[T, S]) Stop() {
	h.store.stopForwarding(h.id)
}

// Wait blocks until the goroutine exits or ctx is cancelled.
// Returns ctx.Err() if the context is cancelled before the goroutine exits.
func (h RunHandle[T, S]) Wait(ctx context.Context) error {
	select {
	case <-h.r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done returns a channel that is closed when the goroutine exits.
func (h RunHandle[T, S]) Done() <-chan struct{} {
	return h.r.done
}
