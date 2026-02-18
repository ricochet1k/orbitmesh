package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestDefaultFactory_Register(t *testing.T) {
	factory := NewDefaultFactory()

	creator := func(sessionID string, config session.Config) (session.Session, error) {
		return &mockSession{}, nil
	}

	factory.Register("test-provider", creator)

	types := factory.SupportedTypes()
	if len(types) != 1 {
		t.Errorf("expected 1 type, got %d", len(types))
	}
	if types[0] != "test-provider" {
		t.Errorf("expected 'test-provider', got %s", types[0])
	}
}

func TestDefaultFactory_CreateSession(t *testing.T) {
	factory := NewDefaultFactory()

	creator := func(sessionID string, config session.Config) (session.Session, error) {
		return &mockSession{}, nil
	}

	factory.Register("test-provider", creator)

	sess, err := factory.CreateSession("test-provider", "session-1", session.Config{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Error("expected session to be non-nil")
	}
}

func TestDefaultFactory_CreateSessionUnknownType(t *testing.T) {
	factory := NewDefaultFactory()

	_, err := factory.CreateSession("unknown-provider", "session-1", session.Config{})

	if err == nil {
		t.Error("expected error for unknown provider type")
	}
}

func TestDefaultFactory_CreateSessionWithError(t *testing.T) {
	factory := NewDefaultFactory()

	expectedErr := errors.New("creation failed")
	creator := func(sessionID string, config session.Config) (session.Session, error) {
		return nil, expectedErr
	}

	factory.Register("failing-provider", creator)

	_, err := factory.CreateSession("failing-provider", "session-1", session.Config{})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestDefaultFactory_SupportedTypes(t *testing.T) {
	factory := NewDefaultFactory()

	if len(factory.SupportedTypes()) != 0 {
		t.Error("expected no types initially")
	}

	factory.Register("type1", func(string, session.Config) (session.Session, error) { return nil, nil })
	factory.Register("type2", func(string, session.Config) (session.Session, error) { return nil, nil })
	factory.Register("type3", func(string, session.Config) (session.Session, error) { return nil, nil })

	types := factory.SupportedTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 types, got %d", len(types))
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    session.State
		expected string
	}{
		{session.StateCreated, "created"},
		{session.StateStarting, "starting"},
		{session.StateRunning, "running"},
		{session.StateStopping, "stopping"},
		{session.StateStopped, "stopped"},
		{session.StateError, "error"},
		{session.State(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}

// mockSession is a minimal session.Session for testing the factory.
type mockSession struct{}

func (m *mockSession) Start(ctx context.Context, config session.Config) error { return nil }
func (m *mockSession) Stop(ctx context.Context) error                         { return nil }
func (m *mockSession) Kill() error                                            { return nil }
func (m *mockSession) Status() session.Status                                 { return session.Status{} }
func (m *mockSession) Events() <-chan domain.Event                            { return nil }
func (m *mockSession) SendInput(ctx context.Context, input string) error      { return nil }
