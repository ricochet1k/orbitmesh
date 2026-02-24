package native

import (
	"context"
	"fmt"

	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// ADKProvider is the factory for ADK (Gemini) sessions.  It implements
// provider.Provider so that TestConfig can be a first-class method alongside
// CreateSession.
type ADKProvider struct{}

// NewADKProvider returns a new ADKProvider.
func NewADKProvider() *ADKProvider { return &ADKProvider{} }

// CreateSession implements provider.Provider.
func (p *ADKProvider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewADKSession(sessionID, adkConfigFromSession(config)), nil
}

// TestConfig implements provider.Provider.  It resolves the API key from the
// config/environment, then creates the Gemini model object.  Creating the model
// performs the initial client setup and key validation against the Gemini API,
// confirming connectivity without sending a user prompt.
func (p *ADKProvider) TestConfig(ctx context.Context, config session.Config) error {
	apiKey := ""
	if v, ok := config.Environment["GOOGLE_API_KEY"]; ok && v != "" {
		apiKey = v
	}
	if apiKey == "" {
		return ErrAPIKey
	}

	cfg := adkConfigFromSession(config)
	modelName := cfg.Model

	clientCfg := &genai.ClientConfig{
		APIKey: apiKey,
	}
	if cfg.UseVertexAI && cfg.ProjectID != "" {
		clientCfg.Project = cfg.ProjectID
		clientCfg.Location = cfg.Location
		clientCfg.APIKey = ""
	}

	_, err := gemini.NewModel(ctx, modelName, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to Gemini API: %w", err)
	}
	return nil
}

// adkConfigFromSession converts a session.Config into an ADKConfig.
// This mirrors the adkConfigFromProvider helper in main.go so we don't
// duplicate the logic in two places; main.go will use this provider directly.
func adkConfigFromSession(config session.Config) ADKConfig {
	cfg := ADKConfig{}
	if config.Custom == nil {
		return cfg
	}
	if model, ok := config.Custom["model"].(string); ok && model != "" {
		cfg.Model = model
	}
	if useVertex, ok := config.Custom["use_vertex_ai"].(bool); ok {
		cfg.UseVertexAI = useVertex
	}
	if projectID, ok := config.Custom["vertex_project_id"].(string); ok && projectID != "" {
		cfg.ProjectID = projectID
	}
	if location, ok := config.Custom["vertex_location"].(string); ok && location != "" {
		cfg.Location = location
	}
	return cfg
}
