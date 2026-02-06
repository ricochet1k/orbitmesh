package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ricochet1k/orbitmesh/internal/api"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const (
	listenAddr      = ":8080"
	shutdownTimeout = 5 * time.Second
)

func main() {
	store, err := storage.NewJSONFileStorage(storage.DefaultBaseDir())
	if err != nil {
		log.Fatalf("storage init: %v", err)
	}

	factory := provider.NewDefaultFactory()
	factory.Register("adk", func(sessionID string, config provider.Config) (provider.Provider, error) {
		return native.NewADKProvider(sessionID, native.ADKConfig{}), nil
	})
	factory.Register("pty", func(sessionID string, config provider.Config) (provider.Provider, error) {
		return pty.NewClaudePTYProvider(sessionID), nil
	})

	broadcaster := service.NewEventBroadcaster(100)

	executor := service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     store,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			return factory.Create(providerType, sessionID, config)
		},
	})

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(api.CSRFMiddleware)

	handler := api.NewHandler(executor, broadcaster)
	handler.Mount(r)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("OrbitMesh listening on %s\n", listenAddr)
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
