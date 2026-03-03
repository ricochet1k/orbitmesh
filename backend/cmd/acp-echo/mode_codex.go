package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
)

type codexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type codexRPCMessage struct {
	Method string          `json:"method,omitempty"`
	ID     *int64          `json:"id,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *codexRPCError  `json:"error,omitempty"`
}

func runCodexAppServerMode(ctx context.Context, in io.Reader, out io.Writer, controller *controller, recorder *transcriptRecorder) error {
	writer := newLineWriter(out, recorder)
	controller.SetEmitter(func(line []byte) error {
		return writer.WriteLine(line)
	})

	state := &codexMockState{threadID: "thread-echo"}
	handler := func(line []byte) error {
		controller.RecordInbound(line)
		var msg codexRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return writeCodexError(writer, nil, -32700, "invalid json")
		}
		if msg.Method == "" {
			return nil
		}
		if msg.ID == nil {
			return nil
		}
		return state.handleRequest(writer, msg)
	}

	return readLines(ctx.Done(), in, handler)
}

type codexMockState struct {
	threadID string
	turnSeq  atomic.Int64
}

func (s *codexMockState) handleRequest(writer *lineWriter, req codexRPCMessage) error {
	switch req.Method {
	case "initialize":
		return writeCodexResult(writer, req.ID, map[string]any{
			"capabilities": map[string]any{},
			"serverInfo": map[string]any{
				"name":    "acp-echo",
				"version": "0.1.0",
			},
		})
	case "thread/start":
		return writeCodexResult(writer, req.ID, map[string]any{"thread": map[string]any{"id": s.threadID}})
	case "turn/start":
		turnID := fmt.Sprintf("turn-%d", s.turnSeq.Add(1))
		input := extractCodexTurnInput(req.Params)

		if err := writeCodexResult(writer, req.ID, map[string]any{"turn": map[string]any{"id": turnID}}); err != nil {
			return err
		}
		if err := writeCodexNotification(writer, "turn/started", map[string]any{"turn": map[string]any{"id": turnID}}); err != nil {
			return err
		}
		if err := writeCodexNotification(writer, "item/agentMessage/delta", map[string]any{
			"threadId": s.threadID,
			"turnId":   turnID,
			"itemId":   fmt.Sprintf("msg-%s", turnID),
			"delta":    string(mustJSON(map[string]string{"echo": input})),
		}); err != nil {
			return err
		}
		return writeCodexNotification(writer, "turn/completed", map[string]any{
			"turn": map[string]any{"id": turnID, "status": "completed"},
		})
	default:
		return writeCodexResult(writer, req.ID, map[string]any{})
	}
}

func extractCodexTurnInput(params json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return "(no input)"
	}
	items, _ := payload["input"].([]any)
	if len(items) == 0 {
		return "(no input)"
	}
	item, _ := items[0].(map[string]any)
	text := asString(item["text"])
	if text == "" {
		return "(no input)"
	}
	return text
}

func writeCodexResult(writer *lineWriter, id *int64, result any) error {
	line, err := marshalJSON(codexRPCMessage{ID: id, Result: result})
	if err != nil {
		return err
	}
	return writer.WriteLine(line)
}

func writeCodexError(writer *lineWriter, id *int64, code int, message string) error {
	line, err := marshalJSON(codexRPCMessage{ID: id, Error: &codexRPCError{Code: code, Message: message}})
	if err != nil {
		return err
	}
	return writer.WriteLine(line)
}

func writeCodexNotification(writer *lineWriter, method string, params any) error {
	line, err := marshalJSON(codexRPCMessage{Method: method, Params: mustJSON(params)})
	if err != nil {
		return err
	}
	return writer.WriteLine(line)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
