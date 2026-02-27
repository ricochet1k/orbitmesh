package entity

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DeferredOpEnvelope is a durable, generic envelope for deferred ActiveStore operations.
type DeferredOpEnvelope struct {
	EnvelopeID     string             `json:"envelope_id"`
	Kind           string             `json:"kind"`
	EntityID       string             `json:"entity_id"`
	Op             ActiveStoreStartOp `json:"op"`
	Payload        json.RawMessage    `json:"payload,omitempty"`
	IdempotencyKey string             `json:"idempotency_key"`
	EnqueuedAt     time.Time          `json:"enqueued_at"`
}

type deferredCreatePayload[S any] struct {
	Snapshot S `json:"snapshot"`
}

func deferredEnvelopeID(key string, enqueuedAt time.Time) string {
	return fmt.Sprintf("%s:%d", key, enqueuedAt.UTC().UnixNano())
}

func deferredIdempotencyKey(kind string, op ActiveStoreStartOp, id string) string {
	return fmt.Sprintf("%s:%s:%s", kind, op, id)
}

type deferredIndex struct {
	once  sync.Once
	err   error
	byKey map[string]string // idempotency key -> envelope ID
}

func (s *ActiveStore[T, S]) initDeferredIndexLocked() error {
	s.deferredIndex.once.Do(func() {
		s.deferredIndex.byKey = make(map[string]string)
		if s.deferredStorage == nil {
			return
		}
		envelopes, err := s.deferredStorage.List()
		if err != nil {
			s.deferredIndex.err = fmt.Errorf("entity.ActiveStore: list deferred ops: %w", err)
			return
		}
		for _, env := range envelopes {
			if env.Kind != s.opts.Kind {
				continue
			}
			if env.IdempotencyKey == "" || env.EnvelopeID == "" {
				continue
			}
			s.deferredIndex.byKey[env.IdempotencyKey] = env.EnvelopeID
		}
	})
	return s.deferredIndex.err
}

func (s *ActiveStore[T, S]) enqueueDeferredStart(op ActiveStoreStartOp, id string, payload json.RawMessage) error {
	if s.deferredStorage == nil {
		return nil
	}

	s.deferredMu.Lock()
	defer s.deferredMu.Unlock()

	if err := s.initDeferredIndexLocked(); err != nil {
		return err
	}

	key := deferredIdempotencyKey(s.opts.Kind, op, id)
	if _, exists := s.deferredIndex.byKey[key]; exists {
		return nil
	}

	enqueuedAt := time.Now().UTC()
	env := DeferredOpEnvelope{
		EnvelopeID:     deferredEnvelopeID(key, enqueuedAt),
		Kind:           s.opts.Kind,
		EntityID:       id,
		Op:             op,
		Payload:        payload,
		IdempotencyKey: key,
		EnqueuedAt:     enqueuedAt,
	}
	if err := s.deferredStorage.Save(env); err != nil {
		return fmt.Errorf("entity.ActiveStore: save deferred op %s/%s: %w", op, id, err)
	}
	s.deferredIndex.byKey[key] = env.EnvelopeID
	return nil
}

func (s *ActiveStore[T, S]) clearDeferredEnvelope(env DeferredOpEnvelope) {
	if s.deferredStorage == nil || env.EnvelopeID == "" {
		return
	}
	_ = s.deferredStorage.Delete(env.EnvelopeID)

	s.deferredMu.Lock()
	defer s.deferredMu.Unlock()
	if s.deferredIndex.byKey == nil {
		return
	}
	if currentID, ok := s.deferredIndex.byKey[env.IdempotencyKey]; ok && currentID == env.EnvelopeID {
		delete(s.deferredIndex.byKey, env.IdempotencyKey)
	}
}

func (s *ActiveStore[T, S]) replayDeferredOps() error {
	if s.deferredStorage == nil {
		return nil
	}

	filtered, err := s.listDeferredOps()
	if err != nil {
		return err
	}

	processedKeys := make(map[string]struct{}, len(filtered))
	for _, env := range filtered {
		if _, seen := processedKeys[env.IdempotencyKey]; seen {
			s.clearDeferredEnvelope(env)
			continue
		}

		err := func() error {
			s.setReplayPending(env)
			defer s.clearReplayPending()
			return s.replayDeferredEnvelope(env)
		}()
		if err != nil {
			if isStartDeferredErr(err) {
				continue
			}
			return err
		}
		processedKeys[env.IdempotencyKey] = struct{}{}
		s.clearDeferredEnvelope(env)
	}

	return nil
}

func (s *ActiveStore[T, S]) listDeferredOps() ([]DeferredOpEnvelope, error) {
	if s.deferredStorage == nil {
		return nil, nil
	}

	envelopes, err := s.deferredStorage.List()
	if err != nil {
		return nil, fmt.Errorf("entity.ActiveStore: list deferred ops: %w", err)
	}

	filtered := make([]DeferredOpEnvelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env.Kind == s.opts.Kind {
			filtered = append(filtered, env)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].EnqueuedAt.Equal(filtered[j].EnqueuedAt) {
			return filtered[i].EnvelopeID < filtered[j].EnvelopeID
		}
		return filtered[i].EnqueuedAt.Before(filtered[j].EnqueuedAt)
	})

	return filtered, nil
}

func (s *ActiveStore[T, S]) setReplayPending(env DeferredOpEnvelope) {
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	copyEnv := env
	s.replayPending = &copyEnv
	s.replayPendingSince = time.Now().UTC()
}

func (s *ActiveStore[T, S]) clearReplayPending() {
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	s.replayPending = nil
	s.replayPendingSince = time.Time{}
}

func (s *ActiveStore[T, S]) replayDeferredEnvelope(env DeferredOpEnvelope) error {
	switch env.Op {
	case ActiveStoreStartCreate:
		return s.replayDeferredCreate(env)
	case ActiveStoreStartLoad, ActiveStoreStartRestart:
		h, err := s.Store.Get(env.EntityID)
		if err != nil {
			if env.Op == ActiveStoreStartLoad {
				return nil
			}
			return fmt.Errorf("entity.ActiveStore.replayDeferredEnvelope: load %s: %w", env.EntityID, err)
		}
		_, err = s.startRun(env.Op, h)
		return err
	default:
		return fmt.Errorf("entity.ActiveStore.replayDeferredEnvelope: unknown op %q", env.Op)
	}
}

func (s *ActiveStore[T, S]) replayDeferredCreate(env DeferredOpEnvelope) error {
	if _, err := s.Store.Get(env.EntityID); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("entity.ActiveStore.replayDeferredCreate: precheck %s: %w", env.EntityID, err)
		}
		if env.Payload == nil {
			return nil
		}
		if s.opts.FromSnapshot == nil {
			return fmt.Errorf("entity.ActiveStore.replayDeferredCreate: FromSnapshot is nil for %s", env.EntityID)
		}
		var payload deferredCreatePayload[S]
		if unmarshalErr := json.Unmarshal(env.Payload, &payload); unmarshalErr != nil {
			return fmt.Errorf("entity.ActiveStore.replayDeferredCreate: decode payload %s: %w", env.EntityID, unmarshalErr)
		}
		if _, createErr := s.Store.Create(env.EntityID, s.opts.FromSnapshot(payload.Snapshot)); createErr != nil && !errors.Is(createErr, ErrAlreadyExists) {
			return fmt.Errorf("entity.ActiveStore.replayDeferredCreate: create %s: %w", env.EntityID, createErr)
		}
	}

	h, err := s.Store.Get(env.EntityID)
	if err != nil {
		return fmt.Errorf("entity.ActiveStore.replayDeferredCreate: get %s: %w", env.EntityID, err)
	}
	_, err = s.startRun(ActiveStoreStartCreate, h)
	return err
}
