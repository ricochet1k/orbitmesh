package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/domain"
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

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionA, domain.SessionStateIdle, domain.SessionStateRunning, "started"))

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

	env.broadcaster.Broadcast(domain.NewStatusChangeEvent(sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "started"))

	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var msg realtimeTypes.ServerEnvelope
	err = conn.ReadJSON(&msg)
	if err == nil {
		t.Fatal("expected no message after unsubscribe")
	}
}
