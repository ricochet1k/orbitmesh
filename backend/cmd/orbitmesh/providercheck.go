package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/provider/conformance"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

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
	if opts.ArtifactsDir == "" {
		opts.ArtifactsDir = filepath.Join(baseDir, "providercheck-artifacts")
	}

	providerStore := storage.NewProviderConfigStorage(baseDir)
	configs, err := providerStore.List()
	if err != nil {
		return err
	}

	runner := conformance.NewBaselineRunner(buildDefaultProviderFactory(), opts)
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

func parseProviderCheckArgs(args []string) (conformance.RunOptions, error) {
	fs := flag.NewFlagSet("providercheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	rawProviders := fs.String("providers", "", "comma-separated provider types")
	rawLane := fs.String("lane", string(conformance.LaneOffline), "execution lane (offline|live)")
	maxUSD := fs.Float64("max-usd", 0, "maximum USD budget for live checks")
	maxTokens := fs.Int64("max-tokens", 0, "maximum token budget for live checks")
	artifactsDir := fs.String("artifacts-dir", "", "directory for providercheck artifacts")

	if err := fs.Parse(args); err != nil {
		return conformance.RunOptions{}, err
	}
	if *maxUSD < 0 {
		return conformance.RunOptions{}, fmt.Errorf("--max-usd must be >= 0")
	}
	if *maxTokens < 0 {
		return conformance.RunOptions{}, fmt.Errorf("--max-tokens must be >= 0")
	}

	lane, err := conformance.ParseLane(*rawLane)
	if err != nil {
		return conformance.RunOptions{}, err
	}

	providers := parseProviderList(*rawProviders)

	return conformance.RunOptions{
		Providers:    providers,
		Lane:         lane,
		MaxUSD:       *maxUSD,
		MaxTokens:    *maxTokens,
		ArtifactsDir: strings.TrimSpace(*artifactsDir),
	}, nil
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
