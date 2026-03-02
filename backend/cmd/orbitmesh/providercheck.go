package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider/conformance"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	providerConfigSourceAPI   = "api"
	providerConfigSourceLocal = "local"
)

type providerCheckOptions struct {
	RunOptions           conformance.RunOptions
	ProviderConfigSource string
	APIBaseURL           string
}

func maybeRunProviderCheck() bool {
	if len(os.Args) < 2 || os.Args[1] != "providercheck" {
		return false
	}
	if err := runProviderCheck(context.Background(), os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "providercheck: %v\n", err)
		os.Exit(1)
	}
	return true
}

func runProviderCheck(ctx context.Context, args []string, out io.Writer) error {
	opts, err := parseProviderCheckArgs(args)
	if err != nil {
		return err
	}

	baseDir := storage.DefaultBaseDir()
	if opts.RunOptions.ArtifactsDir == "" {
		opts.RunOptions.ArtifactsDir = filepath.Join(baseDir, "providercheck-artifacts")
	}

	configs, err := loadProviderConfigs(ctx, opts, http.DefaultClient)
	if err != nil {
		return err
	}

	runner := conformance.NewBaselineRunner(buildDefaultProviderFactory(), opts.RunOptions)
	summary, err := runner.Run(ctx, configs)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(out, conformance.RenderTable(summary)); err != nil {
		return err
	}
	jsonBytes, err := conformance.RenderJSON(summary)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "%s\n", string(jsonBytes)); err != nil {
		return err
	}
	return nil
}

func parseProviderCheckArgs(args []string) (providerCheckOptions, error) {
	return parseProviderCheckOptions(args)
}

func parseProviderCheckOptions(args []string) (providerCheckOptions, error) {
	fs := flag.NewFlagSet("providercheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	rawProviders := fs.String("providers", "", "comma-separated provider types")
	rawLane := fs.String("lane", string(conformance.LaneOffline), "execution lane (offline|live)")
	maxUSD := fs.Float64("max-usd", 0, "maximum USD budget for live checks")
	maxTokens := fs.Int64("max-tokens", 0, "maximum token budget for live checks")
	artifactsDir := fs.String("artifacts-dir", "", "directory for providercheck artifacts")
	rawConfigSource := fs.String("provider-config-source", providerConfigSourceAPI, "provider config source (api|local)")
	apiBaseURL := fs.String("api-base-url", "", "OrbitMesh API base URL")

	if err := fs.Parse(args); err != nil {
		return providerCheckOptions{}, err
	}
	if *maxUSD < 0 {
		return providerCheckOptions{}, fmt.Errorf("--max-usd must be >= 0")
	}
	if *maxTokens < 0 {
		return providerCheckOptions{}, fmt.Errorf("--max-tokens must be >= 0")
	}

	lane, err := conformance.ParseLane(*rawLane)
	if err != nil {
		return providerCheckOptions{}, err
	}

	configSource := strings.ToLower(strings.TrimSpace(*rawConfigSource))
	if configSource == "" {
		configSource = providerConfigSourceAPI
	}
	if configSource != providerConfigSourceAPI && configSource != providerConfigSourceLocal {
		return providerCheckOptions{}, fmt.Errorf("--provider-config-source must be api or local")
	}

	providers := parseProviderList(*rawProviders)
	baseURL := resolveAPIBaseURL(*apiBaseURL)

	return providerCheckOptions{
		RunOptions: conformance.RunOptions{
			Providers:    providers,
			Lane:         lane,
			MaxUSD:       *maxUSD,
			MaxTokens:    *maxTokens,
			ArtifactsDir: strings.TrimSpace(*artifactsDir),
		},
		ProviderConfigSource: configSource,
		APIBaseURL:           baseURL,
	}, nil
}

func resolveAPIBaseURL(flagValue string) string {
	baseURL := strings.TrimSpace(flagValue)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ORBITMESH_API_BASE_URL"))
	}
	if baseURL == "" {
		port := strings.TrimSpace(os.Getenv("ORBITMESH_PORT"))
		if port == "" {
			port = "8080"
		}
		baseURL = "http://127.0.0.1:" + strings.TrimPrefix(port, ":")
	}
	return strings.TrimRight(baseURL, "/")
}

func loadProviderConfigs(ctx context.Context, opts providerCheckOptions, client *http.Client) ([]storage.ProviderConfig, error) {
	if opts.ProviderConfigSource == providerConfigSourceLocal {
		providerStore := storage.NewProviderConfigStorage(storage.DefaultBaseDir())
		configs, err := providerStore.List()
		if err != nil {
			return nil, fmt.Errorf("load provider configs from local storage: %w", err)
		}
		return configs, nil
	}

	configs, err := fetchProviderConfigsFromAPI(ctx, client, opts.APIBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to load provider configs from live OrbitMesh API (%s): %w; ensure OrbitMesh is running or use --provider-config-source local", opts.APIBaseURL, err)
	}
	return configs, nil
}

func fetchProviderConfigsFromAPI(ctx context.Context, client *http.Client, baseURL string) ([]storage.ProviderConfig, error) {
	if client == nil {
		client = http.DefaultClient
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/api/v1/providers", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET /api/v1/providers returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload apiTypes.ProviderConfigListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}

	configs := make([]storage.ProviderConfig, 0, len(payload.Providers))
	for _, cfg := range payload.Providers {
		configs = append(configs, storage.ProviderConfig{
			ID:       cfg.ID,
			Name:     cfg.Name,
			Type:     cfg.Type,
			Command:  cfg.Command,
			APIKey:   cfg.APIKey,
			Env:      cfg.Env,
			Custom:   cfg.Custom,
			IsActive: cfg.IsActive,
		})
	}
	return configs, nil
}

func parseProviderList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		item := strings.ToLower(strings.TrimSpace(part))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}
