package provider

import (
	"fmt"
)

type Factory interface {
	Create(providerType, sessionID string, config Config) (Provider, error)
}

type ProviderCreateFunc func(sessionID string, config Config) (Provider, error)

type DefaultFactory struct {
	creators map[string]ProviderCreateFunc
}

func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{
		creators: make(map[string]ProviderCreateFunc),
	}
}

func (f *DefaultFactory) Register(providerType string, creator ProviderCreateFunc) {
	f.creators[providerType] = creator
}

func (f *DefaultFactory) Create(providerType, sessionID string, config Config) (Provider, error) {
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
