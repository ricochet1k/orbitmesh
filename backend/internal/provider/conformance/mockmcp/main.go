package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	for {
		payload, err := readFrame(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		var req requestEnvelope
		if err := json.Unmarshal(payload, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			resp["result"] = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{
					"name":    "providercheck-mock-mcp",
					"version": "1.0.0",
				},
			}
		case "tools/list":
			resp["result"] = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "orbitmesh_providercheck_echo",
						"description": "Deterministic echo fixture tool",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"text": map[string]any{"type": "string"},
							},
							"required": []string{"text"},
						},
					},
				},
			}
		case "tools/call":
			resp["result"] = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			}
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}

		if err := writeFrame(resp); err != nil {
			return
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "content-length:") {
			raw := strings.TrimSpace(line[len("content-length:"):])
			value, convErr := strconv.Atoi(raw)
			if convErr == nil {
				contentLength = value
			}
		}
	}
	if contentLength < 0 {
		return nil, io.EOF
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFrame(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	_, _ = fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", len(payload))
	out.Write(payload)
	_, err = os.Stdout.Write(out.Bytes())
	return err
}
