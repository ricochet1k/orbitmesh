package entity

import (
	"sort"
	"time"
)

type ActiveStoreWaitReason string

const (
	ActiveStoreWaitReasonRunningBody       ActiveStoreWaitReason = "running body"
	ActiveStoreWaitReasonWaitingDependency ActiveStoreWaitReason = "waiting dependency"
	ActiveStoreWaitReasonDeferredOp        ActiveStoreWaitReason = "deferred op"
	ActiveStoreWaitReasonReplayPending     ActiveStoreWaitReason = "replay pending"
)

type DrainWait struct {
	Reason      ActiveStoreWaitReason `json:"reason"`
	EntityID    string                `json:"entity_id,omitempty"`
	EnvelopeID  string                `json:"envelope_id,omitempty"`
	Op          ActiveStoreStartOp    `json:"op,omitempty"`
	Age         time.Duration         `json:"age"`
	BlockedDeps []Dep                 `json:"blocked_deps,omitempty"`
}

type DrainStatus struct {
	Mode   ActiveStoreLifecycleMode `json:"mode"`
	AsOf   time.Time                `json:"as_of"`
	WaitOn []DrainWait              `json:"wait_on,omitempty"`
	Errors []string                 `json:"errors,omitempty"`
}

// DrainStatus reports what the active store is currently waiting on.
func (s *ActiveStore[T, S]) DrainStatus() DrainStatus {
	now := time.Now().UTC()
	status := DrainStatus{AsOf: now}

	s.runsMu.Lock()
	status.Mode = s.mode
	runs := make(map[string]time.Time, len(s.runs))
	for id, r := range s.runs {
		runs[id] = r.startedAt
	}
	s.runsMu.Unlock()

	for id, startedAt := range runs {
		blocked := s.blockedDeps(id)
		reason := ActiveStoreWaitReasonRunningBody
		if len(blocked) > 0 {
			reason = ActiveStoreWaitReasonWaitingDependency
		}
		status.WaitOn = append(status.WaitOn, DrainWait{
			Reason:      reason,
			EntityID:    id,
			Age:         now.Sub(startedAt),
			BlockedDeps: blocked,
		})
	}

	replayPending, replayPendingSince := s.replaySnapshot()
	if replayPending != nil {
		status.WaitOn = append(status.WaitOn, DrainWait{
			Reason:     ActiveStoreWaitReasonReplayPending,
			EntityID:   replayPending.EntityID,
			EnvelopeID: replayPending.EnvelopeID,
			Op:         replayPending.Op,
			Age:        now.Sub(replayPendingSince),
		})
	}

	deferred, err := s.listDeferredOps()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		for _, env := range deferred {
			if replayPending != nil && env.EnvelopeID == replayPending.EnvelopeID {
				continue
			}
			status.WaitOn = append(status.WaitOn, DrainWait{
				Reason:     ActiveStoreWaitReasonDeferredOp,
				EntityID:   env.EntityID,
				EnvelopeID: env.EnvelopeID,
				Op:         env.Op,
				Age:        now.Sub(env.EnqueuedAt),
			})
		}
	}

	sort.Slice(status.WaitOn, func(i, j int) bool {
		if status.WaitOn[i].Reason != status.WaitOn[j].Reason {
			return status.WaitOn[i].Reason < status.WaitOn[j].Reason
		}
		if status.WaitOn[i].EntityID != status.WaitOn[j].EntityID {
			return status.WaitOn[i].EntityID < status.WaitOn[j].EntityID
		}
		return status.WaitOn[i].EnvelopeID < status.WaitOn[j].EnvelopeID
	})

	return status
}

func (s *ActiveStore[T, S]) blockedDeps(watcherID string) []Dep {
	s.depMu.RLock()
	depList := make([]Dep, len(s.deps[watcherID]))
	copy(depList, s.deps[watcherID])
	s.depMu.RUnlock()

	blocked := make([]Dep, 0, len(depList))
	for _, dep := range depList {
		if dep.Kind != s.opts.Kind {
			blocked = append(blocked, dep)
			continue
		}

		e := s.getEntry(dep.ID)
		if e == nil {
			blocked = append(blocked, dep)
			continue
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
			blocked = append(blocked, dep)
		}
	}

	return blocked
}

func (s *ActiveStore[T, S]) replaySnapshot() (*DeferredOpEnvelope, time.Time) {
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	if s.replayPending == nil {
		return nil, time.Time{}
	}
	copyEnv := *s.replayPending
	return &copyEnv, s.replayPendingSince
}
