package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ricochet1k/orbitmesh/internal/api"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claude"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claudews"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const (
	defaultPort     = "8080"
	shutdownTimeout = 5 * time.Second
)

func listenAddr() string {
	if raw := strings.TrimSpace(os.Getenv("ORBITMESH_PORT")); raw != "" {
		return ":" + strings.TrimPrefix(raw, ":")
	}
	return ":" + defaultPort
}

func main() {
	baseDir := storage.DefaultBaseDir()
	store, err := storage.NewJSONFileStorage(baseDir)
	if err != nil {
		log.Fatalf("storage init: %v", err)
	}

	providerStorage := storage.NewProviderConfigStorage(baseDir)
	agentStorage := storage.NewAgentConfigStorage(baseDir)
	projectStorage := storage.NewProjectStorage(baseDir)

	factory := provider.NewDefaultFactory()
	factory.Register("adk", func(sessionID string, config session.Config) (session.Session, error) {
		return native.NewADKSession(sessionID, adkConfigFromProvider(config)), nil
	})
	factory.Register("pty", func(sessionID string, config session.Config) (session.Session, error) {
		return pty.NewPTYProvider(sessionID), nil
	})
	factory.Register("claude", func(sessionID string, config session.Config) (session.Session, error) {
		return claude.NewClaudeCodeProvider(sessionID), nil
	})
	factory.Register("claude-ws", func(sessionID string, config session.Config) (session.Session, error) {
		// permHandler is nil → auto-allow all tools.
		// Callers can wire a custom handler by constructing the provider directly.
		return claudews.NewClaudeWSProvider(sessionID, nil), nil
	})
	factory.Register("acp", func(sessionID string, config session.Config) (session.Session, error) {
		return acp.NewSession(sessionID, acpConfigFromProvider(config), config)
	})

	broadcaster := service.NewEventBroadcaster(100)

	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:         store,
		TerminalStorage: store,
		Broadcaster:     broadcaster,
		ProviderFactory: func(providerType, sessionID string, config session.Config) (session.Session, error) {
			return factory.CreateSession(providerType, sessionID, config)
		},
	})
	if err := executor.Startup(context.Background()); err != nil {
		log.Fatalf("executor startup recovery: %v", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(api.CORSMiddleware)
	r.Use(api.CSRFMiddleware)

	handler := api.NewHandler(executor, broadcaster, store, providerStorage, agentStorage, projectStorage)
	handler.Mount(r)
	addr := listenAddr()

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("OrbitMesh listening on %s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := executor.Shutdown(shutdownCtx); err != nil {
		log.Printf("executor shutdown: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}

	fmt.Println("OrbitMesh shut down cleanly")
}

func acpConfigFromProvider(config session.Config) acp.Config {
	cfg := acp.Config{}
	if config.Custom == nil {
		return cfg
	}
	if command, ok := config.Custom["acp_command"].(string); ok && command != "" {
		cfg.Command = command
	}
	if args, ok := config.Custom["acp_args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				cfg.Args = append(cfg.Args, s)
			}
		}
	}
	return cfg
}

func adkConfigFromProvider(config session.Config) native.ADKConfig {
	adkCfg := native.ADKConfig{}
	if config.Custom == nil {
		return adkCfg
	}
	if model, ok := config.Custom["model"].(string); ok && model != "" {
		adkCfg.Model = model
	}
	if useVertex, ok := config.Custom["use_vertex_ai"].(bool); ok {
		adkCfg.UseVertexAI = useVertex
	}
	if projectID, ok := config.Custom["vertex_project_id"].(string); ok && projectID != "" {
		adkCfg.ProjectID = projectID
	}
	if location, ok := config.Custom["vertex_location"].(string); ok && location != "" {
		adkCfg.Location = location
	}
	return adkCfg
}
