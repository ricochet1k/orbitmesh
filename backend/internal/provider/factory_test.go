package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// mockProvider is a minimal Provider for testing the factory.
type mockProvider struct {
	createErr error
	testErr   error
}

type mockResumeProvider struct {
	mockProvider
	built []session.Message
}

func (m *mockProvider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &mockSession{}, nil
}

func (m *mockProvider) TestConfig(ctx context.Context, config session.Config) error {
	return m.testErr
}

func (m *mockResumeProvider) BuildResumeMessages(history []domain.Message) []session.Message {
	if len(m.built) > 0 {
		return m.built
	}
	if len(history) == 0 {
		return nil
	}
	return []session.Message{{ID: history[0].ID, Kind: session.MKUser, Contents: history[0].Contents}}
}

func TestDefaultFactory_Register(t *testing.T) {
	factory := NewDefaultFactory()
	factory.Register("test-provider", &mockProvider{})

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
	factory.Register("test-provider", &mockProvider{})

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
	factory.Register("failing-provider", &mockProvider{createErr: expectedErr})

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

	factory.Register("type1", &mockProvider{})
	factory.Register("type2", &mockProvider{})
	factory.Register("type3", &mockProvider{})

	types := factory.SupportedTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 types, got %d", len(types))
	}
}

func TestDefaultFactory_TestConfig(t *testing.T) {
	factory := NewDefaultFactory()

	factory.Register("good-provider", &mockProvider{})
	factory.Register("bad-provider", &mockProvider{testErr: errors.New("bad key")})

	if err := factory.TestConfig(context.Background(), "good-provider", session.Config{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := factory.TestConfig(context.Background(), "bad-provider", session.Config{}); err == nil {
		t.Error("expected error for bad-provider")
	}
	if err := factory.TestConfig(context.Background(), "unknown-provider", session.Config{}); err == nil {
		t.Error("expected error for unknown provider type")
	}
}

func TestDefaultFactory_BuildResumeMessages(t *testing.T) {
	factory := NewDefaultFactory()
	factory.Register("resume-provider", &mockResumeProvider{built: []session.Message{{ID: "m1", Kind: session.MKSystem, Contents: "seed"}}})

	msgs := factory.BuildResumeMessages("resume-provider", []domain.Message{{ID: "orig", Kind: domain.MessageKindUser, Contents: "hello"}})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 resume message, got %d", len(msgs))
	}
	if msgs[0].ID != "m1" || msgs[0].Kind != session.MKSystem || msgs[0].Contents != "seed" {
		t.Fatalf("unexpected mapped message: %+v", msgs[0])
	}
}

func TestDefaultFactory_BuildResumeMessages_NoPolicy(t *testing.T) {
	factory := NewDefaultFactory()
	factory.Register("plain-provider", &mockProvider{})

	msgs := factory.BuildResumeMessages("plain-provider", []domain.Message{{ID: "orig", Kind: domain.MessageKindUser, Contents: "hello"}})
	if len(msgs) != 0 {
		t.Fatalf("expected no resume messages without policy, got %d", len(msgs))
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

func (m *mockSession) Stop(ctx context.Context) error { return nil }
func (m *mockSession) Kill() error                    { return nil }
func (m *mockSession) Status() session.Status         { return session.Status{} }
func (m *mockSession) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	return nil, nil
}
