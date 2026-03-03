package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	dirProviderToOrbitMesh = "provider_to_orbitmesh"
	dirOrbitMeshToProvider = "orbitmesh_to_provider"
)

type transcriptRecorder struct {
	mu sync.Mutex
	f  *os.File
}

type transcriptEntry struct {
	Timestamp string `json:"timestamp"`
	Direction string `json:"direction"`
	Message   any    `json:"message"`
}

func newTranscriptRecorder(path string) (*transcriptRecorder, error) {
	if path == "" {
		return &transcriptRecorder{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open transcript file: %w", err)
	}
	return &transcriptRecorder{f: f}, nil
}

func (r *transcriptRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	return r.f.Close()
}

func (r *transcriptRecorder) Record(direction string, line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return
	}
	entry := transcriptEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Direction: direction,
		Message:   parseMaybeJSON(line),
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = r.f.Write(append(b, '\n'))
}

func parseMaybeJSON(line []byte) any {
	trimmed := trimLine(line)
	if len(trimmed) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err == nil {
		return v
	}
	return string(trimmed)
}
