package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
)

func TestGetACPRuntimeStats(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/acp/runtime", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats acp.RuntimePoolStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if stats.RuntimeCount < 0 || stats.ActiveSessions < 0 || stats.IdleRuntimes < 0 {
		t.Fatalf("unexpected negative stats: %+v", stats)
	}
	if stats.IdleTTLSeconds <= 0 {
		t.Fatalf("expected positive idle_ttl_seconds, got %d", stats.IdleTTLSeconds)
	}
}
