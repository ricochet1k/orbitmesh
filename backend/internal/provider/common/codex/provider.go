package codex

import (
	"context"
	"fmt"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Provider is the factory for Codex app-server sessions.
type Provider struct {
	cfg Config
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a new Codex provider factory.
func NewProvider(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

// CreateSession creates a new Codex app-server session.
func (p *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewCodexProvider(sessionID, p.cfg), nil
}

// TestConfig performs a lightweight app-server handshake.
func (p *Provider) TestConfig(ctx context.Context, config session.Config) error {
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	s := NewCodexProvider("codex-test", p.cfg)
	if err := s.start(testCtx, config); err != nil {
		return fmt.Errorf("failed to start codex app-server: %w", err)
	}
	defer s.Kill()

	return nil
}
