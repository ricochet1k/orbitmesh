package openai

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type Config struct {
	APIKey string `json:"api_key"`
}

type Provider struct {
	client openai.Client
}

var _ provider.Provider = (*Provider)(nil)

func NewProvider(config Config) *Provider {
	return &Provider{
		client: openai.NewClient(
			option.WithBaseURL(""),
			option.WithAPIKey(""),
			// option.WithOrganization()
			// option.WithProject()
			// option.WithHTTPClient(),
			// option.WithHeader("key", "value"),
			// option.WithMiddleware()
			// option.WithMaxRetries()
		),
	}
}

func (o *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return &Session{
		provider: o,
	}, nil
}
