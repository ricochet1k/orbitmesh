package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

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
	userText := extractACPText(req.Prompt)

	payload := map[string]string{"echo": userText}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return acp.PromptResponse{}, fmt.Errorf("acp-echo: marshal: %w", err)
	}

	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: req.SessionId,
		Update:    acp.UpdateAgentMessageText(string(jsonBytes)),
	}); err != nil {
		return acp.PromptResponse{}, err
	}

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func extractACPText(blocks []acp.ContentBlock) string {
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

type observedWriter struct {
	mu      sync.Mutex
	target  io.Writer
	pending []byte
	onLine  func([]byte)
}

func (ow *observedWriter) Write(p []byte) (int, error) {
	ow.mu.Lock()
	defer ow.mu.Unlock()
	n, err := ow.target.Write(p)
	ow.pending = append(ow.pending, p...)
	for {
		idx := bytes.IndexByte(ow.pending, '\n')
		if idx < 0 {
			break
		}
		line := append([]byte(nil), ow.pending[:idx]...)
		ow.pending = ow.pending[idx+1:]
		ow.onLine(line)
	}
	return n, err
}

func runACPMode(ctx context.Context, controller *controller, recorder *transcriptRecorder) error {
	ag := &echoAgent{}

	stdinReader, stdinWriter := io.Pipe()
	inTap := &observedWriter{
		target: stdinWriter,
		onLine: func(line []byte) {
			controller.RecordInbound(line)
		},
	}

	stdoutTap := &observedWriter{
		target: os.Stdout,
		onLine: func(line []byte) {
			recorder.Record(dirProviderToOrbitMesh, line)
		},
	}

	controller.SetEmitter(func(line []byte) error {
		_, err := stdoutTap.Write(append(trimLine(line), '\n'))
		return err
	})

	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(inTap, os.Stdin)
		_ = stdinWriter.Close()
		copyErr <- err
	}()

	asc := acp.NewAgentSideConnection(ag, stdoutTap, stdinReader)
	ag.conn = asc

	select {
	case <-ctx.Done():
		return nil
	case <-asc.Done():
		return nil
	case err := <-copyErr:
		if err != nil {
			return err
		}
		return nil
	}
}
