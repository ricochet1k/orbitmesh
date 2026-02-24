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
	openaiProvider "github.com/ricochet1k/orbitmesh/internal/provider/common/openai"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	ptyProvider "github.com/ricochet1k/orbitmesh/internal/provider/pty"
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
	factory.Register("adk", native.NewADKProvider())
	factory.Register("pty", ptyProvider.NewPTYProviderFactory())
	factory.Register("claude", claude.NewClaudeProvider())
	factory.Register("claude-ws", claudews.NewClaudeWSProviderFactory())
	factory.Register("acp", acp.NewProvider(acp.Config{}))
	factory.Register("openai", openaiProvider.NewProvider(openaiProvider.Config{}))

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
	handler.SetProviderTester(factory)
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
