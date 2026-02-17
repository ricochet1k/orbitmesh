package session

import "sync"

type StreamUpdateKind string

const (
	// Message should be added to the end
	SUNewMessage StreamUpdateKind = "new_message"

	// Message is partial, contents should be appended
	SUAppend StreamUpdateKind = "append"

	// Message is a new replacement, should replace the given ID
	SUReplace StreamUpdateKind = "replace"
)

// StreamUpdate is an update to a conversation. Message.ID should always refer to the last message unless it is new.
type StreamUpdate struct {
	Kind    StreamUpdateKind
	Message Message
}

type MessageKind string

const (
	MKSystem       MessageKind = "system"
	MKAssistant    MessageKind = "assistant"
	MKUser         MessageKind = "user"
	MKToolCall     MessageKind = "tool_call"
	MKToolResponse MessageKind = "tool_response"
	MKError        MessageKind = "error"
)

type Message struct {
	ID       string
	Kind     MessageKind
	Contents string
}

type Stream struct {
	mu          sync.Mutex
	subscribers []*streamSubscriber
	closed      bool
}

func NewStream() *Stream {
	return &Stream{}
}

// Subscribe creates a new subscription and returns the receiving end.
// bufSize controls the channel buffer; 0 means unbuffered.
func (st *Stream) Subscribe(bufSize int) *StreamReceiver {
	sub, recv := newSubscription(bufSize)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		sub.Close()
		return recv
	}
	st.subscribers = append(st.subscribers, sub)
	return recv
}

func (st *Stream) MessageReplace(msg Message) {
	st.sendToAll(StreamUpdate{Kind: SUReplace, Message: msg})
}

func (st *Stream) MessageAppend(deltaMsg Message) {
	st.sendToAll(StreamUpdate{Kind: SUAppend, Message: deltaMsg})
}

func (st *Stream) MessageNew(msg Message) {
	st.sendToAll(StreamUpdate{Kind: SUNewMessage, Message: msg})
}

func (st *Stream) sendToAll(su StreamUpdate) {
	st.mu.Lock()
	defer st.mu.Unlock()

	alive := st.subscribers[:0]
	for _, sub := range st.subscribers {
		if sub.send(su) {
			alive = append(alive, sub)
		}
	}
	st.subscribers = alive
}

func (st *Stream) Close() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.closed = true
	for _, sub := range st.subscribers {
		sub.Close()
	}
	st.subscribers = nil
}
