package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ricochet1k/orbitmesh/internal/api"
	"github.com/ricochet1k/orbitmesh/internal/mcpws"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claude"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claudews"
	codexProvider "github.com/ricochet1k/orbitmesh/internal/provider/common/codex"
	openaiProvider "github.com/ricochet1k/orbitmesh/internal/provider/common/openai"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	ptyProvider "github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
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
	if maybeRunMCPBridge() {
		return
	}

	if err := tools.RegisterDefaultTools(tools.Global()); err != nil {
		log.Fatalf("tools register defaults: %v", err)
	}

	baseDir := storage.DefaultBaseDir()
	store, err := storage.NewJSONFileStorage(baseDir)
	if err != nil {
		log.Fatalf("storage init : %v", err)
	}

	providerStorage := storage.NewProviderConfigStorage(baseDir)
	agentStorage := storage.NewAgentConfigStorage(baseDir)
	projectStorage := storage.NewProjectStorage(baseDir)
	mcpConfigStore := storage.NewMCPGatewayConfigStorage(baseDir)

	factory := provider.NewDefaultFactory()
	factory.Register("adk", native.NewADKProvider())
	factory.Register("pty", ptyProvider.NewPTYProviderFactory())
	factory.Register("claude", claude.NewClaudeProvider())
	factory.Register("claude-ws", claudews.NewClaudeWSProviderFactory())
	factory.Register("codex", codexProvider.NewProvider(codexProvider.Config{}))
	factory.Register("acp", acp.NewProvider(acp.Config{}))
	factory.Register("openai", openaiProvider.NewProvider(openaiProvider.Config{}))

	broadcaster := service.NewEventBroadcaster(100)

	evalStore := storage.NewJSONStore[toolcall.EvalSnapshot](
		filepath.Join(baseDir, "evals"),
		func(s toolcall.EvalSnapshot) string { return s.ID },
	)

	sessionMsgDir := filepath.Join(baseDir, "sessions")
	sessionMsgStore := storage.NewSessionMessagesLogStore(sessionMsgDir)

	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:         store,
		TerminalStorage: store,
		Broadcaster:     broadcaster,
		EvalStorage:     evalStore,
		MessageLogStore: sessionMsgStore,
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
	addr := listenAddr()

	mcpGateway := mcpws.NewGateway(tools.Global(), mcpws.NewOTPStore(2*time.Minute))
	mcpGateway.SetAPIBaseURL("http://127.0.0.1" + addr)
	if cfg, err := mcpConfigStore.Get(); err != nil {
		log.Fatalf("mcp gateway config: %v", err)
	} else if err := mcpGateway.ApplySettings(context.Background(), cfg); err != nil {
		log.Fatalf("mcp gateway startup: %v", err)
	}

	handler := api.NewHandler(executor, broadcaster, store, providerStorage, agentStorage, projectStorage)
	handler.SetMCPGateway(mcpConfigStore, mcpGateway)
	handler.SetProviderTester(factory)
	handler.Mount(r)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resp, err := http.Get("http://192.168.1.31:1234/v1/models")
	if err != nil {
		log.Printf("http get error: %v", err)
	} else {
		log.Printf("http get ok: %v", resp)
		resp.Body.Close()
	}

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
	if err := mcpGateway.Shutdown(shutdownCtx); err != nil {
		log.Printf("mcp gateway shutdown: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}

	fmt.Println("OrbitMesh shut down cleanly")
}
