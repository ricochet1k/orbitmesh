package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// createSessionViaHTTP POSTs a session against a live httptest.Server and
// returns the session ID.
func createSessionViaHTTP(t *testing.T, baseURL string) string {
	t.Helper()
	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp/test",
	})
	resp, err := http.Post(baseURL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
	}
	var session apiTypes.SessionResponse
	_ = json.NewDecoder(resp.Body).Decode(&session)
	return session.ID
}

type sseMessage struct {
	ID    string
	Event string
	Data  string
}

// readSSEMessages launches a goroutine that parses SSE lines from resp.Body
// and sends decoded frames on the returned channel. The channel is closed
// when the body is closed or EOF is reached.
func readSSEMessages(resp *http.Response) <-chan sseMessage {
	ch := make(chan sseMessage, 10)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		var dataLine string
		var eventType string
		var idLine string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				idLine = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				dataLine = strings.TrimPrefix(line, "data: ")
			case line == "" && dataLine != "":
				ch <- sseMessage{ID: idLine, Event: eventType, Data: dataLine}
				dataLine = ""
				eventType = ""
				idLine = ""
			}
		}
	}()
	return ch
}

// readSSEEvents launches a goroutine that parses SSE lines from resp.Body
// and sends decoded events on the returned channel. The channel is closed
// when the body is closed or EOF is reached.
func readSSEEvents(resp *http.Response) <-chan apiTypes.Event {
	ch := make(chan apiTypes.Event, 10)
	frames := readSSEMessages(resp)
	go func() {
		defer close(ch)
		for frame := range frames {
			if frame.Event == "heartbeat" {
				continue
			}
			var ev apiTypes.Event
			_ = json.Unmarshal([]byte(frame.Data), &ev)
			if ev.Type == "" {
				continue
			}
			ch <- ev
		}
	}()
	return ch
}

// ---------------------------------------------------------------------------
// GET /api/sessions/{id}/events  —  session not found
// ---------------------------------------------------------------------------

func TestSSE_NotFound(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/sessions/nonexistent/events")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// response headers
// ---------------------------------------------------------------------------

func TestSSE_Headers(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

// ---------------------------------------------------------------------------
// single event delivery
// ---------------------------------------------------------------------------

func TestSSE_OutputEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "hello world", nil))

	select {
	case ev := <-events:
		if ev.Type != apiTypes.EventTypeOutput {
			t.Errorf("Type = %q, want %q", ev.Type, apiTypes.EventTypeOutput)
		}
		if ev.SessionID != sessionID {
			t.Errorf("SessionID = %q, want %q", ev.SessionID, sessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

// ---------------------------------------------------------------------------
// multiple events, mixed types
// ---------------------------------------------------------------------------

func TestSSE_MultipleEvents(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "first", nil))
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "second", nil))
	env.broadcaster.Broadcast(domain.NewErrorEvent(sessionID, "oops", "ERR_TEST", nil))

	var collected []apiTypes.Event
	timeout := time.After(2 * time.Second)
	for len(collected) < 3 {
		select {
		case ev := <-events:
			collected = append(collected, ev)
		case <-timeout:
			t.Fatalf("timed out; collected %d of 3 events", len(collected))
		}
	}

	if collected[0].Type != apiTypes.EventTypeOutput {
		t.Errorf("event[0].Type = %q, want output", collected[0].Type)
	}
	if collected[2].Type != apiTypes.EventTypeError {
		t.Errorf("event[2].Type = %q, want error", collected[2].Type)
	}
}

// ---------------------------------------------------------------------------
// status-change event
// ---------------------------------------------------------------------------

func TestSSE_StatusChangeEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "started", nil))

	select {
	case ev := <-events:
		if ev.Type != apiTypes.EventTypeStatusChange {
			t.Errorf("Type = %q, want status_change", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for status_change event")
	}
}

// ---------------------------------------------------------------------------
// metric event
// ---------------------------------------------------------------------------

func TestSSE_MetricEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	env.broadcaster.Broadcast(domain.NewMetricEvent(sessionID, 100, 50, 3, nil))

	select {
	case ev := <-events:
		if ev.Type != apiTypes.EventTypeMetric {
			t.Errorf("Type = %q, want metric", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for metric event")
	}
}

// ---------------------------------------------------------------------------
// metadata event
// ---------------------------------------------------------------------------

func TestSSE_MetadataEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	env.broadcaster.Broadcast(domain.NewMetadataEvent(sessionID, "current_task", "do-thing", nil))

	select {
	case ev := <-events:
		if ev.Type != apiTypes.EventTypeMetadata {
			t.Errorf("Type = %q, want metadata", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for metadata event")
	}
}

// ---------------------------------------------------------------------------
// client disconnect cleans up subscriber
// ---------------------------------------------------------------------------

func TestSSE_ClientDisconnect_Cleanup(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)
	before := env.broadcaster.SubscriberCount()

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}

	// Give the server a moment to register the subscriber.
	time.Sleep(50 * time.Millisecond)
	during := env.broadcaster.SubscriberCount()
	if during != before+1 {
		t.Errorf("subscriber count during stream = %d, want %d", during, before+1)
	}

	// Disconnect
	resp.Body.Close()

	// Poll until the server-side goroutine detects the close and unsubscribes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if env.broadcaster.SubscriberCount() == before {
			return // subscriber cleaned up
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscriber not cleaned up after disconnect (count = %d, want %d)",
		env.broadcaster.SubscriberCount(), before)
}

// ---------------------------------------------------------------------------
// events for a different session are not delivered
// ---------------------------------------------------------------------------

func TestSSE_SessionIsolation(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionA := createSessionViaHTTP(t, srv.URL)
	sessionB := createSessionViaHTTP(t, srv.URL)

	// Subscribe only to session A
	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionA + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	events := readSSEEvents(resp)

	// Broadcast to session B only
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionB, "for B only", nil))

	// Broadcast to session A
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionA, "for A", nil))

	// We should only see the event for A
	select {
	case ev := <-events:
		if ev.SessionID != sessionA {
			t.Errorf("received event for session %q, want %q", ev.SessionID, sessionA)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session A event")
	}
}

// ---------------------------------------------------------------------------
// Last-Event-ID replay
// ---------------------------------------------------------------------------

func TestSSE_ReplayWithLastEventID(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}

	frames := readSSEMessages(resp)

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "first", nil))

	var firstFrame sseMessage
	select {
	case firstFrame = <-frames:
		if firstFrame.ID == "" {
			resp.Body.Close()
			t.Fatal("expected SSE id for first event")
		}
	case <-time.After(2 * time.Second):
		resp.Body.Close()
		t.Fatal("timed out waiting for first SSE event")
	}

	resp.Body.Close()

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "second", nil))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/sessions/"+sessionID+"/events", nil)
	req.Header.Set("Last-Event-ID", firstFrame.ID)
	respReplay, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	defer respReplay.Body.Close()

	replayFrames := readSSEMessages(respReplay)
	select {
	case frame := <-replayFrames:
		var ev apiTypes.Event
		_ = json.Unmarshal([]byte(frame.Data), &ev)
		data, _ := ev.Data.(map[string]any)
		content, _ := data["content"].(string)
		if content != "second" {
			t.Fatalf("expected replayed content 'second', got %q", content)
		}
		if frame.ID == firstFrame.ID {
			t.Fatalf("expected replayed event id to differ from first event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed SSE event")
	}
}

func TestSSE_GlobalSessionEvents_Headers(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/sessions/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestSSE_GlobalSessionEvents_MultipleSessions(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionA := createSessionViaHTTP(t, srv.URL)
	sessionB := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	frames := readSSEMessages(resp)

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionA, "ignore-non-status", nil))
	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionA, domain.SessionStateIdle, domain.SessionStateRunning, "start-a", nil))
	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionB, domain.SessionStateIdle, domain.SessionStateRunning, "start-b", nil))

	collected := make([]apiTypes.SessionStateEvent, 0, 2)
	timeout := time.After(2 * time.Second)
	for len(collected) < 2 {
		select {
		case frame := <-frames:
			if frame.Event != string(apiTypes.EventTypeSessionState) {
				continue
			}
			var ev apiTypes.SessionStateEvent
			if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
				t.Fatalf("unmarshal session state event: %v", err)
			}
			collected = append(collected, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for global session state events; got %d", len(collected))
		}
	}

	if collected[0].SessionID != sessionA {
		t.Fatalf("event 0 session_id = %q, want %q", collected[0].SessionID, sessionA)
	}
	if collected[1].SessionID != sessionB {
		t.Fatalf("event 1 session_id = %q, want %q", collected[1].SessionID, sessionB)
	}
	if collected[0].Reason != "start-a" || collected[1].Reason != "start-b" {
		t.Fatalf("unexpected reasons: %q, %q", collected[0].Reason, collected[1].Reason)
	}
}

func TestSSE_GlobalSessionEvents_ReplayWithLastEventID(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionA := createSessionViaHTTP(t, srv.URL)
	sessionB := createSessionViaHTTP(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/sessions/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}

	frames := readSSEMessages(resp)

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionA, domain.SessionStateIdle, domain.SessionStateRunning, "first", nil))

	var firstFrame sseMessage
	select {
	case firstFrame = <-frames:
		if firstFrame.ID == "" {
			resp.Body.Close()
			t.Fatal("expected SSE id for first global event")
		}
	case <-time.After(2 * time.Second):
		resp.Body.Close()
		t.Fatal("timed out waiting for first global SSE event")
	}

	resp.Body.Close()

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionB, domain.SessionStateIdle, domain.SessionStateRunning, "second", nil))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/sessions/events?last_event_id="+firstFrame.ID, nil)
	respReplay, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	defer respReplay.Body.Close()

	replayFrames := readSSEMessages(respReplay)
	select {
	case frame := <-replayFrames:
		if frame.Event != string(apiTypes.EventTypeSessionState) {
			t.Fatalf("expected session_state frame, got %q", frame.Event)
		}
		var ev apiTypes.SessionStateEvent
		if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
			t.Fatalf("unmarshal replay event: %v", err)
		}
		if ev.SessionID != sessionB {
			t.Fatalf("expected replay session %q, got %q", sessionB, ev.SessionID)
		}
		if ev.Reason != "second" {
			t.Fatalf("expected replay reason second, got %q", ev.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed global SSE event")
	}
}

// ---------------------------------------------------------------------------
// convertEventData unit tests
// ---------------------------------------------------------------------------

func TestConvertEventData_AllTypes(t *testing.T) {
	tests := []struct {
		name  string
		event domain.Event
		want  apiTypes.EventType
	}{
		{
			name:  "status_change",
			event: domain.NewStatusChangeEvent("s1", domain.SessionStateIdle, domain.SessionStateRunning, "go", nil),
			want:  apiTypes.EventTypeStatusChange,
		},
		{
			name:  "output",
			event: domain.NewOutputEvent("s1", "hello", nil),
			want:  apiTypes.EventTypeOutput,
		},
		{
			name:  "metric",
			event: domain.NewMetricEvent("s1", 1, 2, 3, nil),
			want:  apiTypes.EventTypeMetric,
		},
		{
			name:  "error",
			event: domain.NewErrorEvent("s1", "bad", "CODE", nil),
			want:  apiTypes.EventTypeError,
		},
		{
			name:  "metadata",
			event: domain.NewMetadataEvent("s1", "k", "v", nil),
			want:  apiTypes.EventTypeMetadata,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiEvt := domainEventToAPIEvent(tt.event)
			if apiEvt.Type != tt.want {
				t.Errorf("Type = %q, want %q", apiEvt.Type, tt.want)
			}
			if apiEvt.SessionID != "s1" {
				t.Errorf("SessionID = %q, want s1", apiEvt.SessionID)
			}
			if apiEvt.Data == nil {
				t.Error("Data should not be nil")
			}
		})
	}
}

func TestConvertEventData_UnknownType(t *testing.T) {
	ev := domain.Event{
		Type:      domain.EventType(999),
		SessionID: "s1",
		Data:      "raw string payload",
	}
	apiEvt := domainEventToAPIEvent(ev)
	if apiEvt.Type != "unknown" {
		t.Errorf("Type = %q, want 'unknown'", apiEvt.Type)
	}
	if apiEvt.Data != "raw string payload" {
		t.Errorf("Data = %v, want 'raw string payload'", apiEvt.Data)
	}
}

// ---------------------------------------------------------------------------
// end-to-end lifecycle: create -> pause -> resume -> stop with SSE
// ---------------------------------------------------------------------------

func TestIntegration_SessionLifecycle(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	// 1. Create
	sessionID := createSessionViaHTTP(t, srv.URL)

	// 2. Open SSE
	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/events")
	if err != nil {
		t.Fatalf("SSE: %v", err)
	}
	defer resp.Body.Close()
	events := readSSEEvents(resp)

	// Per new design, send a message to transition the session to running
	msgReq := apiTypes.SendMessageRequest{
		Content: "test message",
	}
	msgBody, _ := json.Marshal(msgReq)
	msgHTTPReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	msgHTTPReq.Header.Set("Content-Type", "application/json")
	msgResp, err := http.DefaultClient.Do(msgHTTPReq)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	msgResp.Body.Close()

	// Wait for the session to be running after message sent
	waitForStateHTTP(t, srv.URL, sessionID, "running")

	// Stop
	stopReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/sessions/"+sessionID, nil)
	stopResp, err := http.DefaultClient.Do(stopReq)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	stopResp.Body.Close()
	if stopResp.StatusCode != http.StatusNoContent {
		t.Fatalf("stop: expected 204, got %d", stopResp.StatusCode)
	}

	// 6. Verify final state via GET
	getResp, err := http.Get(srv.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var sess apiTypes.SessionStatusResponse
	_ = json.NewDecoder(getResp.Body).Decode(&sess)
	if sess.State != apiTypes.SessionStateIdle {
		t.Errorf("final state = %q, want idle", sess.State)
	}

	// 7. Drain any buffered SSE events — we should have received state changes
	// from the executor's internal broadcasts (starting, running, paused, etc.)
	var received []apiTypes.Event
	drainTimeout := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break drain
			}
			received = append(received, ev)
		case <-drainTimeout:
			break drain
		}
	}

	// At minimum we expect some status_change events from the executor
	hasStatusChange := false
	for _, ev := range received {
		if ev.Type == apiTypes.EventTypeStatusChange {
			hasStatusChange = true
			break
		}
	}
	if !hasStatusChange {
		t.Error("expected at least one status_change event on SSE stream")
	}
}

// waitForStateHTTP polls GET /api/sessions/{id} until the state matches.
func waitForStateHTTP(t *testing.T, baseURL, sessionID, wantState string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/sessions/" + sessionID)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var sess apiTypes.SessionStatusResponse
		_ = json.NewDecoder(resp.Body).Decode(&sess)
		resp.Body.Close()
		if string(sess.State) == wantState {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s state = %q", sessionID, wantState)
}
