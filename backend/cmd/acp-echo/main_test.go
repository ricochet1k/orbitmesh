package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexAppServerMode_MinimalFlow(t *testing.T) {
	recorder, err := newTranscriptRecorder("")
	if err != nil {
		t.Fatalf("newTranscriptRecorder: %v", err)
	}
	controller := newController(recorder)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCodexAppServerMode(ctx, inR, outW, controller, recorder)
	}()

	lines := make(chan map[string]any, 16)
	go scanJSONLines(t, outR, lines)

	writeJSONLine(t, inW, map[string]any{"id": 1, "method": "initialize", "params": map[string]any{}})
	resp := waitForJSONLine(t, lines, func(v map[string]any) bool { return int(vNum(v["id"])) == 1 })
	if _, ok := resp["result"]; !ok {
		t.Fatalf("initialize response missing result: %#v", resp)
	}

	writeJSONLine(t, inW, map[string]any{"id": 2, "method": "thread/start", "params": map[string]any{}})
	resp = waitForJSONLine(t, lines, func(v map[string]any) bool { return int(vNum(v["id"])) == 2 })
	thread := asMap(asMap(resp["result"])["thread"])
	if asString(thread["id"]) == "" {
		t.Fatalf("thread/start response missing thread id: %#v", resp)
	}

	writeJSONLine(t, inW, map[string]any{
		"id":     3,
		"method": "turn/start",
		"params": map[string]any{"threadId": "thread-echo", "input": []map[string]any{{"type": "text", "text": "hello"}}},
	})
	_ = waitForJSONLine(t, lines, func(v map[string]any) bool { return int(vNum(v["id"])) == 3 })
	_ = waitForJSONLine(t, lines, func(v map[string]any) bool { return asString(v["method"]) == "turn/started" })
	delta := waitForJSONLine(t, lines, func(v map[string]any) bool { return asString(v["method"]) == "item/agentMessage/delta" })
	params := asMap(delta["params"])
	if !strings.Contains(asString(params["delta"]), "hello") {
		t.Fatalf("delta did not include echoed input: %#v", delta)
	}
	_ = waitForJSONLine(t, lines, func(v map[string]any) bool { return asString(v["method"]) == "turn/completed" })

	cancel()
	_ = inW.Close()
	_ = outW.Close()
	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("runCodexAppServerMode failed: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for codex mode shutdown")
	}
}

func TestControlInboundAndTranscript(t *testing.T) {
	transcriptPath := filepath.Join(t.TempDir(), "mock.ndjson")
	recorder, err := newTranscriptRecorder(transcriptPath)
	if err != nil {
		t.Fatalf("newTranscriptRecorder: %v", err)
	}
	defer recorder.Close()

	controller := newController(recorder)
	if err := controller.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("controller.Start: %v", err)
	}
	defer controller.Close()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCodexAppServerMode(ctx, inR, outW, controller, recorder)
	}()

	lines := make(chan map[string]any, 16)
	go scanJSONLines(t, outR, lines)

	writeJSONLine(t, inW, map[string]any{"id": 9, "method": "initialize", "params": map[string]any{}})
	_ = waitForJSONLine(t, lines, func(v map[string]any) bool { return int(vNum(v["id"])) == 9 })

	base := "http://" + controller.Addr()
	resp, err := http.Get(base + "/inbound")
	if err != nil {
		t.Fatalf("GET /inbound: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("initialize")) {
		t.Fatalf("/inbound did not include initialize request: %s", string(body))
	}

	emitBody := `{"message":{"method":"item/agentMessage/delta","params":{"delta":"forced"}}}`
	emitResp, err := http.Post(base+"/emit", "application/json", strings.NewReader(emitBody))
	if err != nil {
		t.Fatalf("POST /emit: %v", err)
	}
	defer emitResp.Body.Close()
	_ = waitForJSONLine(t, lines, func(v map[string]any) bool {
		return asString(v["method"]) == "item/agentMessage/delta" && strings.Contains(asString(asMap(v["params"])["delta"]), "forced")
	})

	cancel()
	_ = inW.Close()
	_ = outW.Close()
	_ = <-errCh

	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if !bytes.Contains(data, []byte(dirOrbitMeshToProvider)) {
		t.Fatalf("transcript missing orbitmesh_to_provider direction: %s", string(data))
	}
	if !bytes.Contains(data, []byte(dirProviderToOrbitMesh)) {
		t.Fatalf("transcript missing provider_to_orbitmesh direction: %s", string(data))
	}
	if !bytes.Contains(data, []byte("timestamp")) {
		t.Fatalf("transcript missing timestamps: %s", string(data))
	}
}

func scanJSONLines(t *testing.T, r io.Reader, ch chan<- map[string]any) {
	t.Helper()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var v map[string]any
		if err := json.Unmarshal(line, &v); err != nil {
			continue
		}
		ch <- v
	}
}

func writeJSONLine(t *testing.T, w io.Writer, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		t.Fatalf("write line: %v", err)
	}
}

func waitForJSONLine(t *testing.T, ch <-chan map[string]any, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case v := <-ch:
			if match(v) {
				return v
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching JSON line")
		}
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func vNum(v any) float64 {
	n, _ := v.(float64)
	return n
}
