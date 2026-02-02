package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type mockProvider struct{}

func (m *mockProvider) Start(ctx context.Context, config Config) error { return nil }
func (m *mockProvider) Stop(ctx context.Context) error                 { return nil }
func (m *mockProvider) Pause(ctx context.Context) error                { return nil }
func (m *mockProvider) Resume(ctx context.Context) error               { return nil }
func (m *mockProvider) Kill() error                                    { return nil }
func (m *mockProvider) Status() Status                                 { return Status{} }
func (m *mockProvider) Events() <-chan domain.Event                    { return nil }

func TestDefaultFactory_Register(t *testing.T) {
	factory := NewDefaultFactory()

	creator := func(sessionID string, config Config) (Provider, error) {
		return &mockProvider{}, nil
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

func TestDefaultFactory_Create(t *testing.T) {
	factory := NewDefaultFactory()

	creator := func(sessionID string, config Config) (Provider, error) {
		return &mockProvider{}, nil
	}

	factory.Register("test-provider", creator)

	provider, err := factory.Create("test-provider", "session-1", Config{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Error("expected provider to be non-nil")
	}
}

func TestDefaultFactory_CreateUnknownType(t *testing.T) {
	factory := NewDefaultFactory()

	_, err := factory.Create("unknown-provider", "session-1", Config{})

	if err == nil {
		t.Error("expected error for unknown provider type")
	}
}

func TestDefaultFactory_CreateWithError(t *testing.T) {
	factory := NewDefaultFactory()

	expectedErr := errors.New("creation failed")
	creator := func(sessionID string, config Config) (Provider, error) {
		return nil, expectedErr
	}

	factory.Register("failing-provider", creator)

	_, err := factory.Create("failing-provider", "session-1", Config{})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestDefaultFactory_SupportedTypes(t *testing.T) {
	factory := NewDefaultFactory()

	if len(factory.SupportedTypes()) != 0 {
		t.Error("expected no types initially")
	}

	factory.Register("type1", func(string, Config) (Provider, error) { return nil, nil })
	factory.Register("type2", func(string, Config) (Provider, error) { return nil, nil })
	factory.Register("type3", func(string, Config) (Provider, error) { return nil, nil })

	types := factory.SupportedTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 types, got %d", len(types))
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateCreated, "created"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StatePaused, "paused"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateError, "error"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}
