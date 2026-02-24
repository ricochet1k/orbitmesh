package provider

import (
	"context"
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Provider is the factory interface every backend provider must implement.
// CreateSession starts a new session; TestConfig validates the configuration
// without starting a full session (no user prompt is sent).
type Provider interface {
	CreateSession(sessionID string, config session.Config) (session.Session, error)
	TestConfig(ctx context.Context, config session.Config) error
}

type DefaultFactory struct {
	providers map[string]Provider
}

func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{
		providers: make(map[string]Provider),
	}
}

func (f *DefaultFactory) Register(providerType string, p Provider) {
	f.providers[providerType] = p
}

func (f *DefaultFactory) CreateSession(providerType, sessionID string, config session.Config) (session.Session, error) {
	p, ok := f.providers[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
	return p.CreateSession(sessionID, config)
}

// TestConfig validates the configuration for the given provider type.
func (f *DefaultFactory) TestConfig(ctx context.Context, providerType string, config session.Config) error {
	p, ok := f.providers[providerType]
	if !ok {
		return fmt.Errorf("unknown provider type: %s", providerType)
	}
	return p.TestConfig(ctx, config)
}

func (f *DefaultFactory) SupportedTypes() []string {
	types := make([]string, 0, len(f.providers))
	for t := range f.providers {
		types = append(types, t)
	}
	return types
}
