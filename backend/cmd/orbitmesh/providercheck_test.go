package main

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/provider/conformance"
)

func TestParseProviderCheckArgs_Defaults(t *testing.T) {
	opts, err := parseProviderCheckArgs(nil)
	if err != nil {
		t.Fatalf("parseProviderCheckArgs() error = %v", err)
	}

	if opts.Lane != conformance.LaneOffline {
		t.Fatalf("lane = %s, want %s", opts.Lane, conformance.LaneOffline)
	}
	if opts.MaxUSD != 0 {
		t.Fatalf("max-usd = %f, want 0", opts.MaxUSD)
	}
	if opts.MaxTokens != 0 {
		t.Fatalf("max-tokens = %d, want 0", opts.MaxTokens)
	}
	if len(opts.Providers) != 0 {
		t.Fatalf("providers length = %d, want 0", len(opts.Providers))
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

	if opts.Lane != conformance.LaneLive {
		t.Fatalf("lane = %s, want %s", opts.Lane, conformance.LaneLive)
	}
	if opts.MaxUSD != 3.5 {
		t.Fatalf("max-usd = %f, want 3.5", opts.MaxUSD)
	}
	if opts.MaxTokens != 2000 {
		t.Fatalf("max-tokens = %d, want 2000", opts.MaxTokens)
	}
	if opts.ArtifactsDir != "/tmp/providercheck" {
		t.Fatalf("artifacts-dir = %q, want /tmp/providercheck", opts.ArtifactsDir)
	}
	if len(opts.Providers) != 2 || opts.Providers[0] != "openai" || opts.Providers[1] != "claude" {
		t.Fatalf("providers = %v, want [openai claude]", opts.Providers)
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
