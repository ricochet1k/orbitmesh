// Package entity provides a single, reusable abstraction for durable objects in
// this codebase. It replaces the per-type lock/store/notify patterns scattered
// across service, storage, and toolcall with a consistent Store[T,S] + Handle[T,S]
// model that serialises mutations, persists snapshots, fires pub/sub events, and
// tracks dependency wakeup — all without holding storage locks.
//
// # Quick-start
//
//	type MyEntity struct { ... }
//	type MySnapshot struct { ... }
//	func (e *MyEntity) Snapshot() MySnapshot { return MySnapshot{...} }
//
//	store := entity.NewStore[*MyEntity, MySnapshot](storage, bus, entity.StoreOptions[*MyEntity, MySnapshot]{Kind: "my"})
//	h, _ := store.Create("id-1", &MyEntity{})
//	h.Mutate(func(e **MyEntity) { (*e).Field = "new" })
package entity

import (
	"errors"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Core interfaces
// ────────────────────────────────────────────────────────────────────────────

// Snapshotter is implemented by the live entity type T.  Snapshot() returns a
// serialisable, deep-copy-safe value of type S.  It is called while the
// entity write-lock is held, so it must not block or re-enter the store.
type Snapshotter[S any] interface {
	Snapshot() S
}

// TypedStorage handles persistence of snapshot values.  Save and Load operate
// on the serialisable type S, never on the live T.
type TypedStorage[S any] interface {
	Save(s S) error
	Load(id string) (S, error)
	Delete(id string) error
	// List returns the full snapshot for every persisted entity.
	// Used by OnRestart (which needs to reconstruct live T) and by List()
	// (which warms the cache).  Prefer ListIDs when only the IDs are needed.
	List() ([]S, error)
	// ListIDs returns the stable identifiers of every persisted entity without
	// reading or parsing any entity file.  Implementations should satisfy this
	// with a directory scan or equivalent index read — O(1) per entry, no
	// per-entity I/O.
	ListIDs() ([]string, error)
}

// EventBus receives change notifications after every successful Mutate.
// HasSubscribers is checked before calling T.Snapshot() so the snapshot
// allocation is skipped when no one is listening.
type EventBus interface {
	HasSubscribers(kind, id string) bool
	Publish(event EntityEvent)
}

// EntityEvent is the payload delivered to EventBus.Publish after each mutation.
// Snapshot is nil when HasSubscribers returned false for this entity.
type EntityEvent struct {
	Kind      string
	ID        string
	Snapshot  any
	Timestamp time.Time
}

// ────────────────────────────────────────────────────────────────────────────
// Dep tracking
// ────────────────────────────────────────────────────────────────────────────

// Dep identifies another entity that a handle is waiting on.
// Kind is the entity kind string (e.g. "eval", "session").
// ID is the entity's stable identifier.
type Dep struct {
	Kind string
	ID   string
}

// DepSource is an optional interface for entity types that declare
// dependencies on other entities.  When the store loads T from storage and
// T implements DepSource, it calls Deps() to re-register the wakeup watch
// automatically (needed for crash-recovery).
type DepSource interface {
	// Deps returns the dependencies this entity is currently waiting on.
	// Called on cold load; must not mutate state.
	Deps() []Dep
	// IsDone reports whether this entity has already reached a terminal state.
	// Used during recovery to skip re-registering watches for finished entities.
	IsDone() bool
}

// ────────────────────────────────────────────────────────────────────────────
// Sentinel errors
// ────────────────────────────────────────────────────────────────────────────

var (
	// ErrNotFound is returned when an entity does not exist in the store or storage.
	ErrNotFound = errors.New("entity not found")

	// ErrAlreadyExists is returned by Create when an entity with the given ID
	// already exists in the in-memory map.
	ErrAlreadyExists = errors.New("entity already exists")

	// ErrCyclicDependency is returned by Watch when adding the requested deps
	// would introduce a cycle in the dependency graph.
	ErrCyclicDependency = errors.New("cyclic dependency")
)

// ────────────────────────────────────────────────────────────────────────────
// StoreOptions
// ────────────────────────────────────────────────────────────────────────────

// StoreOptions configures a Store at construction time.
type StoreOptions[T Snapshotter[S], S any] struct {
	// Kind is the entity kind string used in EventBus calls and Dep identifiers.
	// It must be non-empty.
	Kind string

	// FromSnapshot reconstructs a live T from a serialisable snapshot S.
	// Required when storage is non-nil (used by Get/List for lazy loading).
	FromSnapshot func(s S) T

	// IDFromSnapshot extracts the stable string ID from a serialisable snapshot.
	// Required when storage is non-nil and List/OnRestart are used.
	// If T implements IDer after FromSnapshot, this may be omitted and the
	// store will fall back to calling EntityID() on the reconstructed value.
	IDFromSnapshot func(s S) string

	// IsDone is an optional function that tests whether a loaded snapshot
	// represents a terminal entity.  Used during dep-graph recovery to decide
	// whether a re-loaded dep is already done.  If nil, no automatic
	// done-on-load firing occurs.
	IsDone func(s S) bool

	// OnDone is called (outside any lock) after MarkDone fires and all revdep
	// watchers have been notified. id is the entity that became done.
	// Used by coordinators to trigger reschedule logic without polling.
	OnDone func(id string)
}

// RestartHook is a function called once per entity during Store.OnRestart.
// It receives a Handle to the entity so it can inspect and correct state
// (e.g. marking interrupted run attempts).
type RestartHook[T Snapshotter[S], S any] func(h Handle[T, S]) error
