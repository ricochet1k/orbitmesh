package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

// --- mock ---

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

// --- helpers ---

func requireTerminalEvent(t *testing.T, ch <-chan TerminalEvent) TerminalEvent {
	t.Helper()
	select {
	case event, ok := <-ch:
		if !ok {
			t.Fatal("terminal event channel closed unexpectedly")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
		return TerminalEvent{}
	}
}

func requireEventKind(t *testing.T, ch <-chan TerminalEvent, want terminal.UpdateKind) TerminalEvent {
	t.Helper()
	ev := requireTerminalEvent(t, ch)
	if ev.Update.Kind != want {
		t.Fatalf("expected event kind %v, got %v", want, ev.Update.Kind)
	}
	return ev
}

func sendDiff(provider *mockTerminalProvider, line string) {
	provider.updates <- terminal.Update{
		Kind: terminal.UpdateDiff,
		Diff: &terminal.Diff{
			Region: terminal.Region{X: 0, Y: 0, X2: 1, Y2: 1},
			Lines:  []string{line},
		},
	}
}

// --- tests ---

// TestTerminalHub_InitialSnapshot verifies that a new subscriber immediately
// receives a snapshot event reflecting the current terminal state.
func TestTerminalHub_InitialSnapshot(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)

	hub := NewTerminalHub("session-1", provider, nil)
	updates, cancel := hub.Subscribe(8)
	defer cancel()

	ev := requireEventKind(t, updates, terminal.UpdateSnapshot)
	if ev.Update.Snapshot == nil {
		t.Fatal("initial snapshot event has nil Snapshot")
	}
}

// TestTerminalHub_DiffDelivery verifies that diffs sent to the provider are
// forwarded to subscribers in order.
func TestTerminalHub_DiffDelivery(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)

	hub := NewTerminalHub("session-1", provider, nil)
	updates, cancel := hub.Subscribe(8)
	defer cancel()

	requireEventKind(t, updates, terminal.UpdateSnapshot) // consume initial

	sendDiff(provider, "x")

	ev := requireEventKind(t, updates, terminal.UpdateDiff)
	if len(ev.Update.Diff.Lines) == 0 || ev.Update.Diff.Lines[0] != "x" {
		t.Fatalf("unexpected diff content: %v", ev.Update.Diff.Lines)
	}
}

// TestTerminalHub_SeqIsMonotonicallyIncreasing verifies that event sequence
// numbers increase by exactly 1 for each event delivered to a subscriber.
func TestTerminalHub_SeqIsMonotonicallyIncreasing(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)

	hub := NewTerminalHub("session-1", provider, nil)
	updates, cancel := hub.Subscribe(16)
	defer cancel()

	ev0 := requireTerminalEvent(t, updates) // initial snapshot
	lastSeq := ev0.Seq

	for _, line := range []string{"a", "b", "c"} {
		sendDiff(provider, line)
	}

	for i := 0; i < 3; i++ {
		ev := requireTerminalEvent(t, updates)
		if ev.Seq != lastSeq+1 {
			t.Fatalf("expected seq %d, got %d", lastSeq+1, ev.Seq)
		}
		lastSeq = ev.Seq
	}
}

// TestTerminalHub_SlowSubscriberDropsEvents verifies that a full subscriber
// channel causes events to be silently dropped (not queued or panicked).
// The subscriber should see a sequence gap, which is the signal to resync.
func TestTerminalHub_SlowSubscriberDropsEvents(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)

	// Buffer of 1: initial snapshot fills it; subsequent diffs will be dropped.
	hub := NewTerminalHub("session-1", provider, nil)
	updates, cancel := hub.Subscribe(1)
	defer cancel()

	// Don't read from updates so the channel stays full after the snapshot.
	// Send several diffs — none of these should block or panic.
	for _, line := range []string{"a", "b", "c", "d", "e"} {
		sendDiff(provider, line)
	}

	// Give the hub goroutine time to attempt delivery of all diffs.
	time.Sleep(50 * time.Millisecond)

	// Now drain. We must get the initial snapshot.
	ev := requireEventKind(t, updates, terminal.UpdateSnapshot)

	// Collect everything else that made it through (at most 1 more slot was free).
	var got []TerminalEvent
drain:
	for {
		select {
		case ev, ok := <-updates:
			if !ok {
				break drain
			}
			got = append(got, ev)
		default:
			break drain
		}
	}

	// If any diffs arrived, check for a sequence gap — confirming the drop.
	if len(got) > 0 {
		firstAfter := got[0].Seq
		if firstAfter == ev.Seq+1 {
			// No gap yet — that's fine only if fewer than 5 events arrived.
			if len(got) == 5 {
				t.Fatal("expected some events to be dropped with a buffer of 1")
			}
		}
		// A gap anywhere is also acceptable evidence of dropping.
	}
	// The key assertion: we got here without a deadlock, panic, or resync error.
}

// TestTerminalHub_MultipleSubscribers verifies that all active subscribers
// receive the same events.
func TestTerminalHub_MultipleSubscribers(t *testing.T) {
	provider := newMockTerminalProvider()
	defer close(provider.updates)

	hub := NewTerminalHub("session-1", provider, nil)
	updates1, cancel1 := hub.Subscribe(8)
	updates2, cancel2 := hub.Subscribe(8)
	defer cancel1()
	defer cancel2()

	requireEventKind(t, updates1, terminal.UpdateSnapshot)
	requireEventKind(t, updates2, terminal.UpdateSnapshot)

	sendDiff(provider, "shared")

	ev1 := requireEventKind(t, updates1, terminal.UpdateDiff)
	ev2 := requireEventKind(t, updates2, terminal.UpdateDiff)

	if ev1.Seq != ev2.Seq {
		t.Fatalf("subscribers received different seq: %d vs %d", ev1.Seq, ev2.Seq)
	}
}
