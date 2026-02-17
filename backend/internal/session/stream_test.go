package session

import (
	"testing"
	"time"
)

func msg(id, contents string) Message {
	return Message{ID: id, Kind: MKAssistant, Contents: contents}
}

func TestStreamFanOut(t *testing.T) {
	st := &Stream{}

	recv1 := st.Subscribe(8)
	recv2 := st.Subscribe(8)

	st.MessageNew(msg("1", "hello"))

	got1 := <-recv1.C
	got2 := <-recv2.C

	if got1.Kind != SUNewMessage || got1.Message.Contents != "hello" {
		t.Fatalf("recv1: unexpected update %+v", got1)
	}
	if got2.Kind != SUNewMessage || got2.Message.Contents != "hello" {
		t.Fatalf("recv2: unexpected update %+v", got2)
	}
}

func TestStreamAppendAndReplace(t *testing.T) {
	st := &Stream{}
	recv := st.Subscribe(8)

	st.MessageAppend(msg("1", " world"))
	st.MessageReplace(msg("1", "replaced"))

	got := <-recv.C
	if got.Kind != SUAppend || got.Message.Contents != " world" {
		t.Fatalf("append: unexpected %+v", got)
	}
	got = <-recv.C
	if got.Kind != SUReplace || got.Message.Contents != "replaced" {
		t.Fatalf("replace: unexpected %+v", got)
	}
}

func TestClosedSubscriberRemovedFromStream(t *testing.T) {
	st := &Stream{}

	recv1 := st.Subscribe(8)
	recv2 := st.Subscribe(8)

	// Close recv2 before sending
	recv2.Close()

	st.MessageNew(msg("1", "only-recv1"))

	got := <-recv1.C
	if got.Message.Contents != "only-recv1" {
		t.Fatalf("unexpected %+v", got)
	}

	st.mu.Lock()
	count := len(st.subscribers)
	st.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 subscriber after cleanup, got %d", count)
	}
}

func TestReceiverCloseClosesChannel(t *testing.T) {
	st := &Stream{}
	recv := st.Subscribe(8)
	recv.Close()
	if _, ok := <-recv.C; ok {
		t.Fatal("expected channel to be closed after receiver.Close()")
	}
}

func TestStreamCloseClosesAllSubscribers(t *testing.T) {
	st := &Stream{}
	recv1 := st.Subscribe(8)
	recv2 := st.Subscribe(8)

	st.Close()

	if _, ok := <-recv1.C; ok {
		t.Fatal("recv1 channel should be closed")
	}
	if _, ok := <-recv2.C; ok {
		t.Fatal("recv2 channel should be closed")
	}
}

func TestSubscribeAfterCloseImmediatelyCloses(t *testing.T) {
	st := &Stream{}
	st.Close()

	recv := st.Subscribe(8)

	if _, ok := <-recv.C; ok {
		t.Fatal("channel should be closed")
	}
}

func TestDoubleCloseIsSafe(t *testing.T) {
	st := &Stream{}
	recv := st.Subscribe(0)
	recv.Close()
	recv.Close() // should not panic
}

func TestSendBlocksUntilReceived(t *testing.T) {
	st := &Stream{}
	recv := st.Subscribe(0) // unbuffered

	done := make(chan struct{})
	go func() {
		st.MessageNew(msg("1", "blocking"))
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("sendToAll should block on unbuffered channel")
	case <-time.After(50 * time.Millisecond):
	}

	<-recv.C
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sendToAll should have unblocked after receive")
	}
}
