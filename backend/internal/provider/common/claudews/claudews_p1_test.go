package claudews

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestProcessInput_InitializeRequestSentBeforeFirstUserMessage(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(1)
	p.config = session.Config{Custom: map[string]any{"claudews_initialize": true, "claudews_initialize_timeout_ms": 200}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("queue input: %v", err)
	}

	first := <-outbound
	firstPayload := decodeJSONMap(t, first)
	if firstPayload["type"] != "control_request" {
		t.Fatalf("expected first outbound message to be control_request, got %v", firstPayload["type"])
	}
	requestID, _ := firstPayload["request_id"].(string)
	request, _ := firstPayload["request"].(map[string]any)
	if request["subtype"] != "initialize" {
		t.Fatalf("expected initialize subtype, got %+v", request)
	}

	p.dispatchMessage([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{"output_style":"default"}}}`))

	second := <-outbound
	secondPayload := decodeJSONMap(t, second)
	if secondPayload["type"] != "user" {
		t.Fatalf("expected second outbound message to be user, got %v", secondPayload["type"])
	}

	p.dispatchMessage([]byte(`{"type":"keep_alive"}`))
	p.cancel()
}

func TestProcessInput_InitializeRejectedFailsClearly(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(1)
	p.config = session.Config{Custom: map[string]any{"claudews_initialize": true, "claudews_initialize_timeout_ms": 200}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("queue input: %v", err)
	}

	first := <-outbound
	firstPayload := decodeJSONMap(t, first)
	requestID, _ := firstPayload["request_id"].(string)
	p.dispatchMessage([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"` + requestID + `","error":"Already initialized"}}`))

	if errEvent := waitForErrorEventByCode(t, p.events.Events(), "CLAUDEWS_INITIALIZE_FAILED"); !strings.Contains(errEvent.Message, "Already initialized") {
		t.Fatalf("expected initialize rejection details in error event, got %q", errEvent.Message)
	}

	select {
	case msg := <-outbound:
		payload := decodeJSONMap(t, msg)
		if payload["type"] == "user" {
			t.Fatalf("did not expect user message after initialize rejection: %s", string(msg))
		}
	case <-time.After(50 * time.Millisecond):
	}

	p.cancel()
}

func TestProcessInput_InitializeTimeoutFailsClearly(t *testing.T) {
	wc, _, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(1)
	p.config = session.Config{Custom: map[string]any{"claudews_initialize": true, "claudews_initialize_timeout_ms": 20}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("queue input: %v", err)
	}

	errEvent := waitForErrorEventByCode(t, p.events.Events(), "CLAUDEWS_INITIALIZE_FAILED")
	if !strings.Contains(strings.ToLower(errEvent.Message), "timed out") {
		t.Fatalf("expected timeout in initialize error, got %q", errEvent.Message)
	}

	p.cancel()
}

func TestHandleControlRequest_HookCallbackReturnsControlError(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc

	p.handleControlRequest(RawMessage{Raw: []byte(`{"type":"control_request","request_id":"req-hook","request":{"subtype":"hook_callback","callback_id":"cb-1","input":{"x":1}}}`)})

	response := decodeJSONMap(t, <-outbound)
	assertControlErrorResponse(t, response, "req-hook", "hook_callback")
}

func TestHandleControlRequest_UnsupportedSubtypeReturnsControlError(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc

	p.handleControlRequest(RawMessage{Raw: []byte(`{"type":"control_request","request_id":"req-unsupported","request":{"subtype":"mcp_status"}}`)})

	response := decodeJSONMap(t, <-outbound)
	assertControlErrorResponse(t, response, "req-unsupported", "unsupported")
}

func TestWSConn_StartPingDetectsDeadConnection(t *testing.T) {
	wc, _, cleanup := newTestWSConn(t)
	cleanup()

	failCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wc.StartPing(ctx, 10*time.Millisecond, 1, func(err error) {
		failCh <- err
	})

	select {
	case err := <-failCh:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ping write failed") {
			t.Fatalf("expected ping write liveness error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for liveness failure callback")
	}
}

func TestHandleResultMsg_EmitsTurnCompletionSystemMessageAfterOutput(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	p.turnSeq = 2
	rm := RawMessage{Raw: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done","duration_ms":12.5,"duration_api_ms":9.1,"num_turns":2,"total_cost_usd":0.01,"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":5}}`)}

	p.handleResultMsg(rm)

	events := p.events.Events()
	seenCompletion := false
	seenOutput := false
	seenCompletionProgress := false
	seenIdleStatus := false
	deadline := time.After(500 * time.Millisecond)
	for !seenCompletion {
		select {
		case ev := <-events:
			switch ev.Type {
			case domain.EventTypeOutput:
				seenOutput = true
			case domain.EventTypeSystemMessage:
				sm, ok := ev.SystemMessage()
				if !ok || !strings.Contains(strings.ToLower(sm.Content), "turn 2") {
					continue
				}
				if !seenOutput {
					t.Fatal("expected output event before turn completion message")
				}
				seenCompletion = true
			case domain.EventTypeProgress:
				progress, ok := ev.Progress()
				if ok && progress.Done && progress.Channel == "turn_completion" {
					seenCompletionProgress = true
				}
			case domain.EventTypeStatusChange:
				status, ok := ev.StatusChange()
				if ok && status.NewState == domain.SessionStateIdle {
					seenIdleStatus = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for turn completion message (completion=%v output=%v)", seenCompletion, seenOutput)
		}
	}
	for _, ev := range drainEvents(events) {
		switch ev.Type {
		case domain.EventTypeProgress:
			progress, ok := ev.Progress()
			if ok && progress.Done && progress.Channel == "turn_completion" {
				seenCompletionProgress = true
			}
		case domain.EventTypeStatusChange:
			status, ok := ev.StatusChange()
			if ok && status.NewState == domain.SessionStateIdle {
				seenIdleStatus = true
			}
		}
	}
	if !seenCompletionProgress {
		t.Fatal("expected progress done completion marker")
	}
	if !seenIdleStatus {
		t.Fatal("expected running->idle status change on turn completion")
	}
}

func TestDrainAtTurnBoundary_WaitsUntilAllInFlightTurnsComplete(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	p.turnSeq = 2
	p.turnInFlight = 2
	p.turnBoundaryCh = make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.DrainAtTurnBoundary(ctx)
	}()

	p.handleResultMsg(RawMessage{Raw: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"first","duration_ms":1,"duration_api_ms":1,"num_turns":1,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`)})

	select {
	case err := <-done:
		t.Fatalf("drain returned after first completion: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	p.handleResultMsg(RawMessage{Raw: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"second","duration_ms":1,"duration_api_ms":1,"num_turns":2,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`)})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drain returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for drain completion")
	}
}

func TestProcessInput_TwoTurnReentryUsesSessionIDAfterInit(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(2)
	p.config = session.Config{Custom: map[string]any{"claudews_no_response_timeout_ms": 2000}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "first"); err != nil {
		t.Fatalf("queue first input: %v", err)
	}
	initialize := decodeJSONMap(t, <-outbound)
	if initialize["type"] != "control_request" {
		t.Fatalf("expected initialize control_request before first user message, got %v", initialize["type"])
	}
	requestID, _ := initialize["request_id"].(string)
	if requestID == "" {
		t.Fatalf("expected initialize request_id, got payload: %v", initialize)
	}
	p.dispatchMessage([]byte(`{"type":"control_response","response":{"request_id":"` + requestID + `","subtype":"success","response":{}}}`))

	first := decodeJSONMap(t, <-outbound)
	if first["type"] != "user" {
		t.Fatalf("expected first outbound message to be user, got %v", first["type"])
	}
	if first["session_id"] != "" {
		t.Fatalf("expected first turn to send empty session_id, got %+v", first["session_id"])
	}

	p.dispatchMessage([]byte(`{"type":"system","subtype":"init","session_id":"cli-session-1","cwd":"/tmp","model":"claude","claude_code_version":"1","permissionMode":"default","apiKeySource":"test","tools":[],"mcp_servers":[]}`))
	p.dispatchMessage([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"ok","duration_ms":1,"duration_api_ms":1,"num_turns":1,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`))

	if err := p.inputBuffer.Send(context.Background(), "second"); err != nil {
		t.Fatalf("queue second input: %v", err)
	}
	second := decodeJSONMap(t, <-outbound)
	if second["type"] != "user" {
		t.Fatalf("expected second outbound message to be user, got %v", second["type"])
	}
	if second["session_id"] != "cli-session-1" {
		t.Fatalf("expected second turn to use initialized session_id, got %+v", second["session_id"])
	}

	p.dispatchMessage([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"ok","duration_ms":1,"duration_api_ms":1,"num_turns":2,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`))
	p.cancel()
}

func TestProcessInput_FirstTurnDoesNotWaitForInitSessionID(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(1)
	p.config = session.Config{Custom: map[string]any{"claudews_initialize_timeout_ms": 5000}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("queue input: %v", err)
	}

	select {
	case initializeMsg := <-outbound:
		initialize := decodeJSONMap(t, initializeMsg)
		if initialize["type"] != "control_request" {
			t.Fatalf("expected outbound initialize control_request, got %v", initialize["type"])
		}
		requestID, _ := initialize["request_id"].(string)
		if requestID == "" {
			t.Fatalf("expected initialize request_id, got payload: %v", initialize)
		}
		p.dispatchMessage([]byte(`{"type":"control_response","response":{"request_id":"` + requestID + `","subtype":"success","response":{}}}`))
		payload := decodeJSONMap(t, <-outbound)
		if payload["type"] != "user" {
			t.Fatalf("expected outbound user message, got %v", payload["type"])
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected first turn initialize+send sequence to start promptly")
	}

	p.cancel()
}

func decodeJSONMap(t *testing.T, msg []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(msg), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func waitForErrorEventByCode(t *testing.T, events <-chan domain.Event, code string) domain.ErrorData {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != domain.EventTypeError {
				continue
			}
			errData, ok := ev.Error()
			if !ok {
				continue
			}
			if errData.Code == code {
				return errData
			}
		case <-deadline:
			t.Fatalf("timed out waiting for error event code %q", code)
		}
	}
}

func assertControlErrorResponse(t *testing.T, response map[string]any, requestID, expectedContains string) {
	t.Helper()
	if response["type"] != "control_response" {
		t.Fatalf("expected control_response, got %+v", response)
	}
	body, _ := response["response"].(map[string]any)
	if body["subtype"] != "error" {
		t.Fatalf("expected control_response subtype error, got %+v", body)
	}
	if body["request_id"] != requestID {
		t.Fatalf("expected request_id %q, got %+v", requestID, body)
	}
	message, _ := body["error"].(string)
	if !strings.Contains(strings.ToLower(message), strings.ToLower(expectedContains)) {
		t.Fatalf("expected error message containing %q, got %q", expectedContains, message)
	}
}
