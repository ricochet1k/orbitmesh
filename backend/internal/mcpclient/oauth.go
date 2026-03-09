package mcpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// OAuthMetadata represents the OAuth 2.0 Authorization Server Metadata
// as defined in RFC 8414.
type OAuthMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
}

// DynamicRegistrationResponse is the response from RFC 7591 dynamic client registration.
type DynamicRegistrationResponse struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
}

// resourceMetadata is the Protected Resource Metadata per RFC 9728.
type resourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// DiscoverOAuthMetadata discovers the OAuth 2.0 Authorization Server Metadata
// for the given MCP server URL. It follows the MCP spec flow:
//  1. Hit the MCP server URL to provoke a 401 with WWW-Authenticate header
//     containing a resource_metadata URL (RFC 9728).
//  2. Fetch the Protected Resource Metadata to find authorization_servers.
//  3. Fetch the authorization server's .well-known metadata (RFC 8414).
//  4. Fall back to direct .well-known lookups on the server URL itself.
func DiscoverOAuthMetadata(ctx context.Context, serverURL string) (*OAuthMetadata, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("oauth discovery: invalid url: %w", err)
	}

	// Step 1: Try the MCP spec flow — hit the server to get resource_metadata from 401.
	if meta, err := discoverViaMCPProtocol(ctx, parsed); err == nil {
		return meta, nil
	}

	// Step 2: Fall back to direct well-known lookups on the server URL.
	if meta, err := discoverWellKnown(ctx, parsed); err == nil {
		return meta, nil
	}

	return nil, fmt.Errorf("oauth discovery: could not find OAuth metadata for %s (tried MCP 401 resource_metadata discovery and .well-known lookups)", parsed.Host)
}

// discoverViaMCPProtocol hits the MCP server URL expecting a 401 with
// WWW-Authenticate: Bearer resource_metadata="<url>" per the MCP auth spec.
func discoverViaMCPProtocol(ctx context.Context, serverURL *url.URL) (*OAuthMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Parse resource_metadata from WWW-Authenticate header.
	resourceMetaURL := parseResourceMetadataURL(resp.Header.Get("WWW-Authenticate"))
	if resourceMetaURL == "" {
		// Fallback: try the well-known resource metadata path per RFC 9728.
		resourceMetaURL = (&url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/.well-known/oauth-protected-resource",
		}).String()
	}

	// Fetch the Protected Resource Metadata (RFC 9728).
	rmReq, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceMetaURL, nil)
	if err != nil {
		return nil, err
	}
	rmReq.Header.Set("Accept", "application/json")

	rmResp, err := http.DefaultClient.Do(rmReq)
	if err != nil {
		return nil, fmt.Errorf("fetch resource metadata: %w", err)
	}
	defer rmResp.Body.Close()

	if rmResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resource metadata returned %d", rmResp.StatusCode)
	}

	raw, err := io.ReadAll(rmResp.Body)
	if err != nil {
		return nil, err
	}

	var rm resourceMetadata
	if err := json.Unmarshal(raw, &rm); err != nil {
		return nil, fmt.Errorf("decode resource metadata: %w", err)
	}

	if len(rm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("resource metadata has no authorization_servers")
	}

	// Try each authorization server until one works.
	for _, authServer := range rm.AuthorizationServers {
		authParsed, err := url.Parse(authServer)
		if err != nil {
			continue
		}
		if meta, err := discoverWellKnown(ctx, authParsed); err == nil {
			return meta, nil
		}
	}

	return nil, fmt.Errorf("none of the authorization_servers returned valid metadata")
}

// parseResourceMetadataURL extracts the resource_metadata URL from a
// WWW-Authenticate header value like: Bearer resource_metadata="https://..."
func parseResourceMetadataURL(header string) string {
	if header == "" {
		return ""
	}
	const prefix = "resource_metadata=\""
	idx := strings.Index(header, prefix)
	if idx < 0 {
		return ""
	}
	rest := header[idx+len(prefix):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// discoverWellKnown tries to fetch OAuth metadata from .well-known paths
// on the given URL. Per RFC 8414, when the URL has a path component, it is
// appended after .well-known (e.g., https://host/.well-known/oauth-authorization-server/path).
func discoverWellKnown(ctx context.Context, parsed *url.URL) (*OAuthMetadata, error) {
	wellKnownPrefixes := []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	}

	// Build candidate URLs: with path suffix and without.
	pathSuffix := strings.TrimRight(parsed.Path, "/")

	var candidates []string
	for _, wk := range wellKnownPrefixes {
		if pathSuffix != "" {
			candidates = append(candidates, (&url.URL{
				Scheme: parsed.Scheme,
				Host:   parsed.Host,
				Path:   wk + pathSuffix,
			}).String())
		}
		candidates = append(candidates, (&url.URL{
			Scheme: parsed.Scheme,
			Host:   parsed.Host,
			Path:   wk,
		}).String())
	}

	var lastErr error
	for _, candidate := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s returned %d", candidate, resp.StatusCode)
			continue
		}

		if err != nil {
			lastErr = err
			continue
		}

		var meta OAuthMetadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			lastErr = fmt.Errorf("%s: %w", candidate, err)
			continue
		}

		if meta.AuthorizationEndpoint != "" && meta.TokenEndpoint != "" {
			return &meta, nil
		}
		lastErr = fmt.Errorf("%s: missing authorization_endpoint or token_endpoint", candidate)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no well-known metadata found at %s", parsed.Host)
}

// DynamicRegister performs OAuth 2.0 Dynamic Client Registration (RFC 7591)
// at the given registration endpoint. The redirectURI is included in the
// client metadata so the authorization server knows where to redirect.
func DynamicRegister(ctx context.Context, registrationEndpoint, redirectURI string) (*DynamicRegistrationResponse, error) {
	clientMeta := map[string]any{
		"client_name":                "OrbitMesh",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}

	body, err := json.Marshal(clientMeta)
	if err != nil {
		return nil, fmt.Errorf("oauth register: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oauth register: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth register: request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth register: server returned %d: %s", resp.StatusCode, string(raw))
	}

	var reg DynamicRegistrationResponse
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("oauth register: decode response: %w", err)
	}
	if reg.ClientID == "" {
		return nil, fmt.Errorf("oauth register: response missing client_id")
	}
	return &reg, nil
}

// PKCEPair holds a PKCE code verifier and its derived challenge.
type PKCEPair struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE creates a cryptographically random PKCE verifier and its
// S256 challenge suitable for use in an OAuth2 authorization code flow.
func GeneratePKCE() (PKCEPair, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PKCEPair{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return PKCEPair{Verifier: verifier, Challenge: challenge}, nil
}

// BuildAuthURL constructs the OAuth2 authorization URL for the given server
// auth config, using PKCE and the provided state and redirect URI.
func BuildAuthURL(auth storage.MCPServerAuth, state, redirectURI, challenge string) (string, error) {
	if auth.AuthURL == "" {
		return "", fmt.Errorf("oauth: auth_url is required")
	}
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {auth.ClientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if len(auth.Scopes) > 0 {
		params.Set("scope", strings.Join(auth.Scopes, " "))
	}
	return auth.AuthURL + "?" + params.Encode(), nil
}

// tokenResponse is the JSON body returned by the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// ExchangeCode exchanges an authorization code for tokens. It updates the
// Auth struct in place and returns it (caller should persist the entry).
func ExchangeCode(ctx context.Context, auth storage.MCPServerAuth, code, verifier, redirectURI string) (storage.MCPServerAuth, error) {
	if auth.TokenURL == "" {
		return auth, fmt.Errorf("oauth: token_url is required")
	}
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {auth.ClientID},
		"code_verifier": {verifier},
	}
	if auth.ClientSecret != "" {
		body.Set("client_secret", auth.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.TokenURL,
		strings.NewReader(body.Encode()))
	if err != nil {
		return auth, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return auth, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var tok tokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return auth, fmt.Errorf("oauth: decode token response: %w", err)
	}
	if tok.Error != "" {
		return auth, fmt.Errorf("oauth: %s: %s", tok.Error, tok.ErrorDesc)
	}

	auth.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		auth.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		auth.ExpiresAt = &t
	}
	return auth, nil
}

// RefreshAccessToken uses a refresh token to obtain a new access token.
func RefreshAccessToken(ctx context.Context, auth storage.MCPServerAuth) (storage.MCPServerAuth, error) {
	if auth.RefreshToken == "" {
		return auth, fmt.Errorf("oauth: no refresh token available")
	}
	if auth.TokenURL == "" {
		return auth, fmt.Errorf("oauth: token_url is required")
	}
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {auth.RefreshToken},
		"client_id":     {auth.ClientID},
	}
	if auth.ClientSecret != "" {
		body.Set("client_secret", auth.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.TokenURL,
		strings.NewReader(body.Encode()))
	if err != nil {
		return auth, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return auth, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var tok tokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return auth, fmt.Errorf("oauth: decode refresh response: %w", err)
	}
	if tok.Error != "" {
		return auth, fmt.Errorf("oauth: refresh: %s: %s", tok.Error, tok.ErrorDesc)
	}

	auth.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		auth.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		auth.ExpiresAt = &t
	}
	return auth, nil
}
