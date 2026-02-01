package native

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider"
)

func TestNewADKProvider(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
		Model:  "gemini-2.5-flash",
	})

	if p == nil {
		t.Fatal("expected provider to be created")
	}

	if p.sessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got %s", p.sessionID)
	}

	if p.config.Model != "gemini-2.5-flash" {
		t.Errorf("expected model 'gemini-2.5-flash', got %s", p.config.Model)
	}

	status := p.Status()
	if status.State != provider.StateCreated {
		t.Errorf("expected initial state to be StateCreated, got %v", status.State)
	}
}

func TestNewADKProvider_DefaultModel(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	if p.config.Model != DefaultModel {
		t.Errorf("expected default model %s, got %s", DefaultModel, p.config.Model)
	}
}

func TestADKProvider_StartWithoutAPIKey(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{})

	err := p.Start(context.Background(), provider.Config{})

	if err != ErrAPIKey {
		t.Errorf("expected ErrAPIKey, got %v", err)
	}

	status := p.Status()
	if status.State != provider.StateError {
		t.Errorf("expected state to be StateError, got %v", status.State)
	}
}

func TestADKProvider_StartAlreadyStarted(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)

	err := p.Start(context.Background(), provider.Config{})

	if err != ErrAlreadyStarted {
		t.Errorf("expected ErrAlreadyStarted, got %v", err)
	}
}

func TestADKProvider_Stop(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	err := p.Stop(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != provider.StateStopped {
		t.Errorf("expected state to be StateStopped, got %v", status.State)
	}
}

func TestADKProvider_StopAlreadyStopped(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateStopped)

	err := p.Stop(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestADKProvider_Pause(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)

	err := p.Pause(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != provider.StatePaused {
		t.Errorf("expected state to be StatePaused, got %v", status.State)
	}

	if !p.paused {
		t.Error("expected paused flag to be true")
	}
}

func TestADKProvider_PauseNotRunning(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	err := p.Pause(context.Background())

	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestADKProvider_PauseAlreadyPaused(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.paused = true

	err := p.Pause(context.Background())

	if err != ErrAlreadyPaused {
		t.Errorf("expected ErrAlreadyPaused, got %v", err)
	}
}

func TestADKProvider_Resume(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StatePaused)
	p.paused = true

	err := p.Resume(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != provider.StateRunning {
		t.Errorf("expected state to be StateRunning, got %v", status.State)
	}

	if p.paused {
		t.Error("expected paused flag to be false")
	}
}

func TestADKProvider_ResumeNotPaused(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)

	err := p.Resume(context.Background())

	if err != ErrNotPaused {
		t.Errorf("expected ErrNotPaused, got %v", err)
	}
}

func TestADKProvider_Kill(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.paused = true

	err := p.Kill()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != provider.StateStopped {
		t.Errorf("expected state to be StateStopped, got %v", status.State)
	}

	if p.paused {
		t.Error("expected paused flag to be false after kill")
	}
}

func TestADKProvider_Events(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	events := p.Events()
	if events == nil {
		t.Error("expected events channel to be non-nil")
	}
}

func TestADKProvider_CheckPaused(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	done := make(chan struct{})
	go func() {
		p.checkPaused()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("checkPaused should return immediately when not paused")
	}
}

func TestADKProvider_CheckPausedBlocks(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.pauseMu.Lock()
	p.paused = true
	p.pauseMu.Unlock()

	done := make(chan struct{})
	go func() {
		p.checkPaused()
		close(done)
	}()

	select {
	case <-done:
		t.Error("checkPaused should block when paused")
	case <-time.After(50 * time.Millisecond):
	}

	p.pauseMu.Lock()
	p.paused = false
	p.pauseCond.Broadcast()
	p.pauseMu.Unlock()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("checkPaused should unblock after resume")
	}
}

func TestADKProvider_RunPromptNotStarted(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	err := p.RunPrompt(context.Background(), "test prompt")

	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestADKProvider_ConcurrentOperations(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Status()
		}()
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Pause(context.Background())
			_ = p.Resume(context.Background())
		}()
	}

	wg.Wait()
}

func TestEnvMapToSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected int
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: 0,
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: 0,
		},
		{
			name: "single entry",
			input: map[string]string{
				"KEY": "value",
			},
			expected: 1,
		},
		{
			name: "multiple entries",
			input: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
				"KEY3": "value3",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := envMapToSlice(tt.input)
			if len(result) != tt.expected {
				t.Errorf("expected %d entries, got %d", tt.expected, len(result))
			}

			if tt.input != nil {
				for _, entry := range result {
					found := false
					for k, v := range tt.input {
						if entry == k+"="+v {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("unexpected entry: %s", entry)
					}
				}
			}
		})
	}
}

func TestADKProvider_ImplementsProviderInterface(t *testing.T) {
	var _ provider.Provider = (*ADKProvider)(nil)
}
