package acp

import (
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Provider implements provider.Provider for ACP-compatible agents.
type Provider struct {
	config Config
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a new ACP provider with the given configuration.
func NewProvider(config Config) *Provider {
	return &Provider{
		config: config,
	}
}

// CreateSession creates a new ACP session.
func (p *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewSession(sessionID, p.config, config)
}
