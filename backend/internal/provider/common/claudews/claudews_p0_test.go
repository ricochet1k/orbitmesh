package claudews

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

func TestWSConnSend_AppendsNDJSONNewline(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	if err := wc.Send(map[string]any{"type": "keep_alive"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	msg := <-outbound
	if !bytes.HasSuffix(msg, []byte("\n")) {
		t.Fatalf("expected newline-terminated frame, got %q", string(msg))
	}

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSuffix(msg, []byte("\n")), &decoded); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	if decoded["type"] != "keep_alive" {
		t.Fatalf("unexpected outbound payload: %+v", decoded)
	}
}

func TestDispatchFrame_SplitsMultipleNDJSONMessages(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	frame := []byte("{\"type\":\"system\",\"subtype\":\"status\",\"status\":\"compacting\"}\n\n {\"type\":\"tool_use_summary\",\"summary\":\"ran tool\"}\n")

	p.dispatchFrame(frame)

	seenSystem := false
	seenThought := false
	for i := 0; i < 2; i++ {
		select {
		case ev := <-p.events.Events():
			if ev.Type == domain.EventTypeSystemMessage {
				seenSystem = true
			}
			if ev.Type == domain.EventTypeThought {
				seenThought = true
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for parsed events")
		}
	}

	if !seenSystem || !seenThought {
		t.Fatalf("expected both events from multiline frame, got system=%v thought=%v", seenSystem, seenThought)
	}
}

func TestProcessInput_FirstSendIncludesEmptySessionID(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.inputBuffer = buffer.NewInputBuffer(1)
	p.config = session.Config{Custom: map[string]any{"claudews_no_response_timeout_ms": 10}}

	go p.processInput()

	if err := p.inputBuffer.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("queue input: %v", err)
	}

	msg := <-outbound
	trimmed := bytes.TrimSpace(msg)

	var payload map[string]any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	if _, ok := payload["session_id"]; !ok {
		t.Fatalf("expected session_id field in first user payload: %s", string(trimmed))
	}
	if payload["session_id"] != "" {
		t.Fatalf("expected empty session_id before init, got %#v", payload["session_id"])
	}

	p.inboundSignal <- struct{}{}
	p.cancel()
}

func TestPermissionRequest_TimesOutDeterministically(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	p := NewClaudeWSProvider("s1", blockingPermissionHandler{})
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.config = session.Config{Custom: map[string]any{"claudews_permission_timeout_ms": 10}}

	p.handleControlRequest(RawMessage{Raw: []byte(`{"type":"control_request","request_id":"req-timeout","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"sleep 100"},"tool_use_id":"tool-1"}}`)})

	msg := <-outbound
	if !bytes.HasSuffix(msg, []byte("\n")) {
		t.Fatalf("expected newline-terminated response, got %q", string(msg))
	}
	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(msg), &response); err != nil {
		t.Fatalf("unmarshal control response: %v", err)
	}
	reason := extractDenyMessage(response)
	if !strings.Contains(reason, "timed out") {
		t.Fatalf("expected timeout deny reason, got %q", reason)
	}
}

func TestPermissionRequest_CanBeCancelledByControlMessage(t *testing.T) {
	wc, outbound, cleanup := newTestWSConn(t)
	defer cleanup()

	blocked := make(chan struct{})
	p := NewClaudeWSProvider("s1", waitingPermissionHandler{release: blocked})
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wsConn = wc
	p.config = session.Config{Custom: map[string]any{"claudews_permission_timeout_ms": 5000}}

	p.handleControlRequest(RawMessage{Raw: []byte(`{"type":"control_request","request_id":"req-cancel","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"long"},"tool_use_id":"tool-2"}}`)})
	p.dispatchMessage([]byte(`{"type":"control_cancel_request","request_id":"req-cancel"}`))

	msg := <-outbound
	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(msg), &response); err != nil {
		t.Fatalf("unmarshal control response: %v", err)
	}
	reason := extractDenyMessage(response)
	if !strings.Contains(reason, "cancelled") {
		t.Fatalf("expected cancelled deny reason, got %q", reason)
	}

	close(blocked)
}

type blockingPermissionHandler struct{}

func (blockingPermissionHandler) RequestPermission(_ context.Context, _ tools.PermissionRequest) (tools.PermissionDecision, error) {
	select {}
}

type waitingPermissionHandler struct {
	release <-chan struct{}
}

func (h waitingPermissionHandler) RequestPermission(_ context.Context, _ tools.PermissionRequest) (tools.PermissionDecision, error) {
	<-h.release
	return tools.PermissionDecision{Granted: true}, nil
}

func extractDenyMessage(response map[string]any) string {
	resp, _ := response["response"].(map[string]any)
	body, _ := resp["response"].(map[string]any)
	message, _ := body["message"].(string)
	return message
}

func newTestWSConn(t *testing.T) (*wsConn, <-chan []byte, func()) {
	t.Helper()
	outbound := make(chan []byte, 8)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			outbound <- data
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial websocket: %v", err)
	}

	cleanup := func() {
		_ = client.Close()
		server.Close()
	}

	return newWSConn(client), outbound, cleanup
}
