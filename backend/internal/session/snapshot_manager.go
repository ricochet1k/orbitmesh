package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SnapshotStorage defines the interface for snapshot persistence.
type SnapshotStorage interface {
	Save(snapshot *SessionSnapshot) error
	Load(sessionID string) (*SessionSnapshot, error)
	Delete(sessionID string) error
	Exists(sessionID string) bool
}

// Snapshottable defines the interface for sessions that support snapshotting.
type Snapshottable interface {
	CreateSnapshot() (*SessionSnapshot, error)
	RestoreFromSnapshot(snapshot *SessionSnapshot) error
}

// SnapshotManager manages automatic snapshots for sessions.
type SnapshotManager struct {
	storage  SnapshotStorage
	interval time.Duration

	mu             sync.RWMutex
	activeSnapshots map[string]*snapshotTask
}

type snapshotTask struct {
	sessionID  string
	cancel     context.CancelFunc
	lastSaved  time.Time
	saveCount  int
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(storage SnapshotStorage, interval time.Duration) *SnapshotManager {
	if interval <= 0 {
		interval = 5 * time.Minute // Default interval
	}

	return &SnapshotManager{
		storage:         storage,
		interval:        interval,
		activeSnapshots: make(map[string]*snapshotTask),
	}
}

// Snapshot manually saves a snapshot for the given session.
func (sm *SnapshotManager) Snapshot(sess Snapshottable) error {
	snapshot, err := sess.CreateSnapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	if err := sm.storage.Save(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Update last saved time
	sm.mu.Lock()
	if task, ok := sm.activeSnapshots[snapshot.SessionID]; ok {
		task.lastSaved = time.Now()
		task.saveCount++
	}
	sm.mu.Unlock()

	return nil
}

// StartAutoSnapshot starts automatic snapshots for a session.
func (sm *SnapshotManager) StartAutoSnapshot(ctx context.Context, sess Snapshottable, sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Stop existing task if any
	if task, exists := sm.activeSnapshots[sessionID]; exists {
		task.cancel()
		delete(sm.activeSnapshots, sessionID)
	}

	// Create new task
	taskCtx, cancel := context.WithCancel(ctx)
	task := &snapshotTask{
		sessionID: sessionID,
		cancel:    cancel,
		lastSaved: time.Now(),
	}
	sm.activeSnapshots[sessionID] = task

	// Start background goroutine
	go sm.runAutoSnapshot(taskCtx, sess, task)
}

// StopAutoSnapshot stops automatic snapshots for a session.
func (sm *SnapshotManager) StopAutoSnapshot(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if task, exists := sm.activeSnapshots[sessionID]; exists {
		task.cancel()
		delete(sm.activeSnapshots, sessionID)
	}
}

// Restore loads a snapshot from storage.
func (sm *SnapshotManager) Restore(sessionID string) (*SessionSnapshot, error) {
	return sm.storage.Load(sessionID)
}

// Exists checks if a snapshot exists for the given session.
func (sm *SnapshotManager) Exists(sessionID string) bool {
	return sm.storage.Exists(sessionID)
}

// runAutoSnapshot runs the automatic snapshot loop.
func (sm *SnapshotManager) runAutoSnapshot(ctx context.Context, sess Snapshottable, task *snapshotTask) {
	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sm.Snapshot(sess); err != nil {
				// Log error but continue (don't want to crash auto-snapshot)
				// In production, this would emit a metric or log
				_ = err
			}
		}
	}
}

// Stats returns statistics about the snapshot manager.
func (sm *SnapshotManager) Stats() map[string]any {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := make(map[string]any)
	stats["active_sessions"] = len(sm.activeSnapshots)
	stats["interval"] = sm.interval.String()

	sessions := make([]map[string]any, 0, len(sm.activeSnapshots))
	for _, task := range sm.activeSnapshots {
		sessions = append(sessions, map[string]any{
			"session_id":  task.sessionID,
			"last_saved":  task.lastSaved,
			"save_count":  task.saveCount,
			"age":         time.Since(task.lastSaved).String(),
		})
	}
	stats["sessions"] = sessions

	return stats
}
