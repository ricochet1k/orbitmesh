package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/mcpclient"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// pendingOAuth tracks an in-flight OAuth authorization code flow.
type pendingOAuth struct {
	ServerID    string
	Verifier    string
	RedirectURI string
	CreatedAt   time.Time
}

// oauthStateStore is an in-memory store for pending OAuth flows, keyed by state.
type oauthStateStore struct {
	mu      sync.Mutex
	pending map[string]pendingOAuth
}

func newOAuthStateStore() *oauthStateStore {
	return &oauthStateStore{pending: make(map[string]pendingOAuth)}
}

func (s *oauthStateStore) Put(state string, p pendingOAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clean expired entries (older than 10 minutes).
	now := time.Now()
	for k, v := range s.pending {
		if now.Sub(v.CreatedAt) > 10*time.Minute {
			delete(s.pending, k)
		}
	}
	s.pending[state] = p
}

func (s *oauthStateStore) Consume(state string) (pendingOAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[state]
	if ok {
		delete(s.pending, state)
	}
	return p, ok
}

// ---- handlers ----

func (h *Handler) listMCPServers(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	entries, err := h.mcpServerStorage.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list mcp servers", err.Error())
		return
	}
	responses := make([]apiTypes.MCPServerEntryResponse, len(entries))
	for i, e := range entries {
		responses[i] = mcpServerToResponse(e)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.MCPServerEntryListResponse{Servers: responses})
}

func (h *Handler) getMCPServer(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	entry, err := h.mcpServerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcpServerToResponse(*entry))
}

func (h *Handler) createMCPServer(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	var req apiTypes.MCPServerEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.Type != "command" && req.Type != "sse" {
		writeError(w, http.StatusBadRequest, "type must be 'command' or 'sse'", "")
		return
	}

	id := req.ID
	if id == "" {
		id = generateMCPServerID()
	}

	entry := mcpServerFromRequest(id, req)
	if err := h.mcpServerStorage.Save(entry); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save mcp server", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(mcpServerToResponse(entry))
}

func (h *Handler) updateMCPServer(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	var req apiTypes.MCPServerEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}

	// Preserve existing auth tokens if not overridden.
	existing, _ := h.mcpServerStorage.Get(id)
	entry := mcpServerFromRequest(id, req)
	if existing != nil && entry.Auth == nil && existing.Auth != nil {
		entry.Auth = existing.Auth
	} else if existing != nil && entry.Auth != nil && existing.Auth != nil {
		// Preserve stored OAuth tokens if the request doesn't supply new ones.
		if entry.Auth.AccessToken == "" {
			entry.Auth.AccessToken = existing.Auth.AccessToken
			entry.Auth.RefreshToken = existing.Auth.RefreshToken
			entry.Auth.ExpiresAt = existing.Auth.ExpiresAt
		}
	}

	if err := h.mcpServerStorage.Save(entry); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update mcp server", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcpServerToResponse(entry))
}

func (h *Handler) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.mcpServerStorage.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getMCPServerCapabilities(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	entry, err := h.mcpServerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	client, err := mcpclient.Connect(ctx, *entry)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect to mcp server", err.Error())
		return
	}
	defer client.Close()

	tools, toolsErr := client.ListTools(ctx)
	resources, resErr := client.ListResources(ctx)
	prompts, promptsErr := client.ListPrompts(ctx)

	// Log non-fatal errors but still return what we got.
	if toolsErr != nil {
		log.Printf("mcp server %s: list tools: %v", id, toolsErr)
	}
	if resErr != nil {
		log.Printf("mcp server %s: list resources: %v", id, resErr)
	}
	if promptsErr != nil {
		log.Printf("mcp server %s: list prompts: %v", id, promptsErr)
	}

	if tools == nil {
		tools = []apiTypes.MCPToolInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.MCPServerCapabilities{
		Tools:     tools,
		Resources: resources,
		Prompts:   prompts,
	})
}

func (h *Handler) startMCPOAuth(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	entry, err := h.mcpServerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}
	if entry.AuthType != "oauth2" {
		writeError(w, http.StatusBadRequest, "server is not configured for oauth2", "")
		return
	}
	if entry.Auth == nil {
		entry.Auth = &storage.MCPServerAuth{}
	}

	redirectURI := fmt.Sprintf("http://%s/api/v1/mcp/oauth/callback", r.Host)

	// If no client_id is configured, run OAuth metadata discovery + dynamic
	// client registration (RFC 8414 + RFC 7591).
	if entry.Auth.ClientID == "" {
		if entry.URL == "" {
			writeError(w, http.StatusBadRequest, "server url is required for oauth discovery", "")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		meta, err := mcpclient.DiscoverOAuthMetadata(ctx, entry.URL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "oauth metadata discovery failed", err.Error())
			return
		}

		entry.Auth.AuthURL = meta.AuthorizationEndpoint
		entry.Auth.TokenURL = meta.TokenEndpoint

		if meta.RegistrationEndpoint != "" {
			reg, err := mcpclient.DynamicRegister(ctx, meta.RegistrationEndpoint, redirectURI)
			if err != nil {
				log.Printf("mcp oauth: dynamic registration failed for %s: %v", entry.URL, err)
				// Save discovered endpoints so user only needs to add client_id.
				_ = h.mcpServerStorage.Save(*entry)
				writeError(w, http.StatusBadGateway, "dynamic client registration failed",
					fmt.Sprintf("Discovered auth server (auth_url: %s, token_url: %s) but registration failed: %v. "+
						"Set a client_id manually in Advanced OAuth settings.", meta.AuthorizationEndpoint, meta.TokenEndpoint, err))
				return
			}
			entry.Auth.ClientID = reg.ClientID
			entry.Auth.ClientSecret = reg.ClientSecret
			entry.Auth.DynamicRegistration = true
			entry.Auth.RegistrationEndpoint = meta.RegistrationEndpoint
		} else {
			// No registration endpoint — save discovered endpoints so user
			// only needs to provide a client_id.
			_ = h.mcpServerStorage.Save(*entry)
			writeError(w, http.StatusBadGateway, "server does not support dynamic client registration",
				fmt.Sprintf("Discovered auth server (auth_url: %s, token_url: %s) but it has no registration endpoint. "+
					"Set a client_id manually in Advanced OAuth settings.", meta.AuthorizationEndpoint, meta.TokenEndpoint))
			return
		}

		if err := h.mcpServerStorage.Save(*entry); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save registration", err.Error())
			return
		}
	}

	pkce, err := mcpclient.GeneratePKCE()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate PKCE", err.Error())
		return
	}

	state := generateOAuthState()

	authURL, err := mcpclient.BuildAuthURL(*entry.Auth, state, redirectURI, pkce.Challenge)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build auth url", err.Error())
		return
	}

	h.oauthStates.Put(state, pendingOAuth{
		ServerID:    id,
		Verifier:    pkce.Verifier,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.MCPOAuthStartResponse{
		AuthURL: authURL,
		State:   state,
	})
}

func (h *Handler) handleMCPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter", "")
		return
	}

	pending, ok := h.oauthStates.Consume(state)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown or expired state", "")
		return
	}

	entry, err := h.mcpServerStorage.Get(pending.ServerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}
	if entry.Auth == nil {
		writeError(w, http.StatusInternalServerError, "server auth config missing", "")
		return
	}

	updatedAuth, err := mcpclient.ExchangeCode(r.Context(), *entry.Auth, code, pending.Verifier, pending.RedirectURI)
	if err != nil {
		// Show error as HTML since this is a browser redirect.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h2>OAuth Error</h2><p>%s</p><p>You can close this window.</p></body></html>`, err.Error())
		return
	}

	entry.Auth = &updatedAuth
	if err := h.mcpServerStorage.Save(*entry); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h2>Error</h2><p>Failed to save token: %s</p></body></html>`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<html><body><h2>Authorization Successful</h2><p>You can close this window and return to OrbitMesh.</p><script>window.close()</script></body></html>`)
}

func (h *Handler) revokeMCPOAuthToken(w http.ResponseWriter, r *http.Request) {
	if h.mcpServerStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp server storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")
	entry, err := h.mcpServerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found", err.Error())
		return
	}
	if entry.Auth != nil {
		entry.Auth.AccessToken = ""
		entry.Auth.RefreshToken = ""
		entry.Auth.ExpiresAt = nil
	}
	if err := h.mcpServerStorage.Save(*entry); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke token", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func generateMCPServerID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "mcpsrv_" + hex.EncodeToString(b[:])
}

func generateOAuthState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func mcpServerFromRequest(id string, req apiTypes.MCPServerEntryRequest) storage.MCPServerEntry {
	entry := storage.MCPServerEntry{
		ID:       id,
		Name:     req.Name,
		Type:     req.Type,
		Command:  req.Command,
		Args:     req.Args,
		Env:      req.Env,
		URL:      req.URL,
		AuthType: req.AuthType,
	}
	if req.Auth != nil {
		entry.Auth = &storage.MCPServerAuth{
			Token:        req.Auth.Token,
			APIKey:       req.Auth.APIKey,
			ClientID:     req.Auth.ClientID,
			ClientSecret: req.Auth.ClientSecret,
			AuthURL:      req.Auth.AuthURL,
			TokenURL:     req.Auth.TokenURL,
			Scopes:       req.Auth.Scopes,
		}
	}
	return entry
}

func mcpServerToResponse(e storage.MCPServerEntry) apiTypes.MCPServerEntryResponse {
	resp := apiTypes.MCPServerEntryResponse{
		ID:       e.ID,
		Name:     e.Name,
		Type:     e.Type,
		Command:  e.Command,
		Args:     e.Args,
		Env:      e.Env,
		URL:      e.URL,
		AuthType: e.AuthType,
	}
	if e.Auth != nil {
		resp.ClientID = e.Auth.ClientID
		resp.AuthURL = e.Auth.AuthURL
		resp.TokenURL = e.Auth.TokenURL
		resp.Scopes = e.Auth.Scopes
		resp.HasToken = e.Auth.AccessToken != "" || e.Auth.Token != "" || e.Auth.APIKey != ""
		resp.DynamicRegistration = e.Auth.DynamicRegistration
	}
	return resp
}
