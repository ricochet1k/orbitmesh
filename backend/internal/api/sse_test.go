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

// readSSEEvents launches a goroutine that parses SSE lines from resp.Body
// and sends decoded events on the returned channel.  The channel is closed
// when the body is closed or EOF is reached.
func readSSEEvents(resp *http.Response) <-chan apiTypes.Event {
	ch := make(chan apiTypes.Event, 10)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		var dataLine string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "data: "):
				dataLine = strings.TrimPrefix(line, "data: ")
			case line == "" && dataLine != "":
				var ev apiTypes.Event
				_ = json.Unmarshal([]byte(dataLine), &ev)
				ch <- ev
				dataLine = ""
			}
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

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "hello world"))

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

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "first"))
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "second"))
	env.broadcaster.Broadcast(domain.NewErrorEvent(sessionID, "oops", "ERR_TEST"))

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

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionID, "created", "running", "started"))

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

	env.broadcaster.Broadcast(domain.NewMetricEvent(sessionID, 100, 50, 3))

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

	env.broadcaster.Broadcast(domain.NewMetadataEvent(sessionID, "current_task", "do-thing"))

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
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionB, "for B only"))

	// Broadcast to session A
	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionA, "for A"))

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
// convertEventData unit tests
// ---------------------------------------------------------------------------

func TestConvertEventData_AllTypes(t *testing.T) {
	tests := []struct {
		name   string
		event  domain.Event
		want   apiTypes.EventType
	}{
		{
			name:  "status_change",
			event: domain.NewStatusChangeEvent("s1", "created", "running", "go"),
			want:  apiTypes.EventTypeStatusChange,
		},
		{
			name:  "output",
			event: domain.NewOutputEvent("s1", "hello"),
			want:  apiTypes.EventTypeOutput,
		},
		{
			name:  "metric",
			event: domain.NewMetricEvent("s1", 1, 2, 3),
			want:  apiTypes.EventTypeMetric,
		},
		{
			name:  "error",
			event: domain.NewErrorEvent("s1", "bad", "CODE"),
			want:  apiTypes.EventTypeError,
		},
		{
			name:  "metadata",
			event: domain.NewMetadataEvent("s1", "k", "v"),
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

	// Wait for the session to be running (the executor goroutine transitions it)
	waitForStateHTTP(t, srv.URL, sessionID, "running")

	// 3. Pause
	pauseResp, err := http.Post(srv.URL+"/api/sessions/"+sessionID+"/pause", "", nil)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	pauseResp.Body.Close()
	if pauseResp.StatusCode != http.StatusNoContent {
		t.Fatalf("pause: expected 204, got %d", pauseResp.StatusCode)
	}

	// 4. Resume
	resumeResp, err := http.Post(srv.URL+"/api/sessions/"+sessionID+"/resume", "", nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	resumeResp.Body.Close()
	if resumeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("resume: expected 204, got %d", resumeResp.StatusCode)
	}

	// 5. Stop
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
	if sess.State != apiTypes.SessionStateStopped {
		t.Errorf("final state = %q, want stopped", sess.State)
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
