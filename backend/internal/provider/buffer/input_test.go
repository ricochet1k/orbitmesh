package buffer

import (
	"context"
	"testing"
	"time"
)

func TestInputBuffer_SendReceive(t *testing.T) {
	buf := NewInputBuffer(10)
	defer buf.Close()

	ctx := context.Background()

	// Send input
	if err := buf.Send(ctx, "test1"); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// Receive input
	select {
	case input := <-buf.Receive():
		if input != "test1" {
			t.Errorf("expected 'test1', got %q", input)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for input")
	}
}

func TestInputBuffer_MultipleMessages(t *testing.T) {
	buf := NewInputBuffer(10)
	defer buf.Close()

	ctx := context.Background()

	// Send multiple messages
	messages := []string{"msg1", "msg2", "msg3", "msg4", "msg5"}
	for _, msg := range messages {
		if err := buf.Send(ctx, msg); err != nil {
			t.Fatalf("failed to send: %v", err)
		}
	}

	// Receive all messages
	for i, expected := range messages {
		select {
		case input := <-buf.Receive():
			if input != expected {
				t.Errorf("message %d: expected %q, got %q", i, expected, input)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for message %d", i)
		}
	}
}
