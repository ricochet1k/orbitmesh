package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCORSMiddleware(t *testing.T) {
	// Reset env after test
	originalEnv := os.Getenv("ORBITMESH_CORS_ALLOWED_ORIGINS")
	defer os.Setenv("ORBITMESH_CORS_ALLOWED_ORIGINS", originalEnv)

	tests := []struct {
		name           string
		allowedOrigins string
		origin         string
		expectedOrigin string
	}{
		{
			name:           "Default allow localhost:3000",
			allowedOrigins: "",
			origin:         "http://localhost:3000",
			expectedOrigin: "http://localhost:3000",
		},
		{
			name:           "Default deny malicious origin",
			allowedOrigins: "",
			origin:         "http://malicious.com",
			expectedOrigin: "",
		},
		{
			name:           "Configured allow origin (localhost:3000 is NOT allowed if env is set)",
			allowedOrigins: "https://app.orbitmesh.ai, http://localhost:3001",
			origin:         "http://localhost:3000",
			expectedOrigin: "",
		},
		{
			name:           "Configured allow origin",
			allowedOrigins: "https://app.orbitmesh.ai, http://localhost:3001",
			origin:         "https://app.orbitmesh.ai",
			expectedOrigin: "https://app.orbitmesh.ai",
		},
		{
			name:           "Configured deny origin",
			allowedOrigins: "https://app.orbitmesh.ai",
			origin:         "http://localhost:3001",
			expectedOrigin: "",
		},
		{
			name:           "No origin header",
			allowedOrigins: "",
			origin:         "",
			expectedOrigin: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ORBITMESH_CORS_ALLOWED_ORIGINS", tt.allowedOrigins)

			// We need to recreate the handler because allowedOrigins is captured at creation time in CORSMiddleware
			handler := CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			gotOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if gotOrigin != tt.expectedOrigin {
				t.Errorf("expected Access-Control-Allow-Origin %q, got %q", tt.expectedOrigin, gotOrigin)
			}

			if tt.expectedOrigin != "" {
				vary := rr.Header().Get("Vary")
				if vary != "Origin" {
					t.Errorf("expected Vary: Origin, got %q", vary)
				}

				methods := rr.Header().Get("Access-Control-Allow-Methods")
				if methods == "" {
					t.Error("expected Access-Control-Allow-Methods to be set for allowed origin")
				}
			} else if tt.origin != "" {
				// If origin was provided but not allowed, headers should NOT be set
				if rr.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Error("Access-Control-Allow-Origin should NOT be set for disallowed origin")
				}
				if rr.Header().Get("Access-Control-Allow-Methods") != "" {
					t.Error("Access-Control-Allow-Methods should NOT be set for disallowed origin")
				}
			}
		})
	}
}
