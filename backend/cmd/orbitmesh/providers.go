package main

import (
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claude"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claudews"
	codexProvider "github.com/ricochet1k/orbitmesh/internal/provider/common/codex"
	openaiProvider "github.com/ricochet1k/orbitmesh/internal/provider/common/openai"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	ptyProvider "github.com/ricochet1k/orbitmesh/internal/provider/pty"
)

func buildDefaultProviderFactory() *provider.DefaultFactory {
	factory := provider.NewDefaultFactory()
	factory.Register("adk", native.NewADKProvider())
	factory.Register("pty", ptyProvider.NewPTYProviderFactory())
	factory.Register("claude", claude.NewClaudeProvider())
	factory.Register("claude-ws", claudews.NewClaudeWSProviderFactory())
	factory.Register("codex", codexProvider.NewProvider(codexProvider.Config{}))
	factory.Register("acp", acp.NewProvider(acp.Config{}))
	factory.Register("openai", openaiProvider.NewProvider(openaiProvider.Config{}))
	return factory
}
