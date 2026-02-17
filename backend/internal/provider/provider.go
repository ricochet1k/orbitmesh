package provider

import (
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

type Provider interface {
	CreateSession(sessionID string, config session.Config) (session.Session, error)
}

type SessionCreateFunc func(sessionID string, config session.Config) (session.Session, error)

type DefaultFactory struct {
	creators map[string]SessionCreateFunc
}

func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{
		creators: make(map[string]SessionCreateFunc),
	}
}

func (f *DefaultFactory) Register(providerType string, creator SessionCreateFunc) {
	f.creators[providerType] = creator
}

func (f *DefaultFactory) CreateSession(providerType, sessionID string, config session.Config) (session.Session, error) {
	creator, ok := f.creators[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
	return creator(sessionID, config)
}

func (f *DefaultFactory) SupportedTypes() []string {
	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}
