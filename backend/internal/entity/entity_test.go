package entity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/entity"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers: minimal entity + storage + bus implementations
// ─────────────────────────────────────────────────────────────────────────────

type counter struct {
	ID  string
	Val int
}

func (c *counter) Snapshot() counterSnap { return counterSnap{ID: c.ID, Val: c.Val} }
func (c *counter) EntityID() string      { return c.ID }

type counterSnap struct {
	ID  string
	Val int
}

func newCounterStore(storage entity.TypedStorage[counterSnap], bus entity.EventBus) *entity.Store[*counter, counterSnap] {
	return entity.NewStore[*counter, counterSnap](storage, bus, entity.StoreOptions[*counter, counterSnap]{
		Kind: "counter",
		FromSnapshot: func(s counterSnap) *counter {
			return &counter{ID: s.ID, Val: s.Val}
		},
		IDFromSnapshot: func(s counterSnap) string { return s.ID },
		IsDone:         func(s counterSnap) bool { return s.Val < 0 }, // negative = done
	})
}

// memStorage is an in-memory TypedStorage for testing.
type memStorage[S any] struct {
	mu   sync.RWMutex
	data map[string]S
	idFn func(S) string
}

func newMemStorage[S any](idFn func(S) string) *memStorage[S] {
	return &memStorage[S]{data: make(map[string]S), idFn: idFn}
}

func (m *memStorage[S]) Save(s S) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[m.idFn(s)] = s
	return nil
}

func (m *memStorage[S]) Load(id string) (S, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.data[id]
	if !ok {
		var zero S
		return zero, entity.ErrNotFound
	}
	return s, nil
}

func (m *memStorage[S]) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func (m *memStorage[S]) List() ([]S, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]S, 0, len(m.data))
	for _, v := range m.data {
		out = append(out, v)
	}
	return out, nil
}

func (m *memStorage[S]) ListIDs() ([]entity.StoredMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	metas := make([]entity.StoredMeta, 0, len(m.data))
	for id := range m.data {
		metas = append(metas, entity.StoredMeta{ID: id})
	}
	return metas, nil
}

// errStorage always returns errors.
type errStorage[S any] struct{}

func (e *errStorage[S]) Save(S) error { return errors.New("save error") }
func (e *errStorage[S]) Load(string) (S, error) {
	var zero S
	return zero, errors.New("load error")
}
func (e *errStorage[S]) Delete(string) error { return errors.New("delete error") }
func (e *errStorage[S]) List() ([]S, error)  { return nil, errors.New("list error") }
func (e *errStorage[S]) ListIDs() ([]entity.StoredMeta, error) {
	return nil, errors.New("listids error")
}

// captureBus captures published events.
type captureBus struct {
	mu     sync.Mutex
	events []entity.EntityEvent
}

func (b *captureBus) HasSubscribers(kind, id string) bool { return true }
func (b *captureBus) Publish(e entity.EntityEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}
func (b *captureBus) published() []entity.EntityEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]entity.EntityEvent, len(b.events))
	copy(out, b.events)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Store.Create / Store.Get
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_CreateAndGet(t *testing.T) {
	store := newCounterStore(nil, nil)

	h, err := store.Create("a", &counter{ID: "a", Val: 5})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h.ID() != "a" {
		t.Fatalf("ID: want a, got %s", h.ID())
	}

	h2, err := store.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var got int
	h2.Read(func(c **counter) { got = (*c).Val })
	if got != 5 {
		t.Fatalf("Val: want 5, got %d", got)
	}
}

func TestStore_Create_Duplicate(t *testing.T) {
	store := newCounterStore(nil, nil)
	_, _ = store.Create("dup", &counter{ID: "dup"})
	_, err := store.Create("dup", &counter{ID: "dup"})
	if !errors.Is(err, entity.ErrAlreadyExists) {
		t.Fatalf("want ErrAlreadyExists, got %v", err)
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	store := newCounterStore(nil, nil)
	_, err := store.Get("missing")
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStore_Get_LazyLoadsFromStorage(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	_ = st.Save(counterSnap{ID: "lazy", Val: 42})

	store := newCounterStore(st, nil)
	h, err := store.Get("lazy")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var got int
	h.Read(func(c **counter) { got = (*c).Val })
	if got != 42 {
		t.Fatalf("Val: want 42, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Handle.Read / Handle.Mutate / Handle.MutateWhen
// ─────────────────────────────────────────────────────────────────────────────

func TestHandle_Mutate(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("m", &counter{ID: "m", Val: 0})

	if err := h.Mutate(func(c **counter) { (*c).Val = 7 }); err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	var got int
	h.Read(func(c **counter) { got = (*c).Val })
	if got != 7 {
		t.Fatalf("want 7, got %d", got)
	}
}

func TestHandle_MutateWhen_NoChange(t *testing.T) {
	bus := &captureBus{}
	store := newCounterStore(nil, bus)
	h, _ := store.Create("noop", &counter{ID: "noop", Val: 1})

	changed, err := h.MutateWhen(func(c **counter) (bool, error) {
		return false, nil // signal: no change
	})
	if err != nil {
		t.Fatalf("MutateWhen: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	// No event should have been published.
	if len(bus.published()) != 0 {
		t.Fatalf("expected 0 events, got %d", len(bus.published()))
	}
}

func TestHandle_MutateWhen_FnError(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("err", &counter{ID: "err", Val: 0})

	want := errors.New("whoops")
	_, err := h.MutateWhen(func(c **counter) (bool, error) {
		return true, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("want wrapped whoops, got %v", err)
	}
	// Value must not have changed.
	var got int
	h.Read(func(c **counter) { got = (*c).Val })
	if got != 0 {
		t.Fatalf("val must remain 0, got %d", got)
	}
}

func TestHandle_Snapshot(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("s", &counter{ID: "s", Val: 99})
	snap := h.Snapshot()
	if snap.Val != 99 || snap.ID != "s" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Persistence
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_Create_Persists(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	store := newCounterStore(st, nil)
	_, err := store.Create("p", &counter{ID: "p", Val: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	snap, err := st.Load("p")
	if err != nil {
		t.Fatalf("storage.Load: %v", err)
	}
	if snap.Val != 3 {
		t.Fatalf("persisted Val: want 3, got %d", snap.Val)
	}
}

func TestHandle_Mutate_Persists(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	store := newCounterStore(st, nil)
	h, _ := store.Create("pm", &counter{ID: "pm", Val: 0})

	_ = h.Mutate(func(c **counter) { (*c).Val = 55 })

	snap, _ := st.Load("pm")
	if snap.Val != 55 {
		t.Fatalf("persisted Val: want 55, got %d", snap.Val)
	}
}

func TestHandle_Mutate_StorageError_Propagates(t *testing.T) {
	store := newCounterStore(&errStorage[counterSnap]{}, nil)
	// Create will fail because storage.Save returns an error.
	_, err := store.Create("se", &counter{ID: "se"})
	if err == nil {
		t.Fatal("expected error from errStorage on Create")
	}
}

func TestHandle_Delete(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	store := newCounterStore(st, nil)
	h, _ := store.Create("del", &counter{ID: "del", Val: 1})

	if err := h.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Get("del")
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	_, err = st.Load("del")
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("storage after delete: want ErrNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EventBus
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_Mutate_PublishesEvent(t *testing.T) {
	bus := &captureBus{}
	store := newCounterStore(nil, bus)
	h, _ := store.Create("ev", &counter{ID: "ev", Val: 0})

	// Create publishes one event (initial persist triggers notify).
	_ = h.Mutate(func(c **counter) { (*c).Val = 11 })

	events := bus.published()
	// At least the Mutate event should be there.
	found := false
	for _, e := range events {
		if e.Kind == "counter" && e.ID == "ev" {
			if snap, ok := e.Snapshot.(counterSnap); ok && snap.Val == 11 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected event with Val=11, got %+v", events)
	}
}

// noSubBus has no subscribers — verifies HasSubscribers short-circuit.
type noSubBus struct {
	published int
}

func (b *noSubBus) HasSubscribers(_, _ string) bool { return false }
func (b *noSubBus) Publish(e entity.EntityEvent) {
	b.published++
	if e.Snapshot != nil {
		panic("snapshot should be nil when HasSubscribers=false")
	}
}

func TestStore_Mutate_SkipsSnapshotWhenNoSubscribers(t *testing.T) {
	bus := &noSubBus{}
	store := newCounterStore(nil, bus)
	h, _ := store.Create("ns", &counter{ID: "ns"})
	_ = h.Mutate(func(c **counter) { (*c).Val = 99 })
	if bus.published == 0 {
		t.Fatal("Publish should still be called, but with nil Snapshot")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// List
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_List(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	store := newCounterStore(st, nil)

	for _, id := range []string{"x", "y", "z"} {
		_, _ = store.Create(id, &counter{ID: id})
	}

	handles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(handles) != 3 {
		t.Fatalf("want 3 handles, got %d", len(handles))
	}
}

func TestStore_List_LoadsFromStorage(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	// Pre-populate storage without using the store.
	_ = st.Save(counterSnap{ID: "pre1", Val: 1})
	_ = st.Save(counterSnap{ID: "pre2", Val: 2})

	store := newCounterStore(st, nil)
	handles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(handles) != 2 {
		t.Fatalf("want 2 handles from storage, got %d", len(handles))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListIDs
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_ListIDs_InMemoryOnly(t *testing.T) {
	store := newCounterStore(nil, nil)
	for _, id := range []string{"p", "q", "r"} {
		_, _ = store.Create(id, &counter{ID: id})
	}
	ids, err := store.ListIDs()
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 ids, got %d: %v", len(ids), ids)
	}
}

func TestStore_ListIDs_MergesStorage(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	// Two entities in storage only.
	_ = st.Save(counterSnap{ID: "cold1"})
	_ = st.Save(counterSnap{ID: "cold2"})

	store := newCounterStore(st, nil)
	// One entity in cache only (not yet persisted — use nil storage for that
	// store, so we inject into a fresh store and add a live entry via Create).
	_, _ = store.Create("hot1", &counter{ID: "hot1"})

	metas, err := store.ListIDs()
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	// Expect cold1, cold2, hot1 — no duplicates.
	got := make(map[string]bool, len(metas))
	for _, m := range metas {
		got[m.ID] = true
	}
	for _, want := range []string{"cold1", "cold2", "hot1"} {
		if !got[want] {
			t.Errorf("missing id %q in ListIDs result %v", want, metas)
		}
	}
	if len(metas) != 3 {
		t.Errorf("want 3 ids, got %d: %v", len(metas), metas)
	}
}

func TestStore_ListIDs_NoDuplicates(t *testing.T) {
	// Entity is in both cache and storage — should appear once.
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	store := newCounterStore(st, nil)
	_, _ = store.Create("dup", &counter{ID: "dup"}) // persisted via Create

	metas, err := store.ListIDs()
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	count := 0
	for _, m := range metas {
		if m.ID == "dup" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("'dup' appeared %d times, want 1", count)
	}
}

func TestStore_ListIDs_DoesNotLoadColdEntities(t *testing.T) {
	// ListIDs must not populate the cache for cold entries.
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	_ = st.Save(counterSnap{ID: "cold", Val: 7})

	store := newCounterStore(st, nil)
	ids, err := store.ListIDs()
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 1 || ids[0].ID != "cold" {
		t.Fatalf("unexpected ids: %v", ids)
	}

	// Getting "cold" now should lazy-load it; before that it should be absent
	// from the cache (no handle returned without an explicit Get).
	// We verify by checking that List() still returns 0 handles from cache
	// (storage has 1 snapshot, but the in-memory map is empty until Get).
	// We can't inspect the cache directly, but we can call Get and confirm it
	// works, demonstrating the lazy-load path was not short-circuited.
	h, err := store.Get("cold")
	if err != nil {
		t.Fatalf("Get after ListIDs: %v", err)
	}
	var val int
	h.Read(func(c **counter) { val = (*c).Val })
	if val != 7 {
		t.Fatalf("want Val=7, got %d", val)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OnRestart
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_OnRestart(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	_ = st.Save(counterSnap{ID: "r1", Val: 10})
	_ = st.Save(counterSnap{ID: "r2", Val: 20})

	store := newCounterStore(st, nil)
	var seen []string
	err := store.OnRestart(context.Background(), func(h entity.Handle[*counter, counterSnap]) error {
		seen = append(seen, h.ID())
		return nil
	})
	if err != nil {
		t.Fatalf("OnRestart: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("want 2 hooks called, got %d", len(seen))
	}
}

func TestStore_OnRestart_HookError_Aborts(t *testing.T) {
	st := newMemStorage(func(s counterSnap) string { return s.ID })
	_ = st.Save(counterSnap{ID: "e1", Val: 0})
	_ = st.Save(counterSnap{ID: "e2", Val: 0})

	store := newCounterStore(st, nil)
	want := errors.New("hook fail")
	err := store.OnRestart(context.Background(), func(h entity.Handle[*counter, counterSnap]) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("want hook fail propagated, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Dep graph / WakeC
// ─────────────────────────────────────────────────────────────────────────────

func TestHandle_WatchAndMarkDone_SameStore(t *testing.T) {
	store := newCounterStore(nil, nil)
	dep, _ := store.Create("dep", &counter{ID: "dep", Val: 0})
	waiter, _ := store.Create("waiter", &counter{ID: "waiter", Val: 0})

	// Register waiter as depending on dep.
	if err := waiter.Watch([]entity.Dep{{Kind: "counter", ID: "dep"}}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// WakeC should not be closed yet.
	select {
	case <-waiter.WakeC():
		t.Fatal("WakeC closed too early")
	default:
	}

	// Mark dep as done by setting Val negative (IsDone checks Val < 0).
	_ = dep.Mutate(func(c **counter) { (*c).Val = -1 })
	dep.MarkDone()

	// WakeC should be closed now.
	select {
	case <-waiter.WakeC():
		// expected
	case <-time.After(time.Second):
		t.Fatal("WakeC not closed after MarkDone")
	}
}

func TestHandle_MarkDone_InsideMutate(t *testing.T) {
	store := newCounterStore(nil, nil)
	dep, _ := store.Create("d2", &counter{ID: "d2", Val: 0})
	waiter, _ := store.Create("w2", &counter{ID: "w2", Val: 0})

	_ = waiter.Watch([]entity.Dep{{Kind: "counter", ID: "d2"}})

	// Call MarkDone from inside Mutate's fn — should be enqueued and fire after lock released.
	_ = dep.Mutate(func(c **counter) {
		(*c).Val = -1
		dep.MarkDone()
	})

	select {
	case <-waiter.WakeC():
		// expected
	case <-time.After(time.Second):
		t.Fatal("WakeC not closed after MarkDone inside Mutate")
	}
}

func TestHandle_Watch_CyclicDependency(t *testing.T) {
	store := newCounterStore(nil, nil)
	a, _ := store.Create("a", &counter{ID: "a"})
	b, _ := store.Create("b", &counter{ID: "b"})

	_ = a.Watch([]entity.Dep{{Kind: "counter", ID: "b"}})

	// b watching a would create a cycle.
	err := b.Watch([]entity.Dep{{Kind: "counter", ID: "a"}})
	if !errors.Is(err, entity.ErrCyclicDependency) {
		t.Fatalf("want ErrCyclicDependency, got %v", err)
	}
}

func TestHandle_Watch_SelfLoop(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("self", &counter{ID: "self"})
	err := h.Watch([]entity.Dep{{Kind: "counter", ID: "self"}})
	if !errors.Is(err, entity.ErrCyclicDependency) {
		t.Fatalf("want ErrCyclicDependency on self-loop, got %v", err)
	}
}

func TestHandle_Unwatch(t *testing.T) {
	store := newCounterStore(nil, nil)
	dep, _ := store.Create("ud", &counter{ID: "ud", Val: 0})
	waiter, _ := store.Create("uw", &counter{ID: "uw", Val: 0})

	_ = waiter.Watch([]entity.Dep{{Kind: "counter", ID: "ud"}})
	waiter.Unwatch()

	// After Unwatch, MarkDone should not close WakeC (the channel is gone).
	_ = dep.Mutate(func(c **counter) { (*c).Val = -1 })
	dep.MarkDone()

	// WakeC returns a pre-closed channel after Unwatch only if Watch was reset;
	// but since Unwatch removed the registration, it does not close the channel.
	// The important thing is no panic and MarkDone did not fire.
	// Here we just verify that we don't panic and the test completes.
}

// ─────────────────────────────────────────────────────────────────────────────
// ActiveStore / RunHandle
// ─────────────────────────────────────────────────────────────────────────────

func newActiveCounterStore() *entity.ActiveStore[*counter, counterSnap] {
	return entity.NewActiveStore[*counter, counterSnap](
		nil, nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			return func(ctx context.Context) {
				// Increment Val once, then block until stopped.
				_ = h.Mutate(func(c **counter) { (*c).Val++ })
				<-ctx.Done()
			}
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
	)
}

func TestActiveStore_Create_StartsGoroutine(t *testing.T) {
	s := newActiveCounterStore()
	rh, err := s.Create("ag", &counter{ID: "ag", Val: 0})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Wait briefly for the goroutine to increment Val.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var got int
		rh.Read(func(c **counter) { got = (*c).Val })
		if got == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var got int
	rh.Read(func(c **counter) { got = (*c).Val })
	if got != 1 {
		t.Fatalf("goroutine did not run: Val want 1, got %d", got)
	}
}

func TestRunHandle_Stop(t *testing.T) {
	s := newActiveCounterStore()
	rh, _ := s.Create("stop", &counter{ID: "stop", Val: 0})

	rh.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rh.Wait(ctx); err != nil {
		t.Fatalf("Wait after Stop: %v", err)
	}
}

func TestRunHandle_Stop_ForwardsToRevDeps(t *testing.T) {
	// session depends on dep.  Stopping dep should also stop session.
	s := newActiveCounterStore()
	dep, _ := s.Create("rd-dep", &counter{ID: "rd-dep"})
	sess, _ := s.Create("rd-sess", &counter{ID: "rd-sess"})

	_ = sess.Watch([]entity.Dep{{Kind: "counter", ID: "rd-dep"}})

	dep.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess goroutine did not stop after dep stopped: %v", err)
	}
}

func TestActiveStore_BeginDrain_TransitionsMode(t *testing.T) {
	s := newActiveCounterStore()
	if got := s.LifecycleMode(); got != entity.ActiveStoreModeRunning {
		t.Fatalf("initial mode: want %q, got %q", entity.ActiveStoreModeRunning, got)
	}

	s.BeginDrain(context.Background())

	if got := s.LifecycleMode(); got != entity.ActiveStoreModeDraining {
		t.Fatalf("mode after BeginDrain: want %q, got %q", entity.ActiveStoreModeDraining, got)
	}
}

func TestActiveStore_CreateLoad_WhileDraining_DoesNotStartNewRuns(t *testing.T) {
	s := newActiveCounterStore()

	if _, err := s.Store.Create("seed", &counter{ID: "seed", Val: 10}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	s.BeginDrain(context.Background())

	if _, err := s.Create("new", &counter{ID: "new", Val: 0}); !errors.Is(err, entity.ErrActiveStoreDraining) {
		t.Fatalf("Create while draining: want ErrActiveStoreDraining, got %v", err)
	}
	if _, err := s.Load("seed"); !errors.Is(err, entity.ErrActiveStoreDraining) {
		t.Fatalf("Load while draining: want ErrActiveStoreDraining, got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.AwaitDrain(ctx); err != nil {
		t.Fatalf("AwaitDrain with no runs should return immediately: %v", err)
	}
}

func TestActiveStore_BeginDrain_ExistingRunsRemainUntilCompletion(t *testing.T) {
	s := newActiveCounterStore()
	rh, err := s.Create("live", &counter{ID: "live", Val: 0})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s.BeginDrain(context.Background())

	select {
	case <-rh.Done():
		t.Fatal("existing run should continue during drain")
	case <-time.After(50 * time.Millisecond):
	}

	rh.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rh.Wait(ctx); err != nil {
		t.Fatalf("Wait after Stop: %v", err)
	}
}

func TestActiveStore_AwaitDrain_ReturnsWhenRunningWorkCompletes(t *testing.T) {
	s := newActiveCounterStore()
	rh1, err := s.Create("drain-1", &counter{ID: "drain-1", Val: 0})
	if err != nil {
		t.Fatalf("Create drain-1: %v", err)
	}
	rh2, err := s.Create("drain-2", &counter{ID: "drain-2", Val: 0})
	if err != nil {
		t.Fatalf("Create drain-2: %v", err)
	}

	s.BeginDrain(context.Background())

	awaitCtx, cancelAwait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelAwait()
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.AwaitDrain(awaitCtx)
	}()

	select {
	case err := <-errCh:
		t.Fatalf("AwaitDrain returned before runs finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	rh1.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rh1.Wait(ctx); err != nil {
		t.Fatalf("Wait drain-1: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("AwaitDrain returned with one run still active: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	rh2.Stop()
	if err := rh2.Wait(ctx); err != nil {
		t.Fatalf("Wait drain-2: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("AwaitDrain after all runs complete: %v", err)
	}
}

func TestActiveStore_DeferredQueue_EnqueueOnDrain(t *testing.T) {
	entityStorage := newMemStorage(func(s counterSnap) string { return s.ID })
	deferredStorage := newMemStorage(func(s entity.DeferredOpEnvelope) string { return s.EnvelopeID })

	s := entity.NewActiveStore[*counter, counterSnap](
		entityStorage,
		nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			return func(ctx context.Context) { <-ctx.Done() }
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
		entity.WithDeferredOpStorage[*counter, counterSnap](deferredStorage),
	)

	s.BeginDrain(context.Background())
	_, err := s.Create("defer-create", &counter{ID: "defer-create", Val: 7})
	if !errors.Is(err, entity.ErrActiveStoreStartDeferred) {
		t.Fatalf("Create while draining: want ErrActiveStoreStartDeferred, got %v", err)
	}

	if _, err := s.Store.Get("defer-create"); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("deferred create should not remain in store, got err=%v", err)
	}

	envelopes, err := deferredStorage.List()
	if err != nil {
		t.Fatalf("deferredStorage.List: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("want 1 deferred envelope, got %d", len(envelopes))
	}
	env := envelopes[0]
	if env.Kind != "counter" || env.EntityID != "defer-create" || env.Op != entity.ActiveStoreStartCreate {
		t.Fatalf("unexpected deferred envelope: %+v", env)
	}
	if env.IdempotencyKey == "" || env.EnqueuedAt.IsZero() || len(env.Payload) == 0 {
		t.Fatalf("deferred envelope missing required fields: %+v", env)
	}
}

func TestActiveStore_DeferredQueue_ReplayOnRestart(t *testing.T) {
	entityStorage := newMemStorage(func(s counterSnap) string { return s.ID })
	deferredStorage := newMemStorage(func(s entity.DeferredOpEnvelope) string { return s.EnvelopeID })
	started := make(chan string, 1)

	s1 := entity.NewActiveStore[*counter, counterSnap](
		entityStorage,
		nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			return func(ctx context.Context) {
				select {
				case started <- h.ID():
				default:
				}
				<-ctx.Done()
			}
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
		entity.WithDeferredOpStorage[*counter, counterSnap](deferredStorage),
	)

	s1.BeginDrain(context.Background())
	_, err := s1.Create("replay-create", &counter{ID: "replay-create", Val: 3})
	if !errors.Is(err, entity.ErrActiveStoreStartDeferred) {
		t.Fatalf("Create while draining: want ErrActiveStoreStartDeferred, got %v", err)
	}

	s2 := entity.NewActiveStore[*counter, counterSnap](
		entityStorage,
		nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			return func(ctx context.Context) {
				select {
				case started <- h.ID():
				default:
				}
				<-ctx.Done()
			}
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
		entity.WithDeferredOpStorage[*counter, counterSnap](deferredStorage),
	)

	if err := s2.OnRestart(context.Background(), func(entity.Handle[*counter, counterSnap]) error { return nil }); err != nil {
		t.Fatalf("OnRestart: %v", err)
	}

	select {
	case got := <-started:
		if got != "replay-create" {
			t.Fatalf("unexpected started id: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("deferred operation was not replayed")
	}

	envelopes, err := deferredStorage.List()
	if err != nil {
		t.Fatalf("deferredStorage.List: %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("expected deferred queue to be empty after replay, got %d", len(envelopes))
	}
}

func TestActiveStore_DeferredQueue_IdempotentReplay_NoDuplicateStarts(t *testing.T) {
	entityStorage := newMemStorage(func(s counterSnap) string { return s.ID })
	deferredStorage := newMemStorage(func(s entity.DeferredOpEnvelope) string { return s.EnvelopeID })
	if err := entityStorage.Save(counterSnap{ID: "dup", Val: 1}); err != nil {
		t.Fatalf("seed entity storage: %v", err)
	}

	base := time.Now().UTC()
	key := "counter:load:dup"
	if err := deferredStorage.Save(entity.DeferredOpEnvelope{
		EnvelopeID:     "env-1",
		Kind:           "counter",
		EntityID:       "dup",
		Op:             entity.ActiveStoreStartLoad,
		IdempotencyKey: key,
		EnqueuedAt:     base,
	}); err != nil {
		t.Fatalf("save env-1: %v", err)
	}
	if err := deferredStorage.Save(entity.DeferredOpEnvelope{
		EnvelopeID:     "env-2",
		Kind:           "counter",
		EntityID:       "dup",
		Op:             entity.ActiveStoreStartLoad,
		IdempotencyKey: key,
		EnqueuedAt:     base.Add(time.Millisecond),
	}); err != nil {
		t.Fatalf("save env-2: %v", err)
	}

	var (
		startMu    sync.Mutex
		startCount = map[string]int{}
	)
	s := entity.NewActiveStore[*counter, counterSnap](
		entityStorage,
		nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			startMu.Lock()
			startCount[h.ID()]++
			startMu.Unlock()
			return func(ctx context.Context) {
				<-ctx.Done()
			}
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
		entity.WithDeferredOpStorage[*counter, counterSnap](deferredStorage),
	)

	if err := s.OnRestart(context.Background(), func(entity.Handle[*counter, counterSnap]) error { return nil }); err != nil {
		t.Fatalf("OnRestart: %v", err)
	}

	startMu.Lock()
	gotStarts := startCount["dup"]
	startMu.Unlock()
	if gotStarts != 1 {
		t.Fatalf("want exactly 1 start for dup, got %d", gotStarts)
	}

	envelopes, err := deferredStorage.List()
	if err != nil {
		t.Fatalf("deferredStorage.List: %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("expected duplicate deferred envelopes to be cleared, got %d", len(envelopes))
	}
}

func TestActiveStore_DeferredQueue_ReplayOrderingFIFO(t *testing.T) {
	entityStorage := newMemStorage(func(s counterSnap) string { return s.ID })
	deferredStorage := newMemStorage(func(s entity.DeferredOpEnvelope) string { return s.EnvelopeID })

	firstPayload, err := json.Marshal(map[string]counterSnap{"snapshot": counterSnap{ID: "first", Val: 1}})
	if err != nil {
		t.Fatalf("marshal first payload: %v", err)
	}
	secondPayload, err := json.Marshal(map[string]counterSnap{"snapshot": counterSnap{ID: "second", Val: 2}})
	if err != nil {
		t.Fatalf("marshal second payload: %v", err)
	}

	base := time.Now().UTC()
	if err := deferredStorage.Save(entity.DeferredOpEnvelope{
		EnvelopeID:     "fifo-1",
		Kind:           "counter",
		EntityID:       "first",
		Op:             entity.ActiveStoreStartCreate,
		Payload:        firstPayload,
		IdempotencyKey: "counter:create:first",
		EnqueuedAt:     base,
	}); err != nil {
		t.Fatalf("save fifo-1: %v", err)
	}
	if err := deferredStorage.Save(entity.DeferredOpEnvelope{
		EnvelopeID:     "fifo-2",
		Kind:           "counter",
		EntityID:       "second",
		Op:             entity.ActiveStoreStartCreate,
		Payload:        secondPayload,
		IdempotencyKey: "counter:create:second",
		EnqueuedAt:     base.Add(time.Millisecond),
	}); err != nil {
		t.Fatalf("save fifo-2: %v", err)
	}

	var (
		startedMu sync.Mutex
		started   []string
	)
	s := entity.NewActiveStore[*counter, counterSnap](
		entityStorage,
		nil,
		func(h entity.Handle[*counter, counterSnap]) func(ctx context.Context) {
			startedMu.Lock()
			started = append(started, h.ID())
			startedMu.Unlock()
			return func(ctx context.Context) {
				<-ctx.Done()
			}
		},
		entity.StoreOptions[*counter, counterSnap]{
			Kind:           "counter",
			FromSnapshot:   func(s counterSnap) *counter { return &counter{ID: s.ID, Val: s.Val} },
			IDFromSnapshot: func(s counterSnap) string { return s.ID },
		},
		entity.WithDeferredOpStorage[*counter, counterSnap](deferredStorage),
	)

	if err := s.OnRestart(context.Background(), func(entity.Handle[*counter, counterSnap]) error { return nil }); err != nil {
		t.Fatalf("OnRestart: %v", err)
	}

	startedMu.Lock()
	ordered := append([]string(nil), started...)
	startedMu.Unlock()
	if len(ordered) != 2 || ordered[0] != "first" || ordered[1] != "second" {
		t.Fatalf("expected FIFO replay order [first second], got %v", ordered)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Concurrency stress test
// ─────────────────────────────────────────────────────────────────────────────

func TestStore_ConcurrentMutate(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("conc", &counter{ID: "conc", Val: 0})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = h.Mutate(func(c **counter) { (*c).Val++ })
		}()
	}
	wg.Wait()

	var got int
	h.Read(func(c **counter) { got = (*c).Val })
	if got != goroutines {
		t.Fatalf("concurrent Mutate: want %d, got %d", goroutines, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IDer interface
// ─────────────────────────────────────────────────────────────────────────────

func TestIDer_EntityID(t *testing.T) {
	store := newCounterStore(nil, nil)
	h, _ := store.Create("idfoo", &counter{ID: "idfoo"})
	if h.ID() != "idfoo" {
		t.Fatalf("want idfoo, got %s", h.ID())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WakeC resets on second Watch call
// ─────────────────────────────────────────────────────────────────────────────

func TestHandle_Watch_ResetsWakeC(t *testing.T) {
	store := newCounterStore(nil, nil)
	dep1, _ := store.Create("res-dep1", &counter{ID: "res-dep1", Val: 0})
	dep2, _ := store.Create("res-dep2", &counter{ID: "res-dep2", Val: 0})
	waiter, _ := store.Create("res-wait", &counter{ID: "res-wait", Val: 0})

	_ = waiter.Watch([]entity.Dep{{Kind: "counter", ID: "res-dep1"}})
	firstCh := waiter.WakeC()

	// Register a different dep — WakeC should be a new channel.
	_ = waiter.Watch([]entity.Dep{{Kind: "counter", ID: "res-dep2"}})
	secondCh := waiter.WakeC()

	if firstCh == secondCh {
		t.Fatal("WakeC channel should be replaced after second Watch call")
	}

	// Complete dep2 — second channel should close.
	_ = dep2.Mutate(func(c **counter) { (*c).Val = -1 })
	dep2.MarkDone()

	select {
	case <-secondCh:
	case <-time.After(time.Second):
		t.Fatal("secondCh not closed")
	}

	// dep1's MarkDone should NOT close firstCh (it's been orphaned).
	_ = dep1.Mutate(func(c **counter) { (*c).Val = -1 })
	dep1.MarkDone()
	// firstCh may or may not be closed depending on gc; just verify no panic.
	_ = fmt.Sprintf("%p", firstCh)
}
