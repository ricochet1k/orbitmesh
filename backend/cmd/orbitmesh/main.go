package main

import (
	"context"
	"encoding/json"
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
	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/mcpws"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

const (
	defaultPort             = "8080"
	shutdownTimeout         = 5 * time.Second
	gracefulShutdownTimeout = 15 * time.Minute
	drainLogEvery           = 750 * time.Millisecond
)

func listenAddr() string {
	if raw := strings.TrimSpace(os.Getenv("ORBITMESH_PORT")); raw != "" {
		return ":" + strings.TrimPrefix(raw, ":")
	}
	return ":" + defaultPort
}

func logShutdownEvent(event string, fields map[string]any) {
	payload := map[string]any{
		"event": event,
		"at":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range fields {
		payload[k] = v
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Printf("shutdown_event=%s fields=%v marshal_error=%v", event, fields, err)
		return
	}
	log.Printf("%s", encoded)
}

func blockersFromStatus(status service.ExecutorDrainStatus) []map[string]any {
	blockers := make([]map[string]any, 0, len(status.SessionRuns.WaitOn)+len(status.Evals.WaitOn))
	appendWaits := func(store string, waits []entity.DrainWait) {
		for _, wait := range waits {
			deps := make([]map[string]string, 0, len(wait.BlockedDeps))
			for _, dep := range wait.BlockedDeps {
				deps = append(deps, map[string]string{
					"kind": dep.Kind,
					"id":   dep.ID,
				})
			}
			entry := map[string]any{
				"store":     store,
				"reason":    wait.Reason,
				"entity_id": wait.EntityID,
				"age":       wait.Age.String(),
			}
			if wait.EnvelopeID != "" {
				entry["envelope_id"] = wait.EnvelopeID
			}
			if wait.Op != "" {
				entry["op"] = wait.Op
			}
			if len(deps) > 0 {
				entry["blocked_deps"] = deps
			}
			blockers = append(blockers, entry)
		}
	}

	appendWaits("sessions", status.SessionRuns.WaitOn)
	appendWaits("evals", status.Evals.WaitOn)
	return blockers
}

func awaitExecutorDrainWithProgress(ctx context.Context, executor *service.AgentExecutor) error {
	drainDone := make(chan error, 1)
	go func() {
		drainDone <- executor.AwaitDrain(ctx)
	}()

	ticker := time.NewTicker(drainLogEvery)
	defer ticker.Stop()

	for {
		select {
		case err := <-drainDone:
			return err
		case <-ticker.C:
			status := executor.DrainStatus()
			logShutdownEvent("shutdown.drain.progress", map[string]any{
				"session_mode":         status.SessionRuns.Mode,
				"eval_mode":            status.Evals.Mode,
				"session_waiter_count": len(status.SessionRuns.WaitOn),
				"eval_waiter_count":    len(status.Evals.WaitOn),
				"blockers":             blockersFromStatus(status),
				"session_errors":       status.SessionRuns.Errors,
				"eval_errors":          status.Evals.Errors,
			})
		}
	}
}

func main() {
	if maybeRunMCPBridge() {
		return
	}
	if maybeRunProviderCheck() {
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

	factory := buildDefaultProviderFactory()

	broadcaster := service.NewEventBroadcaster(100)

	evalStore := storage.NewJSONStore[toolcall.EvalSnapshot](
		filepath.Join(baseDir, "evals"),
		func(s toolcall.EvalSnapshot) string { return s.ID },
	)
	sessionRunDeferredStore := storage.NewJSONStore[entity.DeferredOpEnvelope](
		filepath.Join(baseDir, "session_start_deferred"),
		func(s entity.DeferredOpEnvelope) string { return s.EnvelopeID },
	)

	sessionMsgDir := filepath.Join(baseDir, "sessions")
	sessionMsgStore := storage.NewSessionMessagesLogStore(sessionMsgDir)

	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:                     store,
		TerminalStorage:             store,
		Broadcaster:                 broadcaster,
		EvalStorage:                 evalStore,
		SessionStartDeferredStorage: sessionRunDeferredStore,
		MessageLogStore:             sessionMsgStore,
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer drainCancel()
	shutdownStartedAt := time.Now().UTC()

	executor.BeginDrain(drainCtx)
	initialDrain := executor.DrainStatus()
	logShutdownEvent("shutdown.drain.start", map[string]any{
		"timeout":              gracefulShutdownTimeout.String(),
		"http_timeout":         shutdownTimeout.String(),
		"session_mode":         initialDrain.SessionRuns.Mode,
		"eval_mode":            initialDrain.Evals.Mode,
		"session_waiter_count": len(initialDrain.SessionRuns.WaitOn),
		"eval_waiter_count":    len(initialDrain.Evals.WaitOn),
		"blockers":             blockersFromStatus(initialDrain),
	})

	httpShutdownCh := make(chan error, 1)
	go func() {
		httpShutdownCh <- srv.Shutdown(shutdownCtx)
	}()

	drainErr := awaitExecutorDrainWithProgress(drainCtx, executor)
	if drainErr != nil {
		status := executor.DrainStatus()
		logShutdownEvent("shutdown.drain.timeout", map[string]any{
			"error":                drainErr.Error(),
			"elapsed":              time.Since(shutdownStartedAt).String(),
			"session_waiter_count": len(status.SessionRuns.WaitOn),
			"eval_waiter_count":    len(status.Evals.WaitOn),
			"blockers":             blockersFromStatus(status),
			"session_errors":       status.SessionRuns.Errors,
			"eval_errors":          status.Evals.Errors,
		})

		forceCtx, forceCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := executor.ForceStop(forceCtx); err != nil {
			logShutdownEvent("shutdown.force_stop.error", map[string]any{"error": err.Error()})
		} else {
			logShutdownEvent("shutdown.force_stop.done", map[string]any{})
		}
		forceCancel()
	} else {
		logShutdownEvent("shutdown.drain.complete", map[string]any{
			"elapsed": time.Since(shutdownStartedAt).String(),
		})
		if err := executor.ForceStop(shutdownCtx); err != nil {
			logShutdownEvent("shutdown.finalize.error", map[string]any{"error": err.Error()})
		}
	}

	if err := <-httpShutdownCh; err != nil {
		logShutdownEvent("shutdown.http.error", map[string]any{"error": err.Error()})
		log.Fatalf("server shutdown: %v", err)
	}

	if err := mcpGateway.Shutdown(shutdownCtx); err != nil {
		logShutdownEvent("shutdown.mcp.error", map[string]any{"error": err.Error()})
		log.Printf("mcp gateway shutdown: %v", err)
	}

	logShutdownEvent("shutdown.complete", map[string]any{
		"elapsed": time.Since(shutdownStartedAt).String(),
	})

	fmt.Println("OrbitMesh shut down cleanly")
}
