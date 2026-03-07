package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Provider is the factory for Codex app-server sessions.
type Provider struct {
	cfg Config
}

var _ provider.Provider = (*Provider)(nil)
var _ provider.ResumeMessagePolicy = (*Provider)(nil)

// NewProvider creates a new Codex provider factory.
func NewProvider(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

// CreateSession creates a new Codex app-server session.
func (p *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewCodexProvider(sessionID, p.cfg), nil
}

func (p *Provider) BuildResumeMessages(history []domain.Message) []session.Message {
	if len(history) == 0 {
		return nil
	}
	out := make([]session.Message, 0, len(history))
	for _, msg := range history {
		contents := strings.TrimSpace(msg.Contents)
		if contents == "" {
			continue
		}
		mapped, ok := mapResumeMessage(msg, contents)
		if !ok {
			continue
		}
		out = append(out, mapped)
	}
	return out
}

func mapResumeMessage(msg domain.Message, contents string) (session.Message, bool) {
	var kind session.MessageKind
	switch msg.Kind {
	case domain.MessageKindUser:
		kind = session.MKUser
	case domain.MessageKindOutput, domain.MessageKindThought:
		kind = session.MKAssistant
	case domain.MessageKindSystem, domain.MessageKindError:
		kind = session.MKSystem
	default:
		return session.Message{}, false
	}
	return session.Message{ID: msg.ID, Kind: kind, Contents: contents}, true
}

// TestConfig performs a lightweight app-server handshake.
func (p *Provider) TestConfig(ctx context.Context, config session.Config) error {
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd, args := buildCodexCommand(p.cfg, config)
	env := mergeEnvironment(p.cfg.Environment, config.Environment)
	workingDir := config.WorkingDir
	if workingDir == "" {
		workingDir = p.cfg.WorkingDir
	}

	mode := "app-server"
	if !supportsAppServer(testCtx, cmd, env, workingDir) {
		mode = "proto"
		extra := []string{}
		if len(args) > 1 {
			extra = append(extra, args[1:]...)
		}
		args = append([]string{"proto"}, extra...)
	}

	mgr, err := process.Start(testCtx, process.Config{
		Command:     cmd,
		Args:        args,
		WorkingDir:  workingDir,
		Environment: env,
	})
	if err != nil {
		return fmt.Errorf("failed to start codex process: %w", err)
	}
	defer mgr.Kill()

	if mode == "app-server" {
		if err := probeAppServerInitialize(testCtx, mgr); err != nil {
			return err
		}
		return nil
	}

	if err := probeProtoInitialize(testCtx, mgr); err != nil {
		return err
	}

	return nil
}

func supportsAppServer(ctx context.Context, cmd string, env map[string]string, workingDir string) bool {
	helpCmd := exec.CommandContext(ctx, cmd, "--help")
	if workingDir != "" {
		helpCmd.Dir = workingDir
	}
	helpCmd.Env = os.Environ()
	for k, v := range env {
		helpCmd.Env = append(helpCmd.Env, k+"="+v)
	}
	out, err := helpCmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	text := strings.ToLower(string(out))
	return strings.Contains(text, "app-server")
}

func probeAppServerInitialize(ctx context.Context, mgr *process.Manager) error {
	if mgr.Stdin() == nil || mgr.Stdout() == nil {
		return fmt.Errorf("codex app-server stdio not available")
	}

	initReq := map[string]any{
		"method": "initialize",
		"id":     int64(1),
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "orbitmesh-config-test",
				"title":   "OrbitMesh",
				"version": "0.1.0",
			},
		},
	}
	if err := writeJSONLine(mgr.Stdin(), initReq); err != nil {
		return fmt.Errorf("failed to send initialize: %w", err)
	}

	lineCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(mgr.Stdout())
		line, err := reader.ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}
		lineCh <- line
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for app-server initialize response: %w", ctx.Err())
	case err := <-errCh:
		return fmt.Errorf("failed to read app-server initialize response: %w", err)
	case line := <-lineCh:
		var resp rpcMessage
		if err := json.Unmarshal(line, &resp); err != nil {
			return fmt.Errorf("invalid app-server initialize response: %w", err)
		}
		if resp.Error != nil {
			return fmt.Errorf("initialize rejected: %s (%d)", resp.Error.Message, resp.Error.Code)
		}
		if resp.ID == nil || *resp.ID != 1 {
			return fmt.Errorf("initialize response missing matching id")
		}
	}

	if err := writeJSONLine(mgr.Stdin(), map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}
	return nil
}

func probeProtoInitialize(ctx context.Context, mgr *process.Manager) error {
	if mgr.Stdout() == nil {
		return fmt.Errorf("codex proto stdout not available")
	}

	type protoEnvelope struct {
		Msg struct {
			Type string `json:"type"`
		} `json:"msg"`
	}

	scanner := bufio.NewScanner(mgr.Stdout())
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var env protoEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Msg.Type == "session_configured" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for codex proto initialization: %w", ctx.Err())
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed while waiting for codex proto initialization: %w", err)
	}
	return fmt.Errorf("codex proto exited before reporting session_configured")
}

func writeJSONLine(w interface{ Write([]byte) (int, error) }, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// DiscoverModels attempts to list models via codex app-server model/list.
// Returns discovered models (if available) and the source used.
func DiscoverModels(ctx context.Context, staticCfg Config, config session.Config) ([]string, string, error) {
	discoverCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	cmd, args := buildCodexCommand(staticCfg, config)
	env := mergeEnvironment(staticCfg.Environment, config.Environment)
	workingDir := config.WorkingDir
	if workingDir == "" {
		workingDir = staticCfg.WorkingDir
	}

	if !supportsAppServer(discoverCtx, cmd, env, workingDir) {
		return nil, "fallback", fmt.Errorf("app-server mode unavailable")
	}

	mgr, err := process.Start(discoverCtx, process.Config{
		Command:     cmd,
		Args:        args,
		WorkingDir:  workingDir,
		Environment: env,
	})
	if err != nil {
		return nil, "fallback", err
	}
	defer mgr.Kill()

	if err := probeAppServerInitialize(discoverCtx, mgr); err != nil {
		return nil, "fallback", err
	}

	if err := writeJSONLine(mgr.Stdin(), map[string]any{"method": "model/list", "id": int64(2), "params": map[string]any{}}); err != nil {
		return nil, "fallback", err
	}

	scanner := bufio.NewScanner(mgr.Stdout())
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-discoverCtx.Done():
			return nil, "fallback", discoverCtx.Err()
		case <-timeout.C:
			return nil, "fallback", fmt.Errorf("model/list timed out")
		default:
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, "fallback", err
			}
			return nil, "fallback", fmt.Errorf("codex exited before model/list response")
		}
		line := scanner.Bytes()
		var resp rpcMessage
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID == nil || *resp.ID != 2 {
			continue
		}
		if resp.Error != nil {
			return nil, "fallback", fmt.Errorf("model/list: %s", resp.Error.Message)
		}
		models := parseModelListResult(resp.Result)
		if len(models) == 0 {
			return nil, "fallback", fmt.Errorf("model/list returned no models")
		}
		return models, "protocol:model/list", nil
	}
}

func parseModelListResult(result json.RawMessage) []string {
	if len(result) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil
	}
	candidates := []any{payload["models"], payload["availableModels"], payload["items"], payload["data"]}
	for _, candidate := range candidates {
		list, ok := candidate.([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			switch v := item.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					out = append(out, strings.TrimSpace(v))
				}
			case map[string]any:
				for _, key := range []string{"id", "model", "name"} {
					if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, strings.TrimSpace(s))
						break
					}
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}
