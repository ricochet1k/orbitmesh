package claudews

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestBuildWSCommandArgs_UsesKebabCaseToolFlags(t *testing.T) {
	args, err := buildWSCommandArgs("ws://127.0.0.1:9999", session.Config{
		AllowedTools:    []string{"Bash"},
		DisallowedTools: []string{"Edit"},
	})
	if err != nil {
		t.Fatalf("buildWSCommandArgs() unexpected error: %v", err)
	}

	hasAllowed := false
	hasDisallowed := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--allowed-tools" && args[i+1] == "Bash" {
			hasAllowed = true
		}
		if args[i] == "--disallowed-tools" && args[i+1] == "Edit" {
			hasDisallowed = true
		}
	}

	if !hasAllowed {
		t.Fatalf("expected --allowed-tools Bash in args, got %v", args)
	}
	if !hasDisallowed {
		t.Fatalf("expected --disallowed-tools Edit in args, got %v", args)
	}
}

func TestResolveClaudeCommand(t *testing.T) {
	t.Run("prefers claudews_command", func(t *testing.T) {
		cfg := session.Config{Custom: map[string]any{
			"claudews_command": "./bin/claude-ws-real",
			"claude_command":   "./bin/claude-real",
		}}
		if got := resolveClaudeCommand(cfg); got != "./bin/claude-ws-real" {
			t.Fatalf("resolveClaudeCommand() = %q, want %q", got, "./bin/claude-ws-real")
		}
	})

	t.Run("falls back to claude_command", func(t *testing.T) {
		cfg := session.Config{Custom: map[string]any{"claude_command": "./bin/claude-real"}}
		if got := resolveClaudeCommand(cfg); got != "./bin/claude-real" {
			t.Fatalf("resolveClaudeCommand() = %q, want %q", got, "./bin/claude-real")
		}
	})

	t.Run("uses env when custom missing", func(t *testing.T) {
		t.Setenv("CLAUDE_COMMAND", "claude-real")
		if got := resolveClaudeCommand(session.Config{}); got != "claude-real" {
			t.Fatalf("resolveClaudeCommand() = %q, want %q", got, "claude-real")
		}
	})

	t.Run("defaults to claude", func(t *testing.T) {
		t.Setenv("CLAUDE_COMMAND", "")
		if got := resolveClaudeCommand(session.Config{}); got != "claude" {
			t.Fatalf("resolveClaudeCommand() = %q, want %q", got, "claude")
		}
	})
}
