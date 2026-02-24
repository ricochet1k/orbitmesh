package openai

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Config holds OpenAI provider configuration, typically loaded from stored
// provider settings. Fields are overlaid by values in session.Config at
// session-creation time so that callers can override them per-session.
type Config struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Provider is the factory for OpenAI sessions.
type Provider struct {
	cfg Config
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a Provider with the given static configuration.  The API
// key and model may also be supplied at session-creation time via
// session.Config.Environment["OPENAI_API_KEY"] and
// session.Config.Custom["model"], which take precedence over the static config.
func NewProvider(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

// CreateSession builds a new OpenAI chat session.  It merges static provider
// configuration with per-session overrides supplied in config.
func (p *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	// Resolve API key: per-session env var wins over static config.
	apiKey := p.cfg.APIKey
	if v, ok := config.Environment["OPENAI_API_KEY"]; ok && v != "" {
		apiKey = v
	}
	if apiKey == "" {
		// Fall back to process environment so the provider works without any
		// stored config when OPENAI_API_KEY is set in the shell.
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	// Resolve base URL: per-session custom field wins over static config.
	baseURL := p.cfg.BaseURL
	if u, ok := config.Custom["base_url"].(string); ok && u != "" {
		baseURL = u
	}

	// Resolve model: per-session custom field wins over static config.
	model := p.cfg.Model
	if m, ok := config.Custom["model"].(string); ok && m != "" {
		model = m
	}
	if model == "" {
		model = string(openai.ChatModelGPT4o)
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	return newSession(sessionID, client, model, config.SystemPrompt, config.ResumeMessages), nil
}

// TestConfig implements provider.Provider.  It calls the OpenAI Models.List
// endpoint, which is a lightweight read-only call that confirms the API key is
// valid and the configured base URL is reachable.
func (p *Provider) TestConfig(ctx context.Context, config session.Config) error {
	apiKey := p.cfg.APIKey
	if v, ok := config.Environment["OPENAI_API_KEY"]; ok && v != "" {
		apiKey = v
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	baseURL := p.cfg.BaseURL
	if u, ok := config.Custom["base_url"].(string); ok && u != "" {
		baseURL = u
	}

	// Only require an API key when talking to the real OpenAI API.  When a
	// custom base_url is set (e.g. Ollama, vLLM, LM Studio) the key is
	// optional and many servers will reject a non-empty placeholder anyway.
	if apiKey == "" && baseURL == "" {
		return fmt.Errorf("OPENAI_API_KEY is not set")
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	// Models.List is a cheap read-only call: authenticates and confirms connectivity.
	page, err := client.Models.List(ctx)
	if err != nil {
		return fmt.Errorf("OpenAI API request failed: %w", err)
	}
	_ = page
	return nil
}
