package service

import (
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestNewEventBroadcaster(t *testing.T) {
	t.Run("with default buffer size", func(t *testing.T) {
		b := NewEventBroadcaster(0)
		if b == nil {
			t.Fatal("expected non-nil broadcaster")
		}
		if b.bufferSize != 100 {
			t.Errorf("expected buffer size 100, got %d", b.bufferSize)
		}
	})

	t.Run("with custom buffer size", func(t *testing.T) {
		b := NewEventBroadcaster(50)
		if b.bufferSize != 50 {
			t.Errorf("expected buffer size 50, got %d", b.bufferSize)
		}
	})
}

func TestEventBroadcaster_Subscribe(t *testing.T) {
	b := NewEventBroadcaster(10)

	sub := b.Subscribe("sub1", "session1")
	if sub == nil {
		t.Fatal("expected non-nil subscriber")
	}
	if sub.ID != "sub1" {
		t.Errorf("expected ID 'sub1', got '%s'", sub.ID)
	}
	if sub.SessionID != "session1" {
		t.Errorf("expected SessionID 'session1', got '%s'", sub.SessionID)
	}
	if sub.Events == nil {
		t.Error("expected non-nil Events channel")
	}
	if b.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", b.SubscriberCount())
	}
}

func TestEventBroadcaster_Unsubscribe(t *testing.T) {
	b := NewEventBroadcaster(10)

	sub := b.Subscribe("sub1", "session1")
	b.Unsubscribe("sub1")

	if b.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", b.SubscriberCount())
	}

	select {
	case _, ok := <-sub.Events:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		t.Error("expected channel to be closed immediately")
	}
}

func TestEventBroadcaster_Broadcast(t *testing.T) {
	b := NewEventBroadcaster(10)

	sub1 := b.Subscribe("sub1", "session1")
	sub2 := b.Subscribe("sub2", "session1")
	sub3 := b.Subscribe("sub3", "session2")
	subAll := b.Subscribe("subAll", "")

	event := domain.NewOutputEvent("session1", "test output")
	b.Broadcast(event)

	timeout := time.After(100 * time.Millisecond)

	select {
	case e := <-sub1.Events:
		if e.SessionID != "session1" {
			t.Errorf("expected session1, got %s", e.SessionID)
		}
	case <-timeout:
		t.Error("sub1 should have received event")
	}

	select {
	case e := <-sub2.Events:
		if e.SessionID != "session1" {
			t.Errorf("expected session1, got %s", e.SessionID)
		}
	case <-timeout:
		t.Error("sub2 should have received event")
	}

	select {
	case <-sub3.Events:
		t.Error("sub3 should not have received event for session1")
	case <-time.After(10 * time.Millisecond):
	}

	select {
	case e := <-subAll.Events:
		if e.SessionID != "session1" {
			t.Errorf("expected session1, got %s", e.SessionID)
		}
	case <-timeout:
		t.Error("subAll should have received event")
	}
}

func TestEventBroadcaster_BroadcastNonBlocking(t *testing.T) {
	b := NewEventBroadcaster(1)

	sub := b.Subscribe("sub1", "session1")

	event1 := domain.NewOutputEvent("session1", "event1")
	event2 := domain.NewOutputEvent("session1", "event2")
	event3 := domain.NewOutputEvent("session1", "event3")

	b.Broadcast(event1)
	b.Broadcast(event2)
	b.Broadcast(event3)

	<-sub.Events
	if b.DroppedEventCount() == 0 {
		t.Fatal("expected dropped events to be tracked for slow subscriber")
	}
}

func TestEventBroadcaster_ConcurrentAccess(t *testing.T) {
	b := NewEventBroadcaster(100)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			subID := string(rune('a' + id))
			sub := b.Subscribe(subID, "session1")
			time.Sleep(10 * time.Millisecond)
			b.Unsubscribe(subID)
			_ = sub
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event := domain.NewOutputEvent("session1", "test")
			b.Broadcast(event)
		}(i)
	}

	wg.Wait()
}

func TestEventBroadcaster_SubscribeWithReplay(t *testing.T) {
	t.Run("no history returns empty replay", func(t *testing.T) {
		b := NewEventBroadcaster(10)
		sub, replay := b.SubscribeWithReplay("sub1", "session1", 0)
		if sub == nil {
			t.Fatal("expected non-nil subscriber")
		}
		if len(replay) != 0 {
			t.Errorf("expected empty replay, got %d events", len(replay))
		}
		if b.SubscriberCount() != 1 {
			t.Errorf("expected 1 subscriber, got %d", b.SubscriberCount())
		}
	})

	t.Run("replays events after lastEventID", func(t *testing.T) {
		b := NewEventBroadcaster(10)

		// Broadcast three events without any subscriber
		e1 := domain.NewOutputEvent("session1", "first")
		e2 := domain.NewOutputEvent("session1", "second")
		e3 := domain.NewOutputEvent("session1", "third")
		b.Broadcast(e1)
		b.Broadcast(e2)
		b.Broadcast(e3)

		// Subscribe with lastEventID=1, so we should replay events 2 and 3
		sub, replay := b.SubscribeWithReplay("sub1", "session1", 1)
		if sub == nil {
			t.Fatal("expected non-nil subscriber")
		}
		if len(replay) != 2 {
			t.Fatalf("expected 2 replayed events, got %d", len(replay))
		}
		if replay[0].ID != 2 || replay[1].ID != 3 {
			t.Errorf("unexpected replayed event IDs: %d, %d", replay[0].ID, replay[1].ID)
		}
	})

	t.Run("lastEventID at current returns no replay", func(t *testing.T) {
		b := NewEventBroadcaster(10)

		e := domain.NewOutputEvent("session1", "only")
		b.Broadcast(e)

		// lastEventID >= nextID means nothing to replay
		sub, replay := b.SubscribeWithReplay("sub1", "session1", 999)
		if sub == nil {
			t.Fatal("expected non-nil subscriber")
		}
		if len(replay) != 0 {
			t.Errorf("expected empty replay, got %d events", len(replay))
		}
	})

	t.Run("replays only matching session events", func(t *testing.T) {
		b := NewEventBroadcaster(10)

		b.Broadcast(domain.NewOutputEvent("session1", "s1 event"))
		b.Broadcast(domain.NewOutputEvent("session2", "s2 event"))
		b.Broadcast(domain.NewOutputEvent("session1", "s1 event 2"))

		_, replay := b.SubscribeWithReplay("sub1", "session1", 0)
		for _, ev := range replay {
			if ev.SessionID != "session1" {
				t.Errorf("got event for wrong session: %s", ev.SessionID)
			}
		}
		if len(replay) != 2 {
			t.Errorf("expected 2 session1 events, got %d", len(replay))
		}
	})

	t.Run("history capped at buffer size", func(t *testing.T) {
		b := NewEventBroadcaster(3)

		for i := 0; i < 5; i++ {
			b.Broadcast(domain.NewOutputEvent("session1", "event"))
		}

		_, replay := b.SubscribeWithReplay("sub1", "session1", 0)
		// historySize is 3, so only last 3 events are kept
		if len(replay) != 3 {
			t.Errorf("expected 3 replayed events (history cap), got %d", len(replay))
		}
	})

	t.Run("wildcard replay uses global history", func(t *testing.T) {
		b := NewEventBroadcaster(10)

		b.Broadcast(domain.NewOutputEvent("session1", "s1 first"))
		b.Broadcast(domain.NewOutputEvent("session2", "s2 first"))
		b.Broadcast(domain.NewOutputEvent("session1", "s1 second"))

		_, replay := b.SubscribeWithReplay("sub-global", "", 1)
		if len(replay) != 2 {
			t.Fatalf("expected 2 global replay events, got %d", len(replay))
		}
		if replay[0].SessionID != "session2" || replay[1].SessionID != "session1" {
			t.Fatalf("unexpected replay order/session ids: %s, %s", replay[0].SessionID, replay[1].SessionID)
		}
		if replay[0].ID != 2 || replay[1].ID != 3 {
			t.Fatalf("unexpected replay IDs: %d, %d", replay[0].ID, replay[1].ID)
		}
	})
}

func TestEventBroadcaster_SessionSubscriberCount(t *testing.T) {
	b := NewEventBroadcaster(10)

	b.Subscribe("sub1", "session1")
	b.Subscribe("sub2", "session1")
	b.Subscribe("sub3", "session2")
	b.Subscribe("subAll", "")

	count := b.SessionSubscriberCount("session1")
	if count != 3 {
		t.Errorf("expected 3 subscribers for session1, got %d", count)
	}

	count = b.SessionSubscriberCount("session2")
	if count != 2 {
		t.Errorf("expected 2 subscribers for session2, got %d", count)
	}
}
