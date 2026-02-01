package service

import (
	"sync"

	"github.com/orbitmesh/orbitmesh/internal/domain"
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
}

func NewEventBroadcaster(bufferSize int) *EventBroadcaster {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &EventBroadcaster{
		subscribers: make(map[string]*Subscriber),
		bufferSize:  bufferSize,
	}
}

func (b *EventBroadcaster) Subscribe(subscriberID, sessionID string) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &Subscriber{
		ID:        subscriberID,
		SessionID: sessionID,
		Events:    make(chan domain.Event, b.bufferSize),
	}

	b.subscribers[subscriberID] = sub
	return sub
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
	b.mu.RLock()
	defer b.mu.RUnlock()

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
