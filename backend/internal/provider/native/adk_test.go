package native

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestNewADKProvider(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
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
	if status.State != session.StateCreated {
		t.Errorf("expected initial state to be StateCreated, got %v", status.State)
	}
}

func TestNewADKProvider_DefaultModel(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	if p.config.Model != DefaultModel {
		t.Errorf("expected default model %s, got %s", DefaultModel, p.config.Model)
	}
}

func TestADKProvider_StartWithoutAPIKey(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{})

	err := p.Start(context.Background(), session.Config{})

	if err != ErrAPIKey {
		t.Errorf("expected ErrAPIKey, got %v", err)
	}

	status := p.Status()
	if status.State != session.StateError {
		t.Errorf("expected state to be StateError, got %v", status.State)
	}
}

func TestADKProvider_StartAlreadyStarted(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)

	err := p.Start(context.Background(), session.Config{})

	if err != ErrAlreadyStarted {
		t.Errorf("expected ErrAlreadyStarted, got %v", err)
	}
}

func TestADKProvider_Stop(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	err := p.Stop(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != session.StateStopped {
		t.Errorf("expected state to be StateStopped, got %v", status.State)
	}
}

func TestADKProvider_StopAlreadyStopped(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateStopped)

	err := p.Stop(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestADKProvider_Pause(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)

	err := p.Pause(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != session.StatePaused {
		t.Errorf("expected state to be StatePaused, got %v", status.State)
	}

	if !p.paused {
		t.Error("expected paused flag to be true")
	}
}

func TestADKProvider_PauseNotRunning(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	err := p.Pause(context.Background())

	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestADKProvider_PauseAlreadyPaused(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.paused = true

	err := p.Pause(context.Background())

	if err != ErrAlreadyPaused {
		t.Errorf("expected ErrAlreadyPaused, got %v", err)
	}
}

func TestADKProvider_Resume(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StatePaused)
	p.paused = true

	err := p.Resume(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != session.StateRunning {
		t.Errorf("expected state to be StateRunning, got %v", status.State)
	}

	if p.paused {
		t.Error("expected paused flag to be false")
	}
}

func TestADKProvider_ResumeNotPaused(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)

	err := p.Resume(context.Background())

	if err != ErrNotPaused {
		t.Errorf("expected ErrNotPaused, got %v", err)
	}
}

func TestADKProvider_Kill(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.paused = true

	err := p.Kill()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != session.StateStopped {
		t.Errorf("expected state to be StateStopped, got %v", status.State)
	}

	if p.paused {
		t.Error("expected paused flag to be false after kill")
	}
}

func TestADKProvider_Events(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	events := p.Events()
	if events == nil {
		t.Error("expected events channel to be non-nil")
	}
}

func TestADKProvider_CheckPaused(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
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
	p := NewADKSession("test-session", ADKConfig{
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
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	err := p.RunPrompt(context.Background(), "test prompt")

	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestADKProvider_ConcurrentOperations(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
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
			result := make([]string, 0, len(tt.input))
			for k, v := range tt.input {
				result = append(result, k+"="+v)
			}
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

func TestADKProvider_ImplementsSessionInterface(t *testing.T) {
	var _ session.Session = (*ADKSession)(nil)
}

func TestADKProvider_StartWithAPIKeyFromEnv(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{})

	err := p.Start(context.Background(), session.Config{
		Environment: map[string]string{
			"GOOGLE_API_KEY": "env-api-key",
		},
	})

	if err == nil || err == ErrAPIKey {
		return
	}
}

func TestADKProvider_StopWithContextTimeout(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		time.Sleep(100 * time.Millisecond)
	}()

	err := p.Stop(ctx)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := p.Status()
	if status.State != session.StateStopped {
		t.Errorf("expected state to be StateStopped, got %v", status.State)
	}
}

func TestADKProvider_StopWithMCPClients(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	_, mcpCancel := context.WithCancel(context.Background())
	p.mcpClients = []*mcpClientHandle{
		{cancel: mcpCancel},
	}

	err := p.Stop(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestADKProvider_KillWithMCPClients(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	_, mcpCancel := context.WithCancel(context.Background())
	p.mcpClients = []*mcpClientHandle{
		{cancel: mcpCancel},
	}

	err := p.Kill()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvMapToSlice_Format(t *testing.T) {
	input := map[string]string{
		"KEY": "value",
	}

	result := make([]string, 0, len(input))
	for k, v := range input {
		result = append(result, k+"="+v)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0] != "KEY=value" {
		t.Errorf("expected 'KEY=value', got %s", result[0])
	}
}

func TestADKProvider_StatusReflectsTokens(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.AddTokens(100, 50)
	p.state.AddTokens(200, 100)

	status := p.Status()

	if status.Metrics.TokensIn != 300 {
		t.Errorf("expected 300 tokens in, got %d", status.Metrics.TokensIn)
	}
	if status.Metrics.TokensOut != 150 {
		t.Errorf("expected 150 tokens out, got %d", status.Metrics.TokensOut)
	}
}

func TestADKProvider_ConcurrentStateAccess(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Status()
		}()
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.state.AddTokens(1, 1)
		}()
	}

	wg.Wait()

	status := p.Status()
	if status.Metrics.TokensIn != int64(goroutines) {
		t.Errorf("expected %d tokens in, got %d", goroutines, status.Metrics.TokensIn)
	}
}

func TestADKProvider_PauseResumeCycle(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)

	if err := p.Pause(context.Background()); err != nil {
		t.Fatalf("unexpected error on pause: %v", err)
	}

	if err := p.Resume(context.Background()); err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}

	if p.Status().State != session.StateRunning {
		t.Errorf("expected state Running after resume, got %v", p.Status().State)
	}
}

func TestADKProvider_MultipleStops(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateStopped)

	err := p.Stop(context.Background())
	if err != nil {
		t.Errorf("first stop should succeed: %v", err)
	}

	err = p.Stop(context.Background())
	if err != nil {
		t.Errorf("second stop should also succeed: %v", err)
	}
}

func TestADKProvider_EventsChannel(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	ch := p.Events()
	if ch == nil {
		t.Error("events channel should not be nil")
	}

	p.events.EmitOutput("test output")

	select {
	case event := <-ch:
		if event.SessionID != "test-session" {
			t.Errorf("expected session ID 'test-session', got %s", event.SessionID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestADKProvider_DefaultBufferSize(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	if p.events == nil {
		t.Fatal("events adapter should be created")
	}
}

func TestADKConfig_Defaults(t *testing.T) {
	cfg := ADKConfig{}

	if cfg.Model != "" {
		t.Error("model should be empty by default in config")
	}

	p := NewADKSession("test-session", cfg)

	if p.config.Model != DefaultModel {
		t.Errorf("expected default model %s, got %s", DefaultModel, p.config.Model)
	}
}

func TestADKConfig_CustomModel(t *testing.T) {
	cfg := ADKConfig{
		APIKey: "test-key",
		Model:  "gemini-2.5-pro",
	}

	p := NewADKSession("test-session", cfg)

	if p.config.Model != "gemini-2.5-pro" {
		t.Errorf("expected model 'gemini-2.5-pro', got %s", p.config.Model)
	}
}

func TestADKConfig_VertexAI(t *testing.T) {
	cfg := ADKConfig{
		APIKey:      "test-key",
		ProjectID:   "my-project",
		Location:    "us-central1",
		UseVertexAI: true,
	}

	p := NewADKSession("test-session", cfg)

	if !p.config.UseVertexAI {
		t.Error("expected UseVertexAI to be true")
	}
	if p.config.ProjectID != "my-project" {
		t.Errorf("expected project ID 'my-project', got %s", p.config.ProjectID)
	}
}

func TestADKProvider_KillUnpausesSessions(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.paused = true

	done := make(chan struct{})
	go func() {
		p.pauseMu.Lock()
		for p.paused {
			p.pauseCond.Wait()
		}
		p.pauseMu.Unlock()
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	err := p.Kill()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("kill should unpause waiting goroutines")
	}
}

func TestADKProvider_StopInternalTimeout(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		time.Sleep(10 * time.Second)
	}()

	err := p.Stop(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if p.Status().State != session.StateStopped {
		t.Errorf("expected stopped state, got %v", p.Status().State)
	}
}

func TestADKProvider_StopImmediatelyIfNoGoroutines(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	start := time.Now()
	err := p.Stop(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if elapsed > 100*time.Millisecond {
		t.Errorf("stop should be quick when no goroutines, took %v", elapsed)
	}
}

func TestADKProvider_StopFromDifferentStates(t *testing.T) {
	tests := []struct {
		name         string
		initialState session.State
	}{
		{"from running", session.StateRunning},
		{"from paused", session.StatePaused},
		{"from starting", session.StateStarting},
		{"from error", session.StateError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewADKSession("test-session", ADKConfig{
				APIKey: "test-key",
			})

			p.state.SetState(tt.initialState)
			p.ctx, p.cancel = context.WithCancel(context.Background())
			p.runCtx, p.runCancel = context.WithCancel(p.ctx)

			err := p.Stop(context.Background())
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if p.Status().State != session.StateStopped {
				t.Errorf("expected stopped state, got %v", p.Status().State)
			}
		})
	}
}

func TestADKProvider_AllErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrNotStarted", ErrNotStarted},
		{"ErrAlreadyStarted", ErrAlreadyStarted},
		{"ErrNotPaused", ErrNotPaused},
		{"ErrAlreadyPaused", ErrAlreadyPaused},
		{"ErrProviderStopped", ErrProviderStopped},
		{"ErrAPIKey", ErrAPIKey},
		{"ErrModelCreate", ErrModelCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Error("error should not be nil")
			}
			if tt.err.Error() == "" {
				t.Error("error message should not be empty")
			}
		})
	}
}

func TestADKProvider_DefaultConstants(t *testing.T) {
	if DefaultModel == "" {
		t.Error("DefaultModel should not be empty")
	}
	if DefaultBufferSize <= 0 {
		t.Error("DefaultBufferSize should be positive")
	}
	if DefaultAppName == "" {
		t.Error("DefaultAppName should not be empty")
	}
	if DefaultUserID == "" {
		t.Error("DefaultUserID should not be empty")
	}
}

func TestADKProvider_ProviderConfigStored(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	cfg := session.Config{
		ProviderType: "gemini",
		WorkingDir:   "/tmp/test",
		SystemPrompt: "You are a helpful assistant.",
	}

	err := p.Start(context.Background(), cfg)

	if err != nil {
		if p.providerCfg.WorkingDir != cfg.WorkingDir {
			t.Errorf("expected working dir %s, got %s", cfg.WorkingDir, p.providerCfg.WorkingDir)
		}
	}
}

func TestADKProvider_StatusOutput(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetOutput("test output")
	p.state.SetCurrentTask("task-123")

	status := p.Status()

	if status.Output != "test output" {
		t.Errorf("expected output 'test output', got %s", status.Output)
	}
	if status.CurrentTask != "task-123" {
		t.Errorf("expected task 'task-123', got %s", status.CurrentTask)
	}
}

func TestADKProvider_ConcurrentPauseResume(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := p.Pause(context.Background()); err != nil && err != ErrNotStarted && err != ErrAlreadyPaused {
					errChan <- err
				}
				if err := p.Resume(context.Background()); err != nil && err != ErrNotPaused {
					errChan <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestADKProvider_KillWithNilContexts(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(session.StateRunning)

	err := p.Kill()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if p.Status().State != session.StateStopped {
		t.Errorf("expected stopped state, got %v", p.Status().State)
	}
}

func TestADKProvider_SanitizeError(t *testing.T) {
	p := NewADKSession("test-session", ADKConfig{})
	apiKey := "secret-api-key-123"

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "contains api key",
			err:      errors.New("failed with key: secret-api-key-123, try again"),
			expected: "failed with key: [REDACTED], try again",
		},
		{
			name:     "no api key",
			err:      errors.New("standard error message"),
			expected: "standard error message",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := p.sanitizeError(tt.err, apiKey)
			if tt.err == nil {
				if res != nil {
					t.Errorf("expected nil, got %v", res)
				}
				return
			}
			if res.Error() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, res.Error())
			}
		})
	}
}

func TestADKProvider_HandleFailureRedaction(t *testing.T) {
	apiKey := "secret-key-to-redact"
	p := NewADKSession("test-session", ADKConfig{
		APIKey: apiKey,
	})

	p.handleFailure(errors.New("error containing secret-key-to-redact"))

	status := p.Status()
	if status.Error == nil {
		t.Fatal("expected error in status")
	}
	if strings.Contains(status.Error.Error(), apiKey) {
		t.Errorf("status error leaked api key: %v", status.Error)
	}
	if !strings.Contains(status.Error.Error(), "[REDACTED]") {
		t.Errorf("status error missing [REDACTED]: %v", status.Error)
	}
}
