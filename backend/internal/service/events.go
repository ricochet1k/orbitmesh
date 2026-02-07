package service

import (
	"sync"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type Subscriber struct {
	ID        string
	SessionID string
	Events    chan domain.Event
}

type EventBroadcaster struct {
	subscribers map[string]*Subscriber
	mu          sync.RWMutex
	bufferSize  int
	history     map[string][]domain.Event
	historySize int
	nextID      int64
}

func NewEventBroadcaster(bufferSize int) *EventBroadcaster {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &EventBroadcaster{
		subscribers: make(map[string]*Subscriber),
		bufferSize:  bufferSize,
		history:     make(map[string][]domain.Event),
		historySize: bufferSize,
	}
}

func (b *EventBroadcaster) Subscribe(subscriberID, sessionID string) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subscribeLocked(subscriberID, sessionID)
}

func (b *EventBroadcaster) SubscribeWithReplay(subscriberID, sessionID string, lastEventID int64) (*Subscriber, []domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := b.subscribeLocked(subscriberID, sessionID)
	snapshotID := b.nextID
	if lastEventID >= snapshotID {
		return sub, nil
	}
	replay := b.replayLocked(sessionID, lastEventID, snapshotID)
	return sub, replay
}

func (b *EventBroadcaster) Unsubscribe(subscriberID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, ok := b.subscribers[subscriberID]; ok {
		close(sub.Events)
		delete(b.subscribers, subscriberID)
	}
}

func (b *EventBroadcaster) Broadcast(event domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	event.ID = b.nextID
	b.appendHistoryLocked(event)

	for _, sub := range b.subscribers {
		if sub.SessionID == "" || sub.SessionID == event.SessionID {
			select {
			case sub.Events <- event:
			default:
			}
		}
	}
}

func (b *EventBroadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

func (b *EventBroadcaster) SessionSubscriberCount(sessionID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, sub := range b.subscribers {
		if sub.SessionID == "" || sub.SessionID == sessionID {
			count++
		}
	}
	return count
}

func (b *EventBroadcaster) subscribeLocked(subscriberID, sessionID string) *Subscriber {
	sub := &Subscriber{
		ID:        subscriberID,
		SessionID: sessionID,
		Events:    make(chan domain.Event, b.bufferSize),
	}

	b.subscribers[subscriberID] = sub
	return sub
}

func (b *EventBroadcaster) replayLocked(sessionID string, lastEventID, maxID int64) []domain.Event {
	history := b.history[sessionID]
	if len(history) == 0 {
		return nil
	}
	replay := make([]domain.Event, 0, len(history))
	for _, event := range history {
		if event.ID > lastEventID && event.ID <= maxID {
			replay = append(replay, event)
		}
	}
	return replay
}

func (b *EventBroadcaster) appendHistoryLocked(event domain.Event) {
	if b.historySize <= 0 {
		return
	}
	history := append(b.history[event.SessionID], event)
	if len(history) > b.historySize {
		history = history[len(history)-b.historySize:]
	}
	b.history[event.SessionID] = history
}
