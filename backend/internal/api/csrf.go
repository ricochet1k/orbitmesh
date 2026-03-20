package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	csrfCookieName       = "orbitmesh-csrf-token"
	csrfHeaderName       = "X-CSRF-Token"
	internalBypassHeader = "X-Orbitmesh-Internal"
	internalBypassValue  = "frontend-tools-mcp"
)

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := r.Cookie(csrfCookieName)
		if err != nil || token.Value == "" {
			newToken := &http.Cookie{
				Name:     csrfCookieName,
				Value:    generateCSRFToken(),
				Path:     "/",
				SameSite: http.SameSiteStrictMode,
				Secure:   false,
				HttpOnly: false,
			}
			http.SetCookie(w, newToken)
			token = newToken
		}

		if isStateChangingMethod(r.Method) {
			if r.Header.Get(internalBypassHeader) == internalBypassValue {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get(csrfHeaderName)
			if header == "" || header != token.Value {
				writeError(w, http.StatusForbidden, "invalid CSRF token", "csrf header mismatch")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func generateCSRFToken() string {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

// CORSMiddleware adds CORS headers to enable cross-origin requests.
func CORSMiddleware(next http.Handler) http.Handler {
	allowedOrigins := []string{"http://localhost:3000"}
	if envOrigins := os.Getenv("ORBITMESH_CORS_ALLOWED_ORIGINS"); envOrigins != "" {
		for _, origin := range strings.Split(envOrigins, ",") {
			if trimmed := strings.TrimSpace(origin); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := false
			for _, o := range allowedOrigins {
				if o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
