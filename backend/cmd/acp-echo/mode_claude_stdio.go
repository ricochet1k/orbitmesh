package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
)

func runClaudeStdioMode(ctx context.Context, in io.Reader, out io.Writer, controller *controller, recorder *transcriptRecorder) error {
	writer := newLineWriter(out, recorder)
	controller.SetEmitter(func(line []byte) error {
		return writer.WriteLine(line)
	})

	var seq atomic.Int64
	handler := func(line []byte) error {
		controller.RecordInbound(line)
		text := extractClaudeUserText(line)
		if text == "" {
			return nil
		}
		payload := map[string]string{"echo": text}
		resp, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		uuid := fmt.Sprintf("mock-msg-%d", seq.Add(1))
		return emitClaudeEcho(writer, uuid, string(resp))
	}

	return readLines(ctx.Done(), in, handler)
}

func extractClaudeUserText(line []byte) string {
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}
	if asString(msg["type"]) != "user" {
		return ""
	}
	message, ok := msg["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := message["content"].(string)
	if content == "" {
		return "(no input)"
	}
	return content
}

func emitClaudeEcho(writer *lineWriter, messageID, content string) error {
	frames := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":    messageID,
				"type":  "message",
				"role":  "assistant",
				"model": "acp-echo-claude",
				"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
			},
		},
		{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		},
		{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": content},
		},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}},
		{"type": "message_stop"},
	}

	for _, frame := range frames {
		line, err := marshalJSON(frame)
		if err != nil {
			return err
		}
		if err := writer.WriteLine(line); err != nil {
			return err
		}
	}
	return nil
}
