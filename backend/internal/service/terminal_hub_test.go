package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

type mockTerminalProvider struct {
	mu       sync.Mutex
	updates  chan terminal.Update
	snapshot terminal.Snapshot
	inputs   []terminal.Input
}

func newMockTerminalProvider() *mockTerminalProvider {
	return &mockTerminalProvider{
		updates:  make(chan terminal.Update, 32),
		snapshot: terminal.Snapshot{Rows: 1, Cols: 4, Lines: []string{"test"}},
	}
}

func (m *mockTerminalProvider) TerminalSnapshot() (terminal.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot, nil
}

func (m *mockTerminalProvider) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
	return m.updates, func() {}
}

func (m *mockTerminalProvider) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
	m.mu.Lock()
	m.inputs = append(m.inputs, input)
	m.mu.Unlock()
	return nil
}

func TestTerminalHub_ResyncOnOverflow(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)
	hub := NewTerminalHub("session-1", provider, nil)
	updates, cancel := hub.Subscribe(1)
	defer cancel()

	initial := <-updates
	if initial.Update.Kind != terminal.UpdateSnapshot {
		t.Fatalf("expected initial snapshot, got %v", initial.Update.Kind)
	}

	for _, line := range []string{"a", "b", "c", "d", "e"} {
		provider.updates <- terminal.Update{Kind: terminal.UpdateDiff, Diff: &terminal.Diff{Region: terminal.Region{X: 0, Y: 0, X2: 1, Y2: 1}, Lines: []string{line}}}
	}

	waitForResync(t, updates)

	provider.updates <- terminal.Update{Kind: terminal.UpdateDiff, Diff: &terminal.Diff{Region: terminal.Region{X: 0, Y: 0, X2: 1, Y2: 1}, Lines: []string{"f"}}}

	snapshot := waitForTerminalEvent(t, updates)
	if snapshot.Update.Kind != terminal.UpdateSnapshot {
		t.Fatalf("expected snapshot after resync, got %v", snapshot.Update.Kind)
	}

	final := waitForTerminalEvent(t, updates)
	if final.Update.Kind != terminal.UpdateDiff {
		t.Fatalf("expected diff after resync, got %v", final.Update.Kind)
	}
}

func waitForTerminalEvent(t *testing.T, ch <-chan TerminalEvent) TerminalEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
		return TerminalEvent{}
	}
}

func waitForResync(t *testing.T, ch <-chan TerminalEvent) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		event := waitForTerminalEvent(t, ch)
		if event.Update.Kind == terminal.UpdateError && event.Update.Error != nil && event.Update.Error.Resync {
			return
		}
	}
	t.Fatal("timed out waiting for resync error")
}
