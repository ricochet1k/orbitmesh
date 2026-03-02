package acp

import (
	"context"
	"errors"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestSendInput_StartFailureEmitsActionableError(t *testing.T) {
	sess, err := NewSession("sess-start-failure", Config{}, session.Config{ProviderType: "acp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	_, err = sess.SendInput(context.Background(), session.Config{ProviderType: "acp"}, "hello")
	if err == nil {
		t.Fatal("expected SendInput start failure")
	}

	foundActionable := false
	for i := 0; i < 4; i++ {
		select {
		case ev := <-sess.events.Events():
			if ev.Type != domain.EventTypeError {
				continue
			}
			errData, ok := ev.Error()
			if ok && errData.Code == "ACP_SEND_INPUT" {
				foundActionable = true
			}
		default:
		}
	}

	if !foundActionable {
		t.Fatal("expected ACP_SEND_INPUT error event")
	}
}

func TestSendInput_NoActiveSessionEmitsError(t *testing.T) {
	sess, err := NewSession("sess-no-active", Config{Command: "noop"}, session.Config{ProviderType: "acp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sess.started = true
	sess.runtime = &sharedRuntime{}

	_, err = sess.SendInput(context.Background(), session.Config{ProviderType: "acp"}, "hello")
	if !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("expected ErrNoActiveSession, got %v", err)
	}

	select {
	case ev := <-sess.events.Events():
		if ev.Type != domain.EventTypeError {
			t.Fatalf("expected error event, got %v", ev.Type)
		}
		errData, ok := ev.Error()
		if !ok {
			t.Fatal("expected error payload")
		}
		if errData.Code != "ACP_SEND_INPUT" {
			t.Fatalf("expected ACP_SEND_INPUT code, got %q", errData.Code)
		}
	default:
		t.Fatal("expected error event for no active session")
	}
}
