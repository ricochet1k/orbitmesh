package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

func TestRealtimeWebSocket_SubscribeGetsSnapshotThenEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionA := createSessionViaHTTP(t, srv.URL)
	sessionB := createSessionViaHTTP(t, srv.URL)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/realtime"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial realtime websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{
		Type:   realtimeTypes.ClientMessageTypeSubscribe,
		Topics: []string{"sessions.state"},
	}); err != nil {
		t.Fatalf("write subscribe message: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var snapshotMsg realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&snapshotMsg); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshotMsg.Type != realtimeTypes.ServerMessageTypeSnapshot {
		t.Fatalf("snapshot type = %q, want %q", snapshotMsg.Type, realtimeTypes.ServerMessageTypeSnapshot)
	}
	if snapshotMsg.Topic != "sessions.state" {
		t.Fatalf("snapshot topic = %q", snapshotMsg.Topic)
	}

	snapshotBytes, err := json.Marshal(snapshotMsg.Payload)
	if err != nil {
		t.Fatalf("marshal snapshot payload: %v", err)
	}
	var snapshot realtimeTypes.SessionsStateSnapshot
	if err := json.Unmarshal(snapshotBytes, &snapshot); err != nil {
		t.Fatalf("decode snapshot payload: %v", err)
	}
	if len(snapshot.Sessions) != 2 {
		t.Fatalf("snapshot sessions len = %d, want 2", len(snapshot.Sessions))
	}
	idSet := map[string]bool{}
	for _, session := range snapshot.Sessions {
		idSet[session.ID] = true
	}
	if !idSet[sessionA] || !idSet[sessionB] {
		t.Fatalf("snapshot missing created sessions")
	}

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionA, domain.SessionStateIdle, domain.SessionStateRunning, "started", nil))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var eventMsg realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&eventMsg); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if eventMsg.Type != realtimeTypes.ServerMessageTypeEvent {
		t.Fatalf("event type = %q, want %q", eventMsg.Type, realtimeTypes.ServerMessageTypeEvent)
	}
	if eventMsg.Topic != "sessions.state" {
		t.Fatalf("event topic = %q", eventMsg.Topic)
	}
	eventBytes, err := json.Marshal(eventMsg.Payload)
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	var stateEvent realtimeTypes.SessionStateEvent
	if err := json.Unmarshal(eventBytes, &stateEvent); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if stateEvent.SessionID != sessionA {
		t.Fatalf("event session_id = %q, want %q", stateEvent.SessionID, sessionA)
	}
}

func TestRealtimeWebSocket_UnsubscribeStopsTopicEvents(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/realtime"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial realtime websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{Type: realtimeTypes.ClientMessageTypeSubscribe, Topics: []string{"sessions.state"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var snapshot realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{Type: realtimeTypes.ClientMessageTypeUnsubscribe, Topics: []string{"sessions.state"}}); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{Type: realtimeTypes.ClientMessageTypePing}); err != nil {
		t.Fatalf("ping after unsubscribe: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var pong realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Type != realtimeTypes.ServerMessageTypePong {
		t.Fatalf("expected pong, got %q", pong.Type)
	}

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "started", nil))

	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var msg realtimeTypes.ServerEnvelope
	err = conn.ReadJSON(&msg)
	if err == nil {
		t.Fatal("expected no message after unsubscribe")
	}
}

func TestRealtimeWebSocket_SessionsActivitySnapshotAndEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/realtime"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial realtime websocket: %v", err)
	}
	defer conn.Close()

	topic := "sessions.activity:" + sessionID
	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{Type: realtimeTypes.ClientMessageTypeSubscribe, Topics: []string{topic}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var snapshotMsg realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&snapshotMsg); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshotMsg.Type != realtimeTypes.ServerMessageTypeSnapshot {
		t.Fatalf("snapshot type = %q", snapshotMsg.Type)
	}
	if snapshotMsg.Topic != topic {
		t.Fatalf("snapshot topic = %q", snapshotMsg.Topic)
	}

	snapshotBytes, err := json.Marshal(snapshotMsg.Payload)
	if err != nil {
		t.Fatalf("marshal snapshot payload: %v", err)
	}
	var snapshot realtimeTypes.SessionActivitySnapshot
	if err := json.Unmarshal(snapshotBytes, &snapshot); err != nil {
		t.Fatalf("decode snapshot payload: %v", err)
	}
	if snapshot.SessionID != sessionID {
		t.Fatalf("snapshot session_id = %q, want %q", snapshot.SessionID, sessionID)
	}

	env.broadcaster.Broadcast(domain.NewOutputEvent(sessionID, "hello from activity stream", nil))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var eventMsg realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&eventMsg); err != nil {
		t.Fatalf("read activity event: %v", err)
	}
	if eventMsg.Type != realtimeTypes.ServerMessageTypeEvent {
		t.Fatalf("event type = %q", eventMsg.Type)
	}
	if eventMsg.Topic != topic {
		t.Fatalf("event topic = %q", eventMsg.Topic)
	}
	eventBytes, err := json.Marshal(eventMsg.Payload)
	if err != nil {
		t.Fatalf("marshal activity event payload: %v", err)
	}
	var activityEvent realtimeTypes.SessionActivityEvent
	if err := json.Unmarshal(eventBytes, &activityEvent); err != nil {
		t.Fatalf("decode activity event payload: %v", err)
	}
	if activityEvent.SessionID != sessionID {
		t.Fatalf("event session_id = %q, want %q", activityEvent.SessionID, sessionID)
	}
	if activityEvent.Type != "output" {
		t.Fatalf("event type = %q, want output", activityEvent.Type)
	}
}

func TestRealtimeWebSocket_TerminalTopicsSnapshotAndEvent(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.router())
	defer srv.Close()

	sessionID := createSessionViaHTTP(t, srv.URL)
	term := domain.NewTerminal(sessionID, sessionID, domain.TerminalKindPTY)
	term.LastSnapshot = &terminal.Snapshot{Rows: 24, Cols: 80, Lines: []string{"hello"}}
	term.LastSeq = 1
	if err := env.store.SaveTerminal(term); err != nil {
		t.Fatalf("save terminal: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/realtime"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial realtime websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(realtimeTypes.ClientEnvelope{Type: realtimeTypes.ClientMessageTypeSubscribe, Topics: []string{"terminals.state", "terminals.output:" + sessionID}}); err != nil {
		t.Fatalf("subscribe terminal topics: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg1 realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&msg1); err != nil {
		t.Fatalf("read terminal snapshot 1: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg2 realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&msg2); err != nil {
		t.Fatalf("read terminal snapshot 2: %v", err)
	}

	var stateMsg, outputMsg realtimeTypes.ServerEnvelope
	if msg1.Topic == "terminals.state" {
		stateMsg, outputMsg = msg1, msg2
	} else {
		stateMsg, outputMsg = msg2, msg1
	}
	if stateMsg.Type != realtimeTypes.ServerMessageTypeSnapshot || stateMsg.Topic != "terminals.state" {
		t.Fatalf("unexpected terminals.state snapshot message: %#v", stateMsg)
	}
	if outputMsg.Type != realtimeTypes.ServerMessageTypeSnapshot || outputMsg.Topic != "terminals.output:"+sessionID {
		t.Fatalf("unexpected terminals.output snapshot message: %#v", outputMsg)
	}

	event := service.TerminalEvent{
		Seq: 2,
		Update: terminal.Update{
			Kind: terminal.UpdateDiff,
			Diff: &terminal.Diff{
				Region: terminal.Region{X: 0, Y: 0, X2: 10, Y2: 1},
				Lines:  []string{"world"},
				Reason: "test",
			},
		},
	}
	env.handler.publishRealtimeTerminalEvent(sessionID, event)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var eventMsg realtimeTypes.ServerEnvelope
	if err := conn.ReadJSON(&eventMsg); err != nil {
		t.Fatalf("read terminal event: %v", err)
	}
	if eventMsg.Topic == "terminals.state" {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&eventMsg); err != nil {
			t.Fatalf("read terminal output event: %v", err)
		}
	}
	if eventMsg.Topic != "terminals.output:"+sessionID {
		t.Fatalf("terminal output topic = %q", eventMsg.Topic)
	}

	payloadBytes, err := json.Marshal(eventMsg.Payload)
	if err != nil {
		t.Fatalf("marshal terminal output event payload: %v", err)
	}
	var outputEvent realtimeTypes.TerminalOutputEvent
	if err := json.Unmarshal(payloadBytes, &outputEvent); err != nil {
		t.Fatalf("decode terminal output event payload: %v", err)
	}
	if outputEvent.Seq != 2 {
		t.Fatalf("output seq = %d, want 2", outputEvent.Seq)
	}
	if outputEvent.Type != "terminal.diff" {
		t.Fatalf("output type = %q, want terminal.diff", outputEvent.Type)
	}
}
