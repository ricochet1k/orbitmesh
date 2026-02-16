package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestTerminals_ListAndGet(t *testing.T) {
	env := newTerminalTestEnv(t)
	server := httptest.NewServer(env.router())
	defer server.Close()

	sessionID := startTerminalSession(t, env)
	if _, err := env.executor.TerminalHub(sessionID); err != nil {
		t.Fatalf("failed to create terminal hub: %v", err)
	}

	env.provider.Emit(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &terminal.Snapshot{Rows: 2, Cols: 2, Lines: []string{"hi", ""}}})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/terminals", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/terminals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var listResp apiTypes.TerminalListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Terminals) != 1 {
		t.Fatalf("expected 1 terminal, got %d", len(listResp.Terminals))
	}
	if listResp.Terminals[0].ID != sessionID {
		t.Fatalf("terminal ID = %q, want %q", listResp.Terminals[0].ID, sessionID)
	}
	if listResp.Terminals[0].SessionID != sessionID {
		t.Fatalf("terminal session_id = %q, want %q", listResp.Terminals[0].SessionID, sessionID)
	}
	if listResp.Terminals[0].TerminalKind != apiTypes.TerminalKindAdHoc {
		t.Fatalf("terminal kind = %q, want %q", listResp.Terminals[0].TerminalKind, apiTypes.TerminalKindAdHoc)
	}

	snapshot := waitForTerminalSnapshot(t, server.URL, sessionID)
	if snapshot == nil || snapshot.Rows != 2 {
		t.Fatalf("expected snapshot to be persisted")
	}

	termResp := fetchTerminalDetail(t, server.URL+"/api/v1/terminals/"+sessionID)
	if termResp.SessionID != sessionID {
		t.Fatalf("terminal detail session_id = %q, want %q", termResp.SessionID, sessionID)
	}
	if termResp.TerminalKind != apiTypes.TerminalKindAdHoc {
		t.Fatalf("terminal detail kind = %q, want %q", termResp.TerminalKind, apiTypes.TerminalKindAdHoc)
	}
	if termResp.LastSnapshot == nil || termResp.LastSnapshot.Rows != 2 {
		t.Fatalf("expected terminal detail snapshot rows=2")
	}

	termSnapshot := fetchTerminalSnapshot(t, server.URL+"/api/v1/terminals/"+sessionID+"/snapshot")
	if termSnapshot.Rows != 2 {
		t.Fatalf("expected terminal snapshot rows=2, got %d", termSnapshot.Rows)
	}
	aliasSnapshot := fetchTerminalSnapshot(t, server.URL+"/api/v1/sessions/"+sessionID+"/terminal/snapshot")
	if aliasSnapshot.Rows != 2 {
		t.Fatalf("expected alias snapshot rows=2, got %d", aliasSnapshot.Rows)
	}
}

func TestTerminalSnapshot_AfterStop(t *testing.T) {
	env := newTerminalTestEnv(t)
	server := httptest.NewServer(env.router())
	defer server.Close()

	sessionID := startTerminalSession(t, env)
	if _, err := env.executor.TerminalHub(sessionID); err != nil {
		t.Fatalf("failed to create terminal hub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := env.executor.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("failed to stop session: %v", err)
	}

	snapshot := fetchTerminalSnapshot(t, server.URL+"/api/v1/terminals/"+sessionID+"/snapshot")
	if snapshot.Rows == 0 {
		t.Fatalf("expected snapshot after stop")
	}
}

func waitForTerminalSnapshot(t *testing.T, baseURL, terminalID string) *apiTypes.TerminalSnapshot {
	t.Helper()
	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/v1/terminals/" + terminalID)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var term apiTypes.TerminalResponse
		_ = json.NewDecoder(resp.Body).Decode(&term)
		resp.Body.Close()
		if term.LastSnapshot != nil {
			return term.LastSnapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func fetchTerminalSnapshot(t *testing.T, url string) apiTypes.TerminalSnapshot {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", url, resp.StatusCode)
	}
	var snapshot apiTypes.TerminalSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return snapshot
}

func fetchTerminalDetail(t *testing.T, url string) apiTypes.TerminalResponse {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", url, resp.StatusCode)
	}
	var term apiTypes.TerminalResponse
	if err := json.NewDecoder(resp.Body).Decode(&term); err != nil {
		t.Fatalf("decode terminal detail: %v", err)
	}
	return term
}
