package codex

import (
	"encoding/json"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestBuildCodexCommand_Defaults(t *testing.T) {
	cmd, args := buildCodexCommand(Config{}, session.Config{})
	if cmd != "codex" {
		t.Fatalf("expected codex command, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("expected [app-server], got %v", args)
	}
}

func TestBuildCodexCommand_CustomOverrides(t *testing.T) {
	cmd, args := buildCodexCommand(Config{}, session.Config{
		Custom: map[string]any{
			"codex_command": "codex-dev",
			"codex_args":    []any{"--listen", "stdio://"},
		},
	})
	if cmd != "codex-dev" {
		t.Fatalf("expected codex-dev command, got %q", cmd)
	}
	if len(args) != 3 || args[0] != "app-server" || args[1] != "--listen" || args[2] != "stdio://" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildTurnStartParams_MapsCustomFields(t *testing.T) {
	params := buildTurnStartParams("thr_123", "hello", session.Config{
		WorkingDir: "/tmp/work",
		Custom: map[string]any{
			"model":           "gpt-5.3-codex",
			"effort":          "high",
			"summary":         "concise",
			"approval_policy": "unlessTrusted",
			"sandbox_mode":    "workspaceWrite",
			"network_access":  true,
		},
	})

	if params["threadId"] != "thr_123" {
		t.Fatalf("expected threadId to be set")
	}
	if params["model"] != "gpt-5.3-codex" {
		t.Fatalf("expected model to be set")
	}
	if params["effort"] != "high" {
		t.Fatalf("expected effort to be set")
	}
	if params["approvalPolicy"] != "unlessTrusted" {
		t.Fatalf("expected approvalPolicy to be set")
	}

	policy, ok := params["sandboxPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandboxPolicy map, got %T", params["sandboxPolicy"])
	}
	if policy["type"] != "workspaceWrite" {
		t.Fatalf("expected workspaceWrite policy")
	}
	if policy["networkAccess"] != true {
		t.Fatalf("expected networkAccess=true")
	}
}

func TestParseThreadID(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"id":"thr_abc"}}`)
	id, err := parseThreadID(raw)
	if err != nil {
		t.Fatalf("parseThreadID returned error: %v", err)
	}
	if id != "thr_abc" {
		t.Fatalf("expected thr_abc, got %q", id)
	}
}
