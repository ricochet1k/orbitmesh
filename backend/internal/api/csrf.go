package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	csrfCookieName = "orbitmesh-csrf-token"
	csrfHeaderName = "X-CSRF-Token"
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
