package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claudews"
)

type wsWriter struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	recorder *transcriptRecorder
}

func (w *wsWriter) sendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.sendRaw(b)
}

func (w *wsWriter) sendRaw(raw []byte) error {
	trimmed := trimLine(raw)
	if !json.Valid(trimmed) {
		return fmt.Errorf("outbound message must be valid JSON")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.WriteMessage(websocket.TextMessage, trimmed); err != nil {
		return err
	}
	w.recorder.Record(dirProviderToOrbitMesh, trimmed)
	return nil
}

func runClaudeWSMode(ctx context.Context, sdkURL string, controller *controller, recorder *transcriptRecorder) error {
	if sdkURL == "" {
		return fmt.Errorf("--sdk-url is required for claudews mode")
	}

	conn, _, err := websocket.DefaultDialer.Dial(sdkURL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close()

	writer := &wsWriter{conn: conn, recorder: recorder}
	controller.SetEmitter(func(line []byte) error {
		return writer.sendRaw(line)
	})

	sessionID := "mock-claudews-session"
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "."
	}
	init := claudews.SystemInitMessage{
		Type:              "system",
		Subtype:           "init",
		SessionID:         sessionID,
		CWD:               filepath.Clean(cwd),
		Model:             "acp-echo-claudews",
		ClaudeCodeVersion: "0.0.1-mock",
		PermissionMode:    "default",
		APIKeySource:      "mock",
		Tools:             []string{"Bash"},
		MCPServers:        []claudews.MCPServerInfo{},
	}
	if err := writer.sendJSON(init); err != nil {
		return err
	}

	var seq atomic.Int64
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil
		}
		controller.RecordInbound(data)

		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		typ := asString(envelope["type"])
		switch typ {
		case "control_request":
			if err := handleClaudeWSControlRequest(writer, envelope); err != nil {
				return err
			}
		case "user":
			if suppressAutoUserResponse() {
				continue
			}
			input := extractClaudeWSUserText(envelope)
			if input == "" {
				input = "(no input)"
			}
			payload, _ := json.Marshal(map[string]string{"echo": input})
			if err := emitClaudeWSAssistant(writer, sessionID, seq.Add(1), string(payload)); err != nil {
				return err
			}
		}
	}
}

func suppressAutoUserResponse() bool {
	v := os.Getenv("ACP_ECHO_SUPPRESS_AUTO_USER_RESPONSE")
	return v == "1" || v == "true" || v == "TRUE"
}

func handleClaudeWSControlRequest(writer *wsWriter, envelope map[string]any) error {
	requestID := asString(envelope["request_id"])
	request, _ := envelope["request"].(map[string]any)
	subtype := asString(request["subtype"])

	responsePayload := map[string]any{}
	if subtype == "can_use_tool" {
		responsePayload["behavior"] = "allow"
		if input, ok := request["input"].(map[string]any); ok {
			responsePayload["updatedInput"] = input
		}
	}

	resp := claudews.ControlResponse{
		Type: "control_response",
		Response: claudews.ControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  responsePayload,
		},
	}
	return writer.sendJSON(resp)
}

func extractClaudeWSUserText(envelope map[string]any) string {
	message, ok := envelope["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := message["content"].(string)
	return content
}

func emitClaudeWSAssistant(writer *wsWriter, sessionID string, seq int64, text string) error {
	msg := map[string]any{
		"type":       "assistant",
		"uuid":       uuid.NewString(),
		"session_id": sessionID,
		"message": map[string]any{
			"id":   fmt.Sprintf("assistant-%d", seq),
			"role": "assistant",
			"content": []map[string]any{{
				"type": "text",
				"text": text,
			}},
			"model": "acp-echo-claudews",
		},
	}
	if err := writer.sendJSON(msg); err != nil {
		return err
	}

	stopReason := "end_turn"
	res := claudews.ResultMessage{
		Type:          "result",
		Subtype:       "success",
		IsError:       false,
		Result:        "ok",
		DurationMS:    1,
		DurationAPIMS: 1,
		NumTurns:      int(seq),
		TotalCostUSD:  0,
		StopReason:    &stopReason,
		Usage:         claudews.ResultUsage{InputTokens: 1, OutputTokens: 1},
		UUID:          uuid.NewString(),
		SessionID:     sessionID,
	}
	return writer.sendJSON(res)
}
