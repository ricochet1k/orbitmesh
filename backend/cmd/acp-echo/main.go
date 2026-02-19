// acp-echo is a minimal ACP agent that echoes the user's message back as a
// JSON string. It speaks the full ACP (Agent Client Protocol) over stdin/stdout
// and is designed for use in end-to-end tests that need a real provider session
// without incurring LLM costs.
//
// Usage:
//
//	acp-echo          # reads from stdin, writes to stdout (typical ACP mode)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	acp "github.com/coder/acp-go-sdk"
)

// echoAgent implements the ACP Agent interface.
// On every Prompt call it streams back a single JSON-encoded message
// containing the original user text, then signals end-of-turn.
type echoAgent struct {
	conn *acp.AgentSideConnection
}

var _ acp.Agent = (*echoAgent)(nil)

func (a *echoAgent) Initialize(_ context.Context, _ acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion:   acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{LoadSession: false},
	}, nil
}

func (a *echoAgent) Authenticate(_ context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *echoAgent) NewSession(_ context.Context, _ acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{SessionId: "echo-session"}, nil
}

func (a *echoAgent) SetSessionMode(_ context.Context, _ acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func (a *echoAgent) Cancel(_ context.Context, _ acp.CancelNotification) error {
	return nil
}

func (a *echoAgent) Prompt(ctx context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
	// Extract text from the prompt blocks.
	userText := extractText(req.Prompt)

	// Build the echo payload as JSON.
	payload := map[string]string{
		"echo": userText,
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return acp.PromptResponse{}, fmt.Errorf("acp-echo: marshal: %w", err)
	}

	// Stream the response back as an agent message text update.
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: req.SessionId,
		Update:    acp.UpdateAgentMessageText(string(jsonBytes)),
	}); err != nil {
		return acp.PromptResponse{}, err
	}

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// extractText returns the concatenated text from a slice of ContentBlocks.
func extractText(blocks []acp.ContentBlock) string {
	result := ""
	for _, block := range blocks {
		if block.Text != nil {
			result += block.Text.Text
		}
	}
	if result == "" {
		return "(no input)"
	}
	return result
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ag := &echoAgent{}
	asc := acp.NewAgentSideConnection(ag, os.Stdout, os.Stdin)
	ag.conn = asc

	select {
	case <-ctx.Done():
	case <-asc.Done():
	}
}
