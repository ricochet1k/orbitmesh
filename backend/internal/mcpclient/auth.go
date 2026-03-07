package mcpclient

import (
	"net/http"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// httpClientForEntry returns an *http.Client with auth headers injected for
// SSE-type MCP servers. For command-type servers this is not called.
func httpClientForEntry(entry storage.MCPServerEntry) *http.Client {
	if entry.Auth == nil {
		return http.DefaultClient
	}

	var token string
	switch entry.AuthType {
	case "bearer":
		token = "Bearer " + entry.Auth.Token
	case "api_key":
		token = "Bearer " + entry.Auth.APIKey
	case "oauth2":
		// Use stored access token if present.
		token = "Bearer " + entry.Auth.AccessToken
	}

	if token == "" {
		return http.DefaultClient
	}

	return &http.Client{
		Transport: &authTransport{
			base:  http.DefaultTransport,
			token: token,
		},
	}
}

// authTransport injects an Authorization header on every request.
type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", t.token)
	return t.base.RoundTrip(req)
}
