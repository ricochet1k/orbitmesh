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

func TestInputBuffer_PauseResume(t *testing.T) {
	buf := NewInputBuffer(10)
	defer buf.Close()

	ctx := context.Background()

	// Pause before sending
	buf.Pause()

	if !buf.IsPaused() {
		t.Error("expected buffer to be paused")
	}

	// Send while paused (should be buffered)
	if err := buf.Send(ctx, "buffered1"); err != nil {
		t.Fatalf("failed to send: %v", err)
	}
	if err := buf.Send(ctx, "buffered2"); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	if buf.BufferedCount() != 2 {
		t.Errorf("expected 2 buffered items, got %d", buf.BufferedCount())
	}

	// Should not receive anything while paused
	select {
	case <-buf.Receive():
		t.Fatal("should not receive input while paused")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	// Resume
	buf.Resume()

	if buf.IsPaused() {
		t.Error("expected buffer to be resumed")
	}

	// Should receive buffered items
	select {
	case input := <-buf.Receive():
		if input != "buffered1" {
			t.Errorf("expected 'buffered1', got %q", input)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for buffered input")
	}

	select {
	case input := <-buf.Receive():
		if input != "buffered2" {
			t.Errorf("expected 'buffered2', got %q", input)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for buffered input")
	}

	if buf.BufferedCount() != 0 {
		t.Errorf("expected 0 buffered items after resume, got %d", buf.BufferedCount())
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
