package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

type frontendAnomalyLogger struct {
	path string
	mu   sync.Mutex
}

type frontendAnomalyLogEntry struct {
	LoggedAt      time.Time      `json:"logged_at"`
	RemoteAddr    string         `json:"remote_addr,omitempty"`
	UserAgent     string         `json:"user_agent,omitempty"`
	Kind          string         `json:"kind"`
	ProbeID       string         `json:"probe_id"`
	RoutePath     string         `json:"route_path"`
	Source        string         `json:"source"`
	Message       string         `json:"message"`
	AnimationName string         `json:"animation_name,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	DetectedAt    time.Time      `json:"detected_at"`
}

func newFrontendAnomalyLogger(rootDir string) *frontendAnomalyLogger {
	base := rootDir
	if base == "" {
		base = "."
	}
	return &frontendAnomalyLogger{
		path: filepath.Join(base, "logs", "frontend-ui-anomalies.ndjson"),
	}
}

func (l *frontendAnomalyLogger) Append(ctx context.Context, req apiTypes.FrontendUIAnomalyRequest, remoteAddr, userAgent string) error {
	if l == nil {
		return errors.New("frontend anomaly logger not configured")
	}

	if req.DetectedAt.IsZero() {
		req.DetectedAt = time.Now().UTC()
	}

	entry := frontendAnomalyLogEntry{
		LoggedAt:      time.Now().UTC(),
		RemoteAddr:    remoteAddr,
		UserAgent:     userAgent,
		Kind:          req.Kind,
		ProbeID:       req.ProbeID,
		RoutePath:     req.RoutePath,
		Source:        req.Source,
		Message:       req.Message,
		AnimationName: req.AnimationName,
		Metadata:      req.Metadata,
		DetectedAt:    req.DetectedAt,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	body, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal anomaly entry: %w", err)
	}
	body = append(body, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create anomaly log directory: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open anomaly log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(body); err != nil {
		return fmt.Errorf("append anomaly log entry: %w", err)
	}
	return nil
}

func (h *Handler) reportFrontendAnomaly(w http.ResponseWriter, r *http.Request) {
	if h.frontendAnomalyLog == nil {
		writeError(w, http.StatusServiceUnavailable, "frontend anomaly logging unavailable", "logger is not configured")
		return
	}

	var req apiTypes.FrontendUIAnomalyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Kind == "" || req.ProbeID == "" || req.RoutePath == "" || req.Message == "" {
		writeError(w, http.StatusBadRequest, "kind, probe_id, route_path, and message are required", "")
		return
	}
	if req.Source == "" {
		req.Source = "passive"
	}

	if err := h.frontendAnomalyLog.Append(r.Context(), req, r.RemoteAddr, r.UserAgent()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist frontend anomaly", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
