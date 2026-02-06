package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestTaskTree_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/tree", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.TaskTreeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.Tasks) == 0 {
		t.Fatalf("expected tasks, got none")
	}
	if resp.Tasks[0].ID != "epic-operations" {
		t.Fatalf("first task ID = %q, want %q", resp.Tasks[0].ID, "epic-operations")
	}
}
