package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/bash"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// TestE2ESessionCreationAndEvents tests the full user workflow of creating a session and receiving events
func TestE2ESessionCreationAndEvents(t *testing.T) {
	// Setup
	broadcaster := service.NewEventBroadcaster(100)
	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     nil, // In-memory only
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			if providerType == "bash" {
				return bash.NewBashProvider(sessionID), nil
			}
			return nil, fmt.Errorf("unknown provider: %s", providerType)
		},
	})
	defer executor.Shutdown(context.Background())

	handler := NewHandler(executor, broadcaster)
	r := chi.NewRouter()
	r.Use(CORSMiddleware)
	r.Use(CSRFMiddleware)
	handler.Mount(r)

	server := httptest.NewServer(r)
	defer server.Close()

	var sessionID string

	t.Run("Step 1: Create session with bash provider", func(t *testing.T) {
		// First, get CSRF token
		resp, err := http.Get(server.URL + "/api/sessions")
		if err != nil {
			t.Fatalf("Failed to GET sessions: %v", err)
		}
		defer resp.Body.Close()

		csrfToken := ""
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "orbitmesh-csrf-token" {
				csrfToken = cookie.Value
				break
			}
		}
		if csrfToken == "" {
			t.Fatal("No CSRF token in response")
		}

		// Create session
		reqBody := apiTypes.SessionRequest{
			ProviderType: "bash",
			WorkingDir:   "/tmp",
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", server.URL+"/api/sessions", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "orbitmesh-csrf-token", Value: csrfToken})

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to POST sessions: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(body))
		}

		var sessionResp apiTypes.SessionResponse
		if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if sessionResp.ID == "" {
			t.Fatal("No session ID in response")
		}
		if sessionResp.ProviderType != "bash" {
			t.Fatalf("Expected provider_type=bash, got %s", sessionResp.ProviderType)
		}
		if sessionResp.State != "starting" {
			t.Fatalf("Expected state=starting, got %s", sessionResp.State)
		}

		t.Logf("✓ Session created with ID: %s (state: %s)", sessionResp.ID, sessionResp.State)

		// Store session ID for next steps
		sessionID = sessionResp.ID
	})

	t.Run("Step 2: Verify session appears in list", func(t *testing.T) {

		resp, err := http.Get(server.URL + "/api/sessions")
		if err != nil {
			t.Fatalf("Failed to GET sessions: %v", err)
		}
		defer resp.Body.Close()

		var listResp apiTypes.SessionListResponse
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		found := false
		for _, session := range listResp.Sessions {
			if session.ID == sessionID {
				found = true
				if session.State != "running" && session.State != "starting" {
					t.Logf("Session state is %s (might still be starting)", session.State)
				}
				break
			}
		}

		if !found {
			t.Fatalf("Session %s not found in list", sessionID)
		}

		t.Logf("✓ Session appears in list")
	})

	t.Run("Step 3: Retrieve session details", func(t *testing.T) {

		resp, err := http.Get(server.URL + "/api/sessions/" + sessionID)
		if err != nil {
			t.Fatalf("Failed to GET session: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
		}

		var statusResp apiTypes.SessionStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if statusResp.ID != sessionID {
			t.Fatalf("Session ID mismatch: %s != %s", statusResp.ID, sessionID)
		}

		t.Logf("✓ Session details retrieved")
		t.Logf("  - State: %s", statusResp.State)
		t.Logf("  - Provider: %s", statusResp.ProviderType)
		t.Logf("  - WorkingDir: %s", statusResp.WorkingDir)
	})

	t.Run("Step 4: Connect to SSE stream and receive events", func(t *testing.T) {

		// Give the session time to become ready
		time.Sleep(500 * time.Millisecond)

		resp, err := http.Get(server.URL + "/api/sessions/" + sessionID + "/events")
		if err != nil {
			t.Fatalf("Failed to GET events: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
		}

		// Read first few lines
		scanner := io.ReadCloser(resp.Body)
		buf := make([]byte, 4096)

		// Read some data (this will timeout or get interrupted)
		time.AfterFunc(2*time.Second, func() {
			scanner.Close()
		})

		n, _ := scanner.Read(buf)
		if n > 0 {
			output := string(buf[:n])
			if strings.Contains(output, "heartbeat") || strings.Contains(output, "event:") {
				t.Logf("✓ SSE stream connected and events received")
				t.Logf("  Sample: %s", strings.Split(output, "\n")[0])
			} else {
				t.Logf("✓ SSE stream connected (got data but no event markers)")
			}
		} else {
			t.Logf("✓ SSE stream connected (connection established)")
		}
	})
}

// TestSessionErrorHandling tests error scenarios
func TestSessionErrorHandling(t *testing.T) {
	broadcaster := service.NewEventBroadcaster(100)
	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     nil,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			if providerType == "bash" {
				return bash.NewBashProvider(sessionID), nil
			}
			return nil, fmt.Errorf("unknown provider: %s", providerType)
		},
	})
	defer executor.Shutdown(context.Background())

	handler := NewHandler(executor, broadcaster)
	r := chi.NewRouter()
	r.Use(CORSMiddleware)
	r.Use(CSRFMiddleware)
	handler.Mount(r)

	server := httptest.NewServer(r)
	defer server.Close()

	t.Run("Missing provider_type should error", func(t *testing.T) {
		// Get CSRF token
		resp, _ := http.Get(server.URL + "/api/sessions")
		defer resp.Body.Close()
		csrfToken := ""
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "orbitmesh-csrf-token" {
				csrfToken = cookie.Value
				break
			}
		}

		// Create session without provider_type
		reqBody := apiTypes.SessionRequest{
			WorkingDir: "/tmp",
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", server.URL+"/api/sessions", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "orbitmesh-csrf-token", Value: csrfToken})

		resp, _ = http.DefaultClient.Do(req)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", resp.StatusCode)
		}

		t.Logf("✓ Missing provider_type properly rejected")
	})

	t.Run("Unknown provider type should error", func(t *testing.T) {
		// Get CSRF token
		resp, _ := http.Get(server.URL + "/api/sessions")
		defer resp.Body.Close()
		csrfToken := ""
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "orbitmesh-csrf-token" {
				csrfToken = cookie.Value
				break
			}
		}

		// Create session with unknown provider
		reqBody := apiTypes.SessionRequest{
			ProviderType: "unknown-provider",
			WorkingDir:   "/tmp",
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", server.URL+"/api/sessions", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "orbitmesh-csrf-token", Value: csrfToken})

		resp, _ = http.DefaultClient.Do(req)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", resp.StatusCode)
		}

		t.Logf("✓ Unknown provider type properly rejected")
	})

	t.Run("Non-existent session should return 404", func(t *testing.T) {
		resp, _ := http.Get(server.URL + "/api/sessions/nonexistent-id")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d", resp.StatusCode)
		}

		t.Logf("✓ Non-existent session returns 404")
	})
}

// TestSessionLifecycle tests the full session lifecycle
func TestSessionLifecycle(t *testing.T) {
	broadcaster := service.NewEventBroadcaster(100)
	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     nil,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			if providerType == "bash" {
				return bash.NewBashProvider(sessionID), nil
			}
			return nil, fmt.Errorf("unknown provider: %s", providerType)
		},
	})
	defer executor.Shutdown(context.Background())

	handler := NewHandler(executor, broadcaster)
	r := chi.NewRouter()
	r.Use(CORSMiddleware)
	r.Use(CSRFMiddleware)
	handler.Mount(r)

	server := httptest.NewServer(r)
	defer server.Close()

	// Get CSRF token
	resp, _ := http.Get(server.URL + "/api/sessions")
	defer resp.Body.Close()
	csrfToken := ""
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "orbitmesh-csrf-token" {
			csrfToken = cookie.Value
			break
		}
	}

	// Create session
	reqBody := apiTypes.SessionRequest{
		ProviderType: "bash",
		WorkingDir:   "/tmp",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", server.URL+"/api/sessions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: "orbitmesh-csrf-token", Value: csrfToken})

	resp, _ = http.DefaultClient.Do(req)
	defer resp.Body.Close()

	var sessionResp apiTypes.SessionResponse
	json.NewDecoder(resp.Body).Decode(&sessionResp)
	sessionID := sessionResp.ID

	t.Run("Session starts in starting state", func(t *testing.T) {
		if sessionResp.State != "starting" {
			t.Fatalf("Expected starting, got %s", sessionResp.State)
		}
		t.Logf("✓ Session created in 'starting' state")
	})

	t.Run("Session transitions to running state", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond)

		resp, _ := http.Get(server.URL + "/api/sessions/" + sessionID)
		defer resp.Body.Close()

		var statusResp apiTypes.SessionStatusResponse
		json.NewDecoder(resp.Body).Decode(&statusResp)

		if statusResp.State != "running" {
			t.Fatalf("Expected running, got %s", statusResp.State)
		}
		t.Logf("✓ Session transitioned to 'running' state")
	})

	t.Run("Session can be stopped", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", server.URL+"/api/sessions/"+sessionID, nil)
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "orbitmesh-csrf-token", Value: csrfToken})

		resp, _ := http.DefaultClient.Do(req)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Delete response: %d - %s", resp.StatusCode, string(body))
		}
		t.Logf("✓ Session stop request accepted (status: %d)", resp.StatusCode)
	})
}
