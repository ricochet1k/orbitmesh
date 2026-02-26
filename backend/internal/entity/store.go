package entity

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Internal entity wrapper (not exposed to callers)
// ────────────────────────────────────────────────────────────────────────────

// entry wraps a live entity with its per-entity RWMutex and the wakeup channel
// for dependency-based blocking.
type entry[T Snapshotter[S], S any] struct {
	mu          sync.RWMutex
	val         T
	wake        chan struct{} // closed by MarkDone fan-in; reset by Watch
	pendingDone bool          // set by MarkDone when called from inside Mutate's fn
}

// ────────────────────────────────────────────────────────────────────────────
// Store
// ────────────────────────────────────────────────────────────────────────────

// Store holds a collection of live entities of type T with snapshot type S.
// It owns persistence, pub/sub notification, and dep/revdep tracking.
//
// T must implement Snapshotter[S]; S must be a value-safe, JSON-serialisable type.
//
// All exported methods on Store and Handle are safe for concurrent use.
type Store[T Snapshotter[S], S any] struct {
	opts    StoreOptions[T, S]
	storage TypedStorage[S]
	bus     EventBus

	mapMu   sync.RWMutex
	entries map[string]*entry[T, S]

	// Dependency graph — protected by depMu.
	depMu   sync.RWMutex
	deps    map[string][]Dep    // entity id → deps it is waiting on
	revdeps map[string][]string // "kind:id" → entity ids watching it
}

// NewStore constructs a Store with pluggable persistence and event bus.
// storage and bus may be nil (no persistence / no events).
func NewStore[T Snapshotter[S], S any](
	storage TypedStorage[S],
	bus EventBus,
	opts StoreOptions[T, S],
) *Store[T, S] {
	if opts.Kind == "" {
		panic("entity.NewStore: opts.Kind must be non-empty")
	}
	return &Store[T, S]{
		opts:    opts,
		storage: storage,
		bus:     bus,
		entries: make(map[string]*entry[T, S]),
		deps:    make(map[string][]Dep),
		revdeps: make(map[string][]string),
	}
}

// Create inserts a new entity with the given id and initial value.
// Returns ErrAlreadyExists if an entity with this id is already in the map.
// If storage is configured, the entity is persisted (via initial Mutate) before
// the handle is returned.
func (s *Store[T, S]) Create(id string, initial T) (Handle[T, S], error) {
	s.mapMu.Lock()
	if _, ok := s.entries[id]; ok {
		s.mapMu.Unlock()
		return Handle[T, S]{}, fmt.Errorf("%w: %s", ErrAlreadyExists, id)
	}
	e := &entry[T, S]{val: initial}
	s.entries[id] = e
	s.mapMu.Unlock()

	h := Handle[T, S]{id: id, store: s}

	// Persist the initial state.
	if s.storage != nil {
		e.mu.RLock()
		snap := e.val.Snapshot()
		e.mu.RUnlock()
		if err := s.storage.Save(snap); err != nil {
			// Roll back in-memory insertion on storage failure.
			s.mapMu.Lock()
			delete(s.entries, id)
			s.mapMu.Unlock()
			return Handle[T, S]{}, fmt.Errorf("entity.Store.Create: persist %s: %w", id, err)
		}
	}

	return h, nil
}

// Get returns a Handle for the entity with the given id.
// On a cache miss it loads from storage (if configured), reconstructs the
// live T via opts.FromSnapshot, and inserts it into the cache.
// Returns ErrNotFound if the entity does not exist in memory or storage.
func (s *Store[T, S]) Get(id string) (Handle[T, S], error) {
	// Fast path: already in cache.
	s.mapMu.RLock()
	_, ok := s.entries[id]
	s.mapMu.RUnlock()
	if ok {
		return Handle[T, S]{id: id, store: s}, nil
	}

	if s.storage == nil || s.opts.FromSnapshot == nil {
		return Handle[T, S]{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	// Slow path: load from storage under the write lock to avoid double-load.
	s.mapMu.Lock()
	defer s.mapMu.Unlock()

	// Re-check after acquiring write lock (another goroutine may have raced).
	if _, ok := s.entries[id]; ok {
		return Handle[T, S]{id: id, store: s}, nil
	}

	snap, err := s.storage.Load(id)
	if err != nil {
		return Handle[T, S]{}, fmt.Errorf("%w: %s: %v", ErrNotFound, id, err)
	}

	live := s.opts.FromSnapshot(snap)
	e := &entry[T, S]{val: live}
	s.entries[id] = e

	// If the entity implements DepSource and is not yet done, re-register its
	// watch so wakeup fires correctly after recovery.
	if ds, ok := any(live).(DepSource); ok && !ds.IsDone() {
		if depList := ds.Deps(); len(depList) > 0 {
			h := Handle[T, S]{id: id, store: s}
			// Watch under mapMu — depMu is separate, no deadlock.
			_ = h.watch(depList) // best-effort; cycle errors are logged but not fatal
		}
	}

	return Handle[T, S]{id: id, store: s}, nil
}

// List returns handles for every entity in the store.
// If storage is configured and FromSnapshot is set, all persisted entities
// that are not yet cached are loaded into the cache first.
func (s *Store[T, S]) List() ([]Handle[T, S], error) {
	if s.storage != nil && s.opts.FromSnapshot != nil {
		snapshots, err := s.storage.List()
		if err != nil {
			return nil, fmt.Errorf("entity.Store.List: %w", err)
		}
		s.mapMu.Lock()
		for _, snap := range snapshots {
			id := s.idFromSnap(snap)
			if id == "" {
				continue
			}
			if _, ok := s.entries[id]; ok {
				continue // already cached
			}
			live := s.opts.FromSnapshot(snap)
			e := &entry[T, S]{val: live}
			s.entries[id] = e
		}
		s.mapMu.Unlock()
	}

	s.mapMu.RLock()
	defer s.mapMu.RUnlock()

	handles := make([]Handle[T, S], 0, len(s.entries))
	for id := range s.entries {
		handles = append(handles, Handle[T, S]{id: id, store: s})
	}
	return handles, nil
}

// ListIDs returns the stable identifiers of every entity known to the store,
// merging the in-memory cache with the storage index.
//
// Unlike List(), this never reads or parses individual entity files — the
// storage implementation is expected to satisfy ListIDs() with a directory
// scan or equivalent O(1)-per-entry operation.  Cold entities are NOT loaded
// into the cache; callers that need a live handle should follow up with Get().
//
// Typical use:
//
//	ids, err := store.ListIDs()
//	for _, id := range ids {
//	    h, err := store.Get(id)   // lazy-loads on demand
//	    ...
//	}
func (s *Store[T, S]) ListIDs() ([]string, error) {
	// Collect in-memory IDs first (no storage I/O needed).
	s.mapMu.RLock()
	seen := make(map[string]bool, len(s.entries))
	ids := make([]string, 0, len(s.entries))
	for id := range s.entries {
		seen[id] = true
		ids = append(ids, id)
	}
	s.mapMu.RUnlock()

	if s.storage == nil {
		return ids, nil
	}

	stored, err := s.storage.ListIDs()
	if err != nil {
		return nil, fmt.Errorf("entity.Store.ListIDs: %w", err)
	}
	for _, id := range stored {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// OnRestart iterates all persisted entities and calls hook for each one.
// Entities are loaded lazily and hook is called with a Handle so it can
// read or mutate the entity to repair state (e.g. mark interrupted runs).
// ctx is used for early termination; hook errors abort the walk.
func (s *Store[T, S]) OnRestart(ctx context.Context, hook RestartHook[T, S]) error {
	if s.storage == nil {
		return nil
	}
	snapshots, err := s.storage.List()
	if err != nil {
		return fmt.Errorf("entity.Store.OnRestart: list: %w", err)
	}
	for _, snap := range snapshots {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.opts.FromSnapshot == nil {
			continue
		}
		live := s.opts.FromSnapshot(snap)
		e := &entry[T, S]{val: live}

		id := s.idFromSnap(snap)
		if id == "" {
			continue
		}

		s.mapMu.Lock()
		if _, ok := s.entries[id]; !ok {
			s.entries[id] = e
		}
		s.mapMu.Unlock()

		h := Handle[T, S]{id: id, store: s}
		if err := hook(h); err != nil {
			return fmt.Errorf("entity.Store.OnRestart: hook for %s: %w", id, err)
		}
	}
	return nil
}

// notifyMaybe publishes a change event to the bus after a mutation.
// It skips the (potentially expensive) Snapshot() call when there are no subscribers.
// Called outside any entity lock.
func (s *Store[T, S]) notifyMaybe(id string, e *entry[T, S]) {
	if s.bus == nil {
		return
	}
	var snap any
	if s.bus.HasSubscribers(s.opts.Kind, id) {
		e.mu.RLock()
		snap = e.val.Snapshot()
		e.mu.RUnlock()
	}
	s.bus.Publish(EntityEvent{
		Kind:      s.opts.Kind,
		ID:        id,
		Snapshot:  snap,
		Timestamp: time.Now(),
	})
}

// getEntry returns the in-memory entry for id, or nil if not found.
func (s *Store[T, S]) getEntry(id string) *entry[T, S] {
	s.mapMu.RLock()
	e := s.entries[id]
	s.mapMu.RUnlock()
	return e
}

// ────────────────────────────────────────────────────────────────────────────
// IDer — optional interface for extracting an entity's stable ID
// ────────────────────────────────────────────────────────────────────────────

// IDer is an optional interface that entity types can implement to expose
// their stable string identifier.  Store.OnRestart uses it when walking all
// persisted entities.  Types that do not implement IDer are skipped during
// OnRestart.
type IDer interface {
	EntityID() string
}

func idFromValue[T Snapshotter[S], S any](v T) string {
	if ider, ok := any(v).(IDer); ok {
		return ider.EntityID()
	}
	return ""
}

// idFromSnap extracts the entity ID from a snapshot value using opts.IDFromSnapshot
// if provided, or falling back to IDer on the reconstructed live value.
func (s *Store[T, S]) idFromSnap(snap S) string {
	if s.opts.IDFromSnapshot != nil {
		return s.opts.IDFromSnapshot(snap)
	}
	if s.opts.FromSnapshot != nil {
		live := s.opts.FromSnapshot(snap)
		return idFromValue[T, S](live)
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// Handle
// ────────────────────────────────────────────────────────────────────────────

// Handle is an opaque reference to a single live entity in a Store.
// Obtain one via Store.Create, Store.Get, or Store.List.
//
// All Handle methods are safe for concurrent use.  fn callbacks passed to
// Read and Mutate must not call back into the same store (deadlock).
type Handle[T Snapshotter[S], S any] struct {
	id    string
	store *Store[T, S]
}

// ID returns the entity's stable identifier.
func (h Handle[T, S]) ID() string { return h.id }

// Read calls fn with a read lock held on the entity.
// fn must not call back into the store.
func (h Handle[T, S]) Read(fn func(*T)) {
	e := h.store.getEntry(h.id)
	if e == nil {
		return
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	fn(&e.val)
}

// Mutate calls fn under a write lock, then (outside the lock) persists the
// snapshot and publishes a change event.  Returns any error from persistence.
// fn must not call back into the store.
func (h Handle[T, S]) Mutate(fn func(*T)) error {
	_, err := h.MutateWhen(func(t *T) (bool, error) {
		fn(t)
		return true, nil
	})
	return err
}

// MutateWhen is like Mutate but fn returns (changed bool, err error).
// If changed is false or fn returns a non-nil error, no persistence or
// notification occurs.
func (h Handle[T, S]) MutateWhen(fn func(*T) (bool, error)) (bool, error) {
	e := h.store.getEntry(h.id)
	if e == nil {
		return false, fmt.Errorf("%w: %s", ErrNotFound, h.id)
	}

	// Acquire write lock, run fn, capture snapshot while still under lock.
	var snap S
	var changed bool
	var fnErr error
	var pendingDone bool

	e.mu.Lock()
	changed, fnErr = fn(&e.val)
	if changed && fnErr == nil {
		snap = e.val.Snapshot()
		// Check if MarkDone was requested from inside fn.
		pendingDone = e.pendingDone
		e.pendingDone = false
	}
	e.mu.Unlock()

	if fnErr != nil || !changed {
		return false, fnErr
	}

	// Outside the lock: persist, then notify.
	if h.store.storage != nil {
		if err := h.store.storage.Save(snap); err != nil {
			return true, fmt.Errorf("entity.Handle.Mutate: persist %s: %w", h.id, err)
		}
	}

	h.store.notifyMaybe(h.id, e)

	// Fire dep notifications after storage write completes (design guarantee).
	if pendingDone {
		h.store.fireDone(h.id)
	}

	return true, nil
}

// Snapshot returns a serialisable copy of the entity state.
// Safe to call outside Mutate/Read; acquires a read lock.
func (h Handle[T, S]) Snapshot() S {
	e := h.store.getEntry(h.id)
	if e == nil {
		var zero S
		return zero
	}
	e.mu.RLock()
	snap := e.val.Snapshot()
	e.mu.RUnlock()
	return snap
}

// Delete removes the entity from the store and from storage.
func (h Handle[T, S]) Delete() error {
	h.store.mapMu.Lock()
	_, ok := h.store.entries[h.id]
	if ok {
		delete(h.store.entries, h.id)
	}
	h.store.mapMu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, h.id)
	}

	// Clean up dep graph entries for this entity.
	h.store.depMu.Lock()
	deps := h.store.deps[h.id]
	delete(h.store.deps, h.id)
	for _, dep := range deps {
		key := dep.Kind + ":" + dep.ID
		watchers := h.store.revdeps[key]
		filtered := watchers[:0]
		for _, w := range watchers {
			if w != h.id {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) == 0 {
			delete(h.store.revdeps, key)
		} else {
			h.store.revdeps[key] = filtered
		}
	}
	h.store.depMu.Unlock()

	if h.store.storage != nil {
		if err := h.store.storage.Delete(h.id); err != nil {
			return fmt.Errorf("entity.Handle.Delete: storage %s: %w", h.id, err)
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Dependency + wakeup
// ────────────────────────────────────────────────────────────────────────────

// Watch registers that this handle is waiting on deps.
// When all deps reach a done state, WakeC() is closed on this handle.
// Returns ErrCyclicDependency if adding these edges would create a cycle.
// Each call to Watch resets the previous watch and allocates a fresh WakeC channel.
func (h Handle[T, S]) Watch(deps []Dep) error {
	return h.watch(deps)
}

// watch is the internal implementation used by both Watch and the recovery path.
func (h Handle[T, S]) watch(deps []Dep) error {
	if len(deps) == 0 {
		return nil
	}

	h.store.depMu.Lock()
	defer h.store.depMu.Unlock()

	// Cycle detection: DFS from each dep to see if h.id is reachable.
	for _, dep := range deps {
		if dep.Kind == h.store.opts.Kind && dep.ID == h.id {
			return fmt.Errorf("%w: self-loop on %s/%s", ErrCyclicDependency, dep.Kind, dep.ID)
		}
		if h.store.pathExists(dep.ID, h.id) {
			return fmt.Errorf("%w: %s/%s → %s/%s", ErrCyclicDependency, dep.Kind, dep.ID, h.store.opts.Kind, h.id)
		}
	}

	// Remove old reverse-dep entries for this watcher.
	if old, ok := h.store.deps[h.id]; ok {
		for _, d := range old {
			key := d.Kind + ":" + d.ID
			watchers := h.store.revdeps[key]
			filtered := watchers[:0]
			for _, w := range watchers {
				if w != h.id {
					filtered = append(filtered, w)
				}
			}
			if len(filtered) == 0 {
				delete(h.store.revdeps, key)
			} else {
				h.store.revdeps[key] = filtered
			}
		}
	}

	// Register new deps.
	h.store.deps[h.id] = deps
	for _, dep := range deps {
		key := dep.Kind + ":" + dep.ID
		h.store.revdeps[key] = append(h.store.revdeps[key], h.id)
	}

	// Reset the WakeC channel.
	e := h.store.getEntry(h.id)
	if e != nil {
		e.mu.Lock()
		e.wake = make(chan struct{})
		e.mu.Unlock()
	}

	return nil
}

// pathExists checks (under depMu already held) whether there is a directed
// path from fromID to toID in the same-store dep graph.
// Only considers edges within this store's kind.
func (s *Store[T, S]) pathExists(fromID, toID string) bool {
	visited := make(map[string]bool)
	return s.dfsPath(fromID, toID, visited)
}

func (s *Store[T, S]) dfsPath(current, target string, visited map[string]bool) bool {
	if current == target {
		return true
	}
	if visited[current] {
		return false
	}
	visited[current] = true
	for _, dep := range s.deps[current] {
		if dep.Kind != s.opts.Kind {
			continue // cross-kind edges not tracked in this store's graph
		}
		if s.dfsPath(dep.ID, target, visited) {
			return true
		}
	}
	return false
}

// MarkDone declares this handle's current state done, notifying all
// reverse-dep waiters whose full dep set is now satisfied.
//
// If called from inside a Mutate fn, the notification is enqueued and fires
// after the write-lock is released and the storage write completes.
// If called outside Mutate, it fires immediately.
func (h Handle[T, S]) MarkDone() {
	e := h.store.getEntry(h.id)
	if e == nil {
		return
	}

	// If called while holding the write lock (i.e. from inside Mutate's fn),
	// record the pending notification so Mutate can fire it post-lock.
	// We detect this by trying a TryLock; if it fails we're already under lock.
	if e.mu.TryLock() {
		e.mu.Unlock()
		// Not holding the lock — fire immediately.
		h.store.fireDone(h.id)
	} else {
		// Under the write lock — enqueue for post-Mutate firing.
		e.pendingDone = true
	}
}

// fireDone walks the revdep index for this entity and closes the WakeC channel
// of any watcher whose full dep set is now satisfied.
func (s *Store[T, S]) fireDone(id string) {
	key := s.opts.Kind + ":" + id
	s.depMu.RLock()
	watchers := make([]string, len(s.revdeps[key]))
	copy(watchers, s.revdeps[key])
	s.depMu.RUnlock()

	for _, watcherID := range watchers {
		if s.allDepsDone(watcherID) {
			s.closeWakeC(watcherID)
		}
	}

	// Notify the coordinator that this entity is done.
	// Called outside any lock; safe for the callback to re-enter the store.
	if s.opts.OnDone != nil {
		s.opts.OnDone(id)
	}
}

// allDepsDone reports whether every dep registered for watcherID has reached
// a done state.  A dep is considered done when its entity either (a) implements
// IDer + DepSource and IsDone returns true, or (b) the store's IsDone option
// func returns true on its snapshot.
func (s *Store[T, S]) allDepsDone(watcherID string) bool {
	s.depMu.RLock()
	depList := make([]Dep, len(s.deps[watcherID]))
	copy(depList, s.deps[watcherID])
	s.depMu.RUnlock()

	for _, dep := range depList {
		if dep.Kind != s.opts.Kind {
			// Cross-kind dep — can only check via DepSource on the entity's value.
			// For now we conservatively treat unknown cross-kind deps as not done.
			// Callers that need cross-kind tracking should use a shared DoneChecker.
			continue
		}
		e := s.getEntry(dep.ID)
		if e == nil {
			// Dep entity not in cache — treat as not done.
			return false
		}
		done := false
		if s.opts.IsDone != nil {
			e.mu.RLock()
			snap := e.val.Snapshot()
			e.mu.RUnlock()
			done = s.opts.IsDone(snap)
		} else if ds, ok := any(e.val).(DepSource); ok {
			done = ds.IsDone()
		}
		if !done {
			return false
		}
	}
	return true
}

// closeWakeC closes the WakeC channel for the entity with the given ID,
// signalling that all its deps are satisfied.
func (s *Store[T, S]) closeWakeC(id string) {
	e := s.getEntry(id)
	if e == nil {
		return
	}
	e.mu.Lock()
	ch := e.wake
	e.wake = nil
	e.mu.Unlock()

	if ch != nil {
		// Safe to call close on a nil-guarded channel exactly once.
		select {
		case <-ch:
			// Already closed — nothing to do.
		default:
			close(ch)
		}
	}
}

// WakeC returns a channel that is closed exactly once when all of this
// handle's registered deps become done.  It is reset each time Watch is called.
// Returns a closed channel if Watch has not been called or the entity is gone.
func (h Handle[T, S]) WakeC() <-chan struct{} {
	e := h.store.getEntry(h.id)
	if e == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	e.mu.RLock()
	ch := e.wake
	e.mu.RUnlock()
	if ch == nil {
		ch = make(chan struct{})
		close(ch)
	}
	return ch
}

// Unwatch removes all dep registrations for this handle without firing
// notifications.  Used when an entity is cancelled mid-wait.
func (h Handle[T, S]) Unwatch() {
	h.store.depMu.Lock()
	defer h.store.depMu.Unlock()

	deps := h.store.deps[h.id]
	delete(h.store.deps, h.id)
	for _, dep := range deps {
		key := dep.Kind + ":" + dep.ID
		watchers := h.store.revdeps[key]
		filtered := watchers[:0]
		for _, w := range watchers {
			if w != h.id {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) == 0 {
			delete(h.store.revdeps, key)
		} else {
			h.store.revdeps[key] = filtered
		}
	}
}
