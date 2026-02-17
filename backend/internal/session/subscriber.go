package session

import "sync/atomic"

// streamSubscriber is the sending side of a subscription held by the Stream.
type streamSubscriber struct {
	c       chan StreamUpdate
	closedC chan struct{}
	closed  atomic.Bool
}

// StreamReceiver is the receiving end of a subscription held by the consumer.
type StreamReceiver struct {
	C   <-chan StreamUpdate
	sub *streamSubscriber
}

func newSubscription(bufSize int) (*streamSubscriber, *StreamReceiver) {
	ch := make(chan StreamUpdate, bufSize)
	closedC := make(chan struct{})
	sub := &streamSubscriber{
		c:       ch,
		closedC: closedC,
	}
	recv := &StreamReceiver{
		C:   ch,
		sub: sub,
	}
	return sub, recv
}

// send attempts a non-blocking send. Returns false if the subscriber is closed.
func (ss *streamSubscriber) send(su StreamUpdate) bool {
	if ss.closed.Load() {
		return false
	}
	select {
	case ss.c <- su:
		return true
	case <-ss.closedC:
		return false
	}
}

// Close shuts down the subscription from the sending side.
func (ss *streamSubscriber) Close() {
	if ss.closed.CompareAndSwap(false, true) {
		close(ss.closedC)
		close(ss.c)
	}
}

func (ss *streamSubscriber) IsClosed() bool {
	return ss.closed.Load()
}

// Close shuts down the subscription from the receiving side.
func (sr *StreamReceiver) Close() {
	sr.sub.Close()
}
