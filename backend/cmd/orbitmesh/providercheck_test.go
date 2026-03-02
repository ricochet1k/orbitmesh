package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/provider/conformance"
)

func TestParseProviderCheckArgs_Defaults(t *testing.T) {
	opts, err := parseProviderCheckArgs(nil)
	if err != nil {
		t.Fatalf("parseProviderCheckArgs() error = %v", err)
	}

	if opts.RunOptions.Lane != conformance.LaneOffline {
		t.Fatalf("lane = %s, want %s", opts.RunOptions.Lane, conformance.LaneOffline)
	}
	if opts.RunOptions.MaxUSD != 0 {
		t.Fatalf("max-usd = %f, want 0", opts.RunOptions.MaxUSD)
	}
	if opts.RunOptions.MaxTokens != 0 {
		t.Fatalf("max-tokens = %d, want 0", opts.RunOptions.MaxTokens)
	}
	if len(opts.RunOptions.Providers) != 0 {
		t.Fatalf("providers length = %d, want 0", len(opts.RunOptions.Providers))
	}
	if opts.ProviderConfigSource != providerConfigSourceAPI {
		t.Fatalf("provider-config-source = %q, want %q", opts.ProviderConfigSource, providerConfigSourceAPI)
	}
}

func TestParseProviderCheckArgs_AllFlags(t *testing.T) {
	opts, err := parseProviderCheckArgs([]string{
		"--providers", "openai,claude,openai",
		"--lane", "live",
		"--max-usd", "3.5",
		"--max-tokens", "2000",
		"--artifacts-dir", " /tmp/providercheck ",
	})
	if err != nil {
		t.Fatalf("parseProviderCheckArgs() error = %v", err)
	}

	if opts.RunOptions.Lane != conformance.LaneLive {
		t.Fatalf("lane = %s, want %s", opts.RunOptions.Lane, conformance.LaneLive)
	}
	if opts.RunOptions.MaxUSD != 3.5 {
		t.Fatalf("max-usd = %f, want 3.5", opts.RunOptions.MaxUSD)
	}
	if opts.RunOptions.MaxTokens != 2000 {
		t.Fatalf("max-tokens = %d, want 2000", opts.RunOptions.MaxTokens)
	}
	if opts.RunOptions.ArtifactsDir != "/tmp/providercheck" {
		t.Fatalf("artifacts-dir = %q, want /tmp/providercheck", opts.RunOptions.ArtifactsDir)
	}
	if len(opts.RunOptions.Providers) != 2 || opts.RunOptions.Providers[0] != "openai" || opts.RunOptions.Providers[1] != "claude" {
		t.Fatalf("providers = %v, want [openai claude]", opts.RunOptions.Providers)
	}
}

func TestParseProviderCheckArgs_BudgetValidation(t *testing.T) {
	if _, err := parseProviderCheckArgs([]string{"--max-usd", "-1"}); err == nil {
		t.Fatal("expected error for negative --max-usd")
	}
	if _, err := parseProviderCheckArgs([]string{"--max-tokens", "-10"}); err == nil {
		t.Fatal("expected error for negative --max-tokens")
	}
}

func TestLoadProviderConfigs_UsesLiveAPIByDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"providers":[{"id":"cfg_openai","name":"OpenAI","type":"openai","is_active":true}]}`))
	}))
	defer server.Close()

	configs, err := loadProviderConfigs(context.Background(), providerCheckOptions{
		ProviderConfigSource: providerConfigSourceAPI,
		APIBaseURL:           server.URL,
	}, server.Client())
	if err != nil {
		t.Fatalf("loadProviderConfigs() error = %v", err)
	}
	if len(configs) != 1 || configs[0].Type != "openai" {
		t.Fatalf("configs = %+v, want single openai config", configs)
	}
}

func TestLiveConfigFiltering_UsesFetchedConfigs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"providers":[{"id":"cfg_pty","name":"PTY","type":"pty","is_active":true},{"id":"cfg_openai","name":"OpenAI","type":"openai","is_active":true}]}`))
	}))
	defer server.Close()

	configs, err := fetchProviderConfigsFromAPI(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchProviderConfigsFromAPI() error = %v", err)
	}
	matrix, err := conformance.BuildProviderMatrix([]string{"openai", "pty"}, configs, []string{"openai", "pty"})
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 2 {
		t.Fatalf("matrix len = %d, want 2", len(matrix))
	}
	if matrix[0].ProviderType != "openai" || matrix[1].ProviderType != "pty" {
		t.Fatalf("matrix = %+v, want openai + pty", matrix)
	}
	if !matrix[1].Capabilities.Ignored {
		t.Fatalf("expected pty to be ignored, got %+v", matrix[1].Capabilities)
	}
}

func TestLoadProviderConfigs_LiveAPIUnavailableActionableError(t *testing.T) {
	t.Parallel()

	_, err := loadProviderConfigs(context.Background(), providerCheckOptions{
		ProviderConfigSource: providerConfigSourceAPI,
		APIBaseURL:           "http://127.0.0.1:1",
	}, &http.Client{})
	if err == nil {
		t.Fatal("expected live API unavailable error")
	}
	if !strings.Contains(err.Error(), "live OrbitMesh API") || !strings.Contains(err.Error(), "--provider-config-source local") {
		t.Fatalf("error = %v, want actionable live API guidance", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error should include action guidance, got bare deadline exceeded: %v", err)
	}
}
