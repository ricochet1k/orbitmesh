package claudews

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestProvider_TestConfig_RealClaudeWSInitialize(t *testing.T) {
	command := resolveClaudeCommand(session.Config{})
	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("claude CLI not found for command %q", command)
	}

	p := NewClaudeWSProviderFactory()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := p.TestConfig(ctx, session.Config{WorkingDir: t.TempDir()}); err != nil {
		t.Fatalf("expected TestConfig initialize probe to succeed with real ClaudeWS CLI: %v", err)
	}
}

func TestProvider_TestConfig_LegacyPermissionModeIsAccepted(t *testing.T) {
	command := resolveClaudeCommand(session.Config{})
	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("claude CLI not found for command %q", command)
	}

	p := NewClaudeWSProviderFactory()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	err := p.TestConfig(ctx, session.Config{
		WorkingDir: t.TempDir(),
		Custom: map[string]any{
			"permission_mode": "autoEdit",
		},
	})

	if err != nil {
		t.Fatalf("expected legacy permission_mode autoEdit to be normalized and succeed, got: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("expected startup probe to finish quickly; took %v", time.Since(start))
	}
}

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

func TestBuildWSCommandArgs_DoesNotInjectPrintPrompt(t *testing.T) {
	args, err := buildWSCommandArgs("ws://127.0.0.1:9999", session.Config{})
	if err != nil {
		t.Fatalf("buildWSCommandArgs() unexpected error: %v", err)
	}

	for i := 0; i < len(args); i++ {
		if args[i] == "-p" || args[i] == "--print" {
			t.Fatalf("did not expect print-mode flags in claudews args, got %v", args)
		}
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

func TestBuildWSCommandArgs_AutoResumeFromSessionCustom(t *testing.T) {
	args, err := buildWSCommandArgs("ws://127.0.0.1:9999", session.Config{
		SessionCustom: map[string]any{"claude_session_id": "sess_ws_123"},
	})
	if err != nil {
		t.Fatalf("buildWSCommandArgs() unexpected error: %v", err)
	}

	hasResume := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--resume" && args[i+1] == "sess_ws_123" {
			hasResume = true
			break
		}
	}
	if !hasResume {
		t.Fatalf("expected --resume sess_ws_123 in args, got %v", args)
	}
}

func TestBuildWSCommandArgs_AutoContinueFromSessionCustom(t *testing.T) {
	args, err := buildWSCommandArgs("ws://127.0.0.1:9999", session.Config{
		SessionCustom: map[string]any{"claude_has_prior_session": true},
	})
	if err != nil {
		t.Fatalf("buildWSCommandArgs() unexpected error: %v", err)
	}

	hasContinue := false
	for _, arg := range args {
		if arg == "--continue" {
			hasContinue = true
			break
		}
	}
	if !hasContinue {
		t.Fatalf("expected --continue in args, got %v", args)
	}
}

func TestBuildWSCommandArgs_NormalizesLegacyPermissionMode(t *testing.T) {
	args, err := buildWSCommandArgs("ws://127.0.0.1:9999", session.Config{
		Custom: map[string]any{"permission_mode": "autoEdit"},
	})
	if err != nil {
		t.Fatalf("buildWSCommandArgs() unexpected error: %v", err)
	}

	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--permission-mode" {
			found = true
			if args[i+1] != "acceptEdits" {
				t.Fatalf("expected autoEdit to normalize to acceptEdits, got %q", args[i+1])
			}
		}
	}
	if !found {
		t.Fatalf("expected --permission-mode in args, got %v", args)
	}
}
