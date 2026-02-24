package main

import (
	"context"
	"encoding/json"
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
	// Register built-in tool definitions so providers can filter and advertise them.
	if err := tools.Global().Register(tools.ToolDef{
		Name:        "read_file",
		Description: "Read the contents of a file at the given path",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative file path"}},"required":["path"]}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			log.Printf("TOOL read_file handler %v", string(input))
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", fmt.Errorf("read_file: invalid arguments: %w", err)
			}
			data, err := os.ReadFile(args.Path)
			if err != nil {
				return "", fmt.Errorf("read_file: %w", err)
			}
			return string(data), nil
		},
	}); err != nil {
		log.Fatalf("tools register read_file: %v", err)
	}
	if err := tools.Global().Register(tools.ToolDef{
		Name:        "write_file",
		Description: "Write content to a file at the given path",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var args struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", fmt.Errorf("write_file: invalid arguments: %w", err)
			}
			if err := os.WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
				return "", fmt.Errorf("write_file: %w", err)
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
		},
	}); err != nil {
		log.Fatalf("tools register write_file: %v", err)
	}

	baseDir := storage.DefaultBaseDir()
	store, err := storage.NewJSONFileStorage(baseDir)
	if err != nil {
		log.Fatalf("storage init : %v", err)
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
		EvalStorage:     store,
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}

	fmt.Println("OrbitMesh shut down cleanly")
}
