package entity

import (
	"context"
	"fmt"
	"sync"
)

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
) *ActiveStore[T, S] {
	return &ActiveStore[T, S]{
		Store:    NewStore(storage, bus, opts),
		makeBody: makeBody,
		runs:     make(map[string]*run),
	}
}

// Create inserts a new entity and immediately starts its goroutine.
// Returns a RunHandle whose goroutine is already running.
func (s *ActiveStore[T, S]) Create(id string, initial T) (RunHandle[T, S], error) {
	h, err := s.Store.Create(id, initial)
	if err != nil {
		return RunHandle[T, S]{}, err
	}
	return s.startRun(h), nil
}

// Load retrieves an entity from storage (or the in-memory cache) and
// starts its goroutine if it is not already running.
func (s *ActiveStore[T, S]) Load(id string) (RunHandle[T, S], error) {
	h, err := s.Store.Get(id)
	if err != nil {
		return RunHandle[T, S]{}, err
	}
	return s.startRun(h), nil
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
		s.startRun(h)
	}
	return nil
}

// startRun starts a goroutine for the entity if one is not already running.
// It is idempotent: calling it twice for the same id is safe.
func (s *ActiveStore[T, S]) startRun(h Handle[T, S]) RunHandle[T, S] {
	s.runsMu.Lock()
	if r, ok := s.runs[h.id]; ok {
		s.runsMu.Unlock()
		// Already running — return a RunHandle backed by the existing run.
		return RunHandle[T, S]{
			Handle: h,
			store:  s,
			r:      r,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		cancel: cancel,
		done:   make(chan struct{}),
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
	}
}

// cleanRun removes the run record when the goroutine exits naturally.
// It does not remove runs that were explicitly stopped (those stay until GC'd).
func (s *ActiveStore[T, S]) cleanRun(id string, r *run) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.runs[id] == r {
		delete(s.runs, id)
	}
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
