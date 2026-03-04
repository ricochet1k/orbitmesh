package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// createSessionViaHTTP POSTs a session against a live httptest.Server and
// returns the session ID.
func createSessionViaHTTP(t *testing.T, baseURL string) string {
	t.Helper()
	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp/test",
	})
	resp, err := http.Post(baseURL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
	}
	var session apiTypes.SessionResponse
	_ = json.NewDecoder(resp.Body).Decode(&session)
	return session.ID
}
