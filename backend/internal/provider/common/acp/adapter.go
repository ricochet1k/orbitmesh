package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/ricochet1k/orbitmesh/internal/domain"
)

// acpClientAdapter implements acpsdk.Client for OrbitMesh.
// It receives requests and notifications from the ACP agent.
type acpClientAdapter struct {
	session *Session
}

var _ acpsdk.Client = (*acpClientAdapter)(nil)

// newACPClientAdapter creates a new adapter for the given session.
func newACPClientAdapter(session *Session) *acpClientAdapter {
	return &acpClientAdapter{
		session: session,
	}
}

// ReadTextFile handles file read requests from the agent.
func (a *acpClientAdapter) ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	// Ensure path is absolute
	path := req.Path
	if !filepath.IsAbs(path) {
		// Make relative to working directory
		if a.session.sessionConfig.WorkingDir != "" {
			path = filepath.Join(a.session.sessionConfig.WorkingDir, path)
		} else {
			return acpsdk.ReadTextFileResponse{}, fmt.Errorf("path must be absolute: %s", req.Path)
		}
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, err
	}

	content := string(data)

	// Handle line range if specified
	if req.Line != nil || req.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if req.Line != nil && *req.Line > 0 {
			start = *req.Line - 1
			if start >= len(lines) {
				start = len(lines)
			}
		}

		end := len(lines)
		if req.Limit != nil && *req.Limit > 0 && start+*req.Limit < end {
			end = start + *req.Limit
		}

		content = strings.Join(lines[start:end], "\n")
	}

	// Emit event for file read
	a.emitMetadata("file_read", map[string]any{
		"path": req.Path,
	})

	return acpsdk.ReadTextFileResponse{
		Content: content,
	}, nil
}

// WriteTextFile handles file write requests from the agent.
func (a *acpClientAdapter) WriteTextFile(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	// Ensure path is absolute
	path := req.Path
	if !filepath.IsAbs(path) {
		// Make relative to working directory
		if a.session.sessionConfig.WorkingDir != "" {
			path = filepath.Join(a.session.sessionConfig.WorkingDir, path)
		} else {
			return acpsdk.WriteTextFileResponse{}, fmt.Errorf("path must be absolute: %s", req.Path)
		}
	}

	// Create parent directory if needed
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return acpsdk.WriteTextFileResponse{}, err
		}
	}

	// Write the file
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		return acpsdk.WriteTextFileResponse{}, err
	}

	// Emit event for file write
	a.emitMetadata("file_write", map[string]any{
		"path": req.Path,
		"size": len(req.Content),
	})

	return acpsdk.WriteTextFileResponse{}, nil
}

// RequestPermission handles permission requests from the agent.
func (a *acpClientAdapter) RequestPermission(ctx context.Context, req acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}

	// Emit permission request as metadata so it can be handled by the orchestration layer
	a.emitMetadata("permission_request", map[string]any{
		"tool_call": req.ToolCall,
		"title":     title,
		"options":   req.Options,
	})

	// For now, auto-approve the first option if available
	// TODO: Integrate with OrbitMesh permission system
	if len(req.Options) == 0 {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.RequestPermissionOutcome{
				Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{},
			},
		}, nil
	}

	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Selected: &acpsdk.RequestPermissionOutcomeSelected{
				OptionId: req.Options[0].OptionId,
			},
		},
	}, nil
}

// SessionUpdate handles session update notifications from the agent.
func (a *acpClientAdapter) SessionUpdate(ctx context.Context, notif acpsdk.SessionNotification) error {
	update := notif.Update

	// Note: Usage tracking happens on PromptResponse, not SessionUpdate
	// The streaming updates don't include final token counts

	switch {
	case update.UserMessageChunk != nil:
		// User message echo - useful for confirmation
		a.handleContentBlock(update.UserMessageChunk.Content)
		a.emitMetadata("user_message_chunk", map[string]any{
			"content": update.UserMessageChunk.Content,
		})

	case update.AgentMessageChunk != nil:
		// Streaming agent message chunk
		a.handleContentBlock(update.AgentMessageChunk.Content)

	case update.AgentThoughtChunk != nil:
		// Internal reasoning/thinking process
		a.handleContentBlock(update.AgentThoughtChunk.Content)
		raw, _ := json.Marshal(update.AgentThoughtChunk)
		if update.AgentThoughtChunk.Content.Text != nil {
			a.session.events.Emit(domain.NewThoughtEvent(a.session.sessionID, update.AgentThoughtChunk.Content.Text.Text, raw))
		}

	case update.ToolCall != nil:
		// Tool call notification
		raw, _ := json.Marshal(update.ToolCall)
		a.session.events.Emit(domain.NewToolCallEvent(a.session.sessionID, domain.ToolCallData{
			Status: fmt.Sprint(update.ToolCall.Status),
			Title:  update.ToolCall.Title,
		}, raw))

	case update.ToolCallUpdate != nil:
		// Tool call status update
		raw, _ := json.Marshal(update.ToolCallUpdate)
		a.session.events.Emit(domain.NewToolCallEvent(a.session.sessionID, domain.ToolCallData{
			ID:     string(update.ToolCallUpdate.ToolCallId),
			Status: fmt.Sprint(update.ToolCallUpdate.Status),
			Title:  "tool call update",
		}, raw))

	case update.Plan != nil:
		// Agent's execution plan for complex tasks
		raw, _ := json.Marshal(update.Plan)
		a.session.events.Emit(domain.NewPlanEvent(a.session.sessionID, domain.PlanData{Description: fmt.Sprint(update.Plan)}, raw))

	case update.AvailableCommandsUpdate != nil:
		// Dynamic command discovery
		a.emitMetadata("available_commands", map[string]any{
			"commands": update.AvailableCommandsUpdate,
		})

	case update.CurrentModeUpdate != nil:
		// Session mode changes
		a.emitMetadata("mode_change", map[string]any{
			"mode": update.CurrentModeUpdate,
		})
	}

	return nil
}

// CreateTerminal handles terminal creation requests from the agent.
func (a *acpClientAdapter) CreateTerminal(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	// Generate unique terminal ID
	termID := fmt.Sprintf("term-%s-%d", a.session.sessionID[:8], time.Now().UnixNano())

	// Convert ACP env to map
	env := make(map[string]string)
	for _, e := range req.Env {
		env[e.Name] = e.Value
	}

	// Create terminal using manager
	_, err := a.session.terminalManager.Create(
		termID,
		req.Command,
		req.Args,
		req.Cwd,
		env,
	)
	if err != nil {
		return acpsdk.CreateTerminalResponse{}, fmt.Errorf("failed to create terminal: %w", err)
	}

	// Emit event
	a.emitMetadata("terminal_created", map[string]any{
		"terminal_id": termID,
		"command":     req.Command,
		"args":        req.Args,
	})

	return acpsdk.CreateTerminalResponse{TerminalId: termID}, nil
}

// TerminalOutput handles terminal output requests.
func (a *acpClientAdapter) TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	term, err := a.session.terminalManager.Get(req.TerminalId)
	if err != nil {
		return acpsdk.TerminalOutputResponse{}, err
	}

	// Read all output captured so far
	output, truncated := term.outputLog.ReadAll()

	// Check if process has exited
	var exitStatus *acpsdk.TerminalExitStatus
	term.mu.RLock()
	if term.exitCode != nil || term.exitSignal != nil {
		exitStatus = &acpsdk.TerminalExitStatus{
			ExitCode: term.exitCode,
			Signal:   term.exitSignal,
		}
	}
	term.mu.RUnlock()

	return acpsdk.TerminalOutputResponse{
		Output:     output,
		Truncated:  truncated,
		ExitStatus: exitStatus,
	}, nil
}

// WaitForTerminalExit handles waiting for terminal exit.
func (a *acpClientAdapter) WaitForTerminalExit(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	term, err := a.session.terminalManager.Get(req.TerminalId)
	if err != nil {
		return acpsdk.WaitForTerminalExitResponse{}, err
	}

	// Block until terminal exits or context cancelled
	if err := term.WaitForExit(ctx); err != nil {
		return acpsdk.WaitForTerminalExitResponse{}, err
	}

	// Get exit status
	term.mu.RLock()
	exitCode := term.exitCode
	signal := term.exitSignal
	term.mu.RUnlock()

	return acpsdk.WaitForTerminalExitResponse{
		ExitCode: exitCode,
		Signal:   signal,
	}, nil
}

// KillTerminalCommand handles terminal kill requests.
func (a *acpClientAdapter) KillTerminalCommand(ctx context.Context, req acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	return acpsdk.KillTerminalCommandResponse{}, a.session.terminalManager.Kill(req.TerminalId)
}

// ReleaseTerminal handles terminal release requests.
func (a *acpClientAdapter) ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, a.session.terminalManager.Release(req.TerminalId)
}

// handleContentBlock processes an ACP content block from session updates.
func (a *acpClientAdapter) handleContentBlock(block acpsdk.ContentBlock) {
	switch {
	case block.Text != nil:
		// Text output
		a.session.state.SetOutput(block.Text.Text)
		a.session.events.Emit(domain.NewOutputEvent(a.session.sessionID, block.Text.Text, nil))

		// Track assistant message for snapshots
		a.session.mu.Lock()
		a.session.messageHistory = append(a.session.messageHistory, SnapshotMessage{
			Role:      "assistant",
			Content:   block.Text.Text,
			Timestamp: time.Now(),
		})
		a.session.mu.Unlock()

	case block.Image != nil:
		// Image output (emit as metadata)
		a.emitMetadata("image", map[string]any{
			"source": block.Image,
		})
	}
}

func (a *acpClientAdapter) emitMetadata(key string, value any) {
	a.session.events.Emit(domain.NewMetadataEvent(a.session.sessionID, key, value, nil))
}
