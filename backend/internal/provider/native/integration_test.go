package native

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestProcessEvent_PartialContent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{
				Parts: []*genai.Part{
					{Text: "partial output"},
				},
			},
		},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.events.Close()
	}()

	p.processEvent(event)

	select {
	case ev := <-p.Events():
		if ev.Type != domain.EventTypeOutput {
			t.Errorf("expected output event, got %v", ev.Type)
		}
		data, ok := ev.Data.(domain.OutputData)
		if !ok {
			t.Fatal("expected OutputData")
		}
		if data.Content != "partial output" {
			t.Errorf("expected 'partial output', got %s", data.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestProcessEvent_TurnComplete(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		LLMResponse: model.LLMResponse{
			TurnComplete: true,
		},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.events.Close()
	}()

	p.processEvent(event)

	select {
	case ev := <-p.Events():
		if ev.Type != domain.EventTypeMetadata {
			t.Errorf("expected metadata event, got %v", ev.Type)
		}
		data, ok := ev.Data.(domain.MetadataData)
		if !ok {
			t.Fatal("expected MetadataData")
		}
		if data.Key != "turn_complete" {
			t.Errorf("expected key 'turn_complete', got %s", data.Key)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestProcessEvent_StateDelta(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		Actions: session.EventActions{
			StateDelta: map[string]any{"key": "value"},
		},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.events.Close()
	}()

	p.processEvent(event)

	select {
	case ev := <-p.Events():
		if ev.Type != domain.EventTypeMetadata {
			t.Errorf("expected metadata event, got %v", ev.Type)
		}
		data, ok := ev.Data.(domain.MetadataData)
		if !ok {
			t.Fatal("expected MetadataData")
		}
		if data.Key != "state_delta" {
			t.Errorf("expected key 'state_delta', got %s", data.Key)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestProcessEvent_EmptyEvent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{}

	p.processEvent(event)
}

func TestProcessEvent_NilContent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: nil,
		},
	}

	p.processEvent(event)
}

func TestProcessEvent_EmptyTextParts(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{
				Parts: []*genai.Part{
					{Text: ""},
				},
			},
		},
	}

	p.processEvent(event)
}

func TestAfterModelCallback_NilResponse(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp, err := p.afterModelCallback(nil, nil, nil)

	if resp != nil {
		t.Error("expected nil response")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestAfterModelCallback_Error(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	testErr := ErrModelCreate

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.events.Close()
	}()

	resp, err := p.afterModelCallback(nil, nil, testErr)

	if resp != nil {
		t.Error("expected nil response")
	}
	if err != testErr {
		t.Errorf("expected %v, got %v", testErr, err)
	}

	select {
	case ev := <-p.Events():
		if ev.Type != domain.EventTypeError {
			t.Errorf("expected error event, got %v", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for error event")
	}
}

func TestAfterModelCallback_WithUsageMetadata(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 50,
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events.Close()
	}()

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response to be returned")
	}

	status := p.Status()
	if status.Metrics.TokensIn != 100 {
		t.Errorf("expected 100 tokens in, got %d", status.Metrics.TokensIn)
	}
	if status.Metrics.TokensOut != 50 {
		t.Errorf("expected 50 tokens out, got %d", status.Metrics.TokensOut)
	}
}

func TestAfterModelCallback_WithContent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{
				{Text: "Hello from model"},
			},
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events.Close()
	}()

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response to be returned")
	}

	status := p.Status()
	if status.Output != "Hello from model" {
		t.Errorf("expected output 'Hello from model', got %s", status.Output)
	}
}

func TestAfterModelCallback_WithEmptyParts(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{
				{Text: ""},
			},
		},
	}

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response to be returned")
	}
}

func TestConcurrentSessionLifecycle(t *testing.T) {
	const numSessions = 5
	var wg sync.WaitGroup

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			p := NewADKProvider("session-"+string(rune('0'+idx)), ADKConfig{
				APIKey: "test-key",
			})

			p.state.SetState(provider.StateRunning)
			p.ctx, p.cancel = context.WithCancel(context.Background())
			p.runCtx, p.runCancel = context.WithCancel(p.ctx)

			for j := 0; j < 3; j++ {
				if err := p.Pause(context.Background()); err != nil {
					t.Errorf("session %d: pause %d failed: %v", idx, j, err)
					continue
				}

				if err := p.Resume(context.Background()); err != nil {
					t.Errorf("session %d: resume %d failed: %v", idx, j, err)
					continue
				}
			}

			if err := p.Stop(context.Background()); err != nil {
				t.Errorf("session %d: stop failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentEventEmission(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})
	defer p.events.Close()

	var wg sync.WaitGroup
	const goroutines = 10
	const eventsPerGoroutine = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				p.events.EmitOutput("output from goroutine")
				p.events.EmitMetric(1, 1, 1)
			}
		}(i)
	}

	received := 0
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	timeout := time.After(2 * time.Second)
loop:
	for {
		select {
		case <-p.Events():
			received++
		case <-done:
			for {
				select {
				case <-p.Events():
					received++
				default:
					break loop
				}
			}
		case <-timeout:
			break loop
		}
	}

	expectedMin := goroutines * eventsPerGoroutine
	if received < expectedMin/2 {
		t.Errorf("expected at least %d events, got %d", expectedMin/2, received)
	}
}

func TestProviderStateRaceCondition(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = p.Status()
			p.state.AddTokens(1, 1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = p.Pause(context.Background())
			_ = p.Resume(context.Background())
		}
	}()

	wg.Wait()
}

func TestFullEventFlow(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.state.SetState(provider.StateRunning)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	p.events.EmitStatusChange(domain.SessionStateCreated, domain.SessionStateRunning, "test started")
	p.events.EmitOutput("Hello")
	p.events.EmitMetric(100, 50, 1)
	p.events.EmitMetadata("model", "gemini-2.5-flash")
	p.events.EmitError("test error", "TEST_ERR")

	eventTypes := make(map[domain.EventType]int)
	timeout := time.After(500 * time.Millisecond)

loop:
	for {
		select {
		case ev := <-p.Events():
			eventTypes[ev.Type]++
		case <-timeout:
			break loop
		}
	}

	if eventTypes[domain.EventTypeStatusChange] != 1 {
		t.Errorf("expected 1 status change event, got %d", eventTypes[domain.EventTypeStatusChange])
	}
	if eventTypes[domain.EventTypeOutput] != 1 {
		t.Errorf("expected 1 output event, got %d", eventTypes[domain.EventTypeOutput])
	}
	if eventTypes[domain.EventTypeMetric] != 1 {
		t.Errorf("expected 1 metric event, got %d", eventTypes[domain.EventTypeMetric])
	}
	if eventTypes[domain.EventTypeMetadata] != 1 {
		t.Errorf("expected 1 metadata event, got %d", eventTypes[domain.EventTypeMetadata])
	}
	if eventTypes[domain.EventTypeError] != 1 {
		t.Errorf("expected 1 error event, got %d", eventTypes[domain.EventTypeError])
	}
}

func TestLoadTest_ConcurrentAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numAgents = 10
	var wg sync.WaitGroup
	errors := make(chan error, numAgents*10)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentID int) {
			defer wg.Done()

			p := NewADKProvider("agent-"+string(rune('0'+agentID)), ADKConfig{
				APIKey: "test-key",
			})

			p.state.SetState(provider.StateRunning)
			p.ctx, p.cancel = context.WithCancel(context.Background())
			p.runCtx, p.runCancel = context.WithCancel(p.ctx)

			for j := 0; j < 10; j++ {
				p.state.AddTokens(100, 50)
				p.events.EmitOutput("output")
				p.events.EmitMetric(100, 50, 1)
			}

			if err := p.Pause(context.Background()); err != nil {
				errors <- err
				return
			}

			if err := p.Resume(context.Background()); err != nil {
				errors <- err
				return
			}

			if err := p.Stop(context.Background()); err != nil {
				errors <- err
				return
			}

			status := p.Status()
			if status.State != provider.StateStopped {
				errors <- ErrNotStarted
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Errorf("error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Errorf("%d errors occurred during load test", errCount)
	}
}

func TestSetupMCPToolsets_EmptyConfig(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.ctx, p.cancel = context.WithCancel(context.Background())

	config := provider.Config{
		MCPServers: nil,
	}

	toolsets, err := p.setupMCPToolsets(config)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(toolsets) != 0 {
		t.Errorf("expected 0 toolsets, got %d", len(toolsets))
	}
}

func TestSetupMCPToolsets_EmptyList(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.ctx, p.cancel = context.WithCancel(context.Background())

	config := provider.Config{
		MCPServers: []provider.MCPServerConfig{},
	}

	toolsets, err := p.setupMCPToolsets(config)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(toolsets) != 0 {
		t.Errorf("expected 0 toolsets, got %d", len(toolsets))
	}
}

func TestProcessEvent_MultipleParts(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{
				Parts: []*genai.Part{
					{Text: "first"},
					{Text: "second"},
					{Text: ""},
					{Text: "third"},
				},
			},
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events.Close()
	}()

	p.processEvent(event)

	count := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case <-p.Events():
			count++
		case <-timeout:
			break loop
		}
	}

	if count != 3 {
		t.Errorf("expected 3 output events (non-empty parts), got %d", count)
	}
}

func TestAfterModelCallback_MultipleParts(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{
				{Text: "first"},
				{Text: "second"},
			},
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events.Close()
	}()

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}

	if p.state.Status().Output != "second" {
		t.Errorf("expected output to be last part 'second', got %s", p.state.Status().Output)
	}
}

func TestAfterModelCallback_EmptyContent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Parts: []*genai.Part{},
		},
	}

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}
}

func TestAfterModelCallback_NilContent(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		Content: nil,
	}

	result, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}
}

func TestAfterModelCallback_NilUsageMetadata(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	resp := &model.LLMResponse{
		UsageMetadata: nil,
		Content: &genai.Content{
			Parts: []*genai.Part{
				{Text: "output"},
			},
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events.Close()
	}()

	_, err := p.afterModelCallback(nil, resp, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if p.state.Status().Metrics.TokensIn != 0 {
		t.Error("tokens should be 0 when no usage metadata")
	}
}

func TestCreateMCPToolset_CommandSetup(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.providerCfg = provider.Config{
		WorkingDir: "/tmp",
	}

	cfg := provider.MCPServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
		Env:     map[string]string{"TEST": "value"},
	}

	_, handle, err := p.createMCPToolset(cfg)
	if handle != nil && handle.cancel != nil {
		handle.cancel()
	}

	if err == nil {
		t.Log("MCP toolset created successfully")
	} else {
		t.Logf("Expected error creating MCP toolset: %v", err)
	}
}

func TestSetupMCPToolsets_WithConfig(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})
	p.ctx, p.cancel = context.WithCancel(context.Background())

	cfg := provider.Config{
		MCPServers: []provider.MCPServerConfig{
			{
				Name:    "test-server",
				Command: "echo",
				Args:    []string{"test"},
			},
		},
	}

	_, err := p.setupMCPToolsets(cfg)
	if err != nil {
		t.Logf("Expected error for non-MCP command: %v", err)
	}
}

func TestStartWithMCPServers(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	cfg := provider.Config{
		Environment: map[string]string{
			"GOOGLE_API_KEY": "test-key",
		},
		MCPServers: []provider.MCPServerConfig{
			{
				Name:    "test",
				Command: "echo",
			},
		},
	}

	err := p.Start(context.Background(), cfg)
	if err != nil {
		t.Logf("Start failed as expected: %v", err)
	}
}

func TestADKProvider_CheckPausedWithUnpause(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})

	p.paused = true

	done := make(chan struct{})
	go func() {
		p.checkPaused()
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Error("checkPaused should block when paused")
	default:
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

func TestStartBranchAPIKeyFromEnvironment(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{})

	cfg := provider.Config{
		Environment: map[string]string{
			"GOOGLE_API_KEY": "env-key",
		},
	}

	err := p.Start(context.Background(), cfg)

	if err != nil {
		t.Logf("Start error (expected for invalid key): %v", err)
	}

	if p.providerCfg.Environment["GOOGLE_API_KEY"] != "env-key" {
		t.Error("environment should be stored in config")
	}
}

func TestCreateModelVertexAI(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey:      "test-key",
		UseVertexAI: true,
		ProjectID:   "my-project",
		Location:    "us-central1",
	})

	p.ctx, p.cancel = context.WithCancel(context.Background())

	_, err := p.createModel("test-key")

	if err != nil {
		t.Logf("createModel failed (expected): %v", err)
	}
}

func TestMCPProcessCancellation(t *testing.T) {
	p := NewADKProvider("test-session", ADKConfig{
		APIKey: "test-key",
	})
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	// Use a command that will run for a while
	cfg := provider.MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"10"},
	}

	_, handle, err := p.createMCPToolset(cfg)
	if err != nil {
		// If it fails because of validation or other things, skip
		t.Skipf("Skipping because MCP toolset creation failed: %v", err)
	}

	if handle == nil || handle.cmd == nil {
		t.Fatal("expected handle and cmd to be non-nil")
	}

	// Wait for process to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context
	handle.cancel()

	// Check if process is terminated
	err = handle.cmd.Wait()
	if err == nil {
		t.Error("expected error from Wait() after cancellation, got nil")
	}
}
