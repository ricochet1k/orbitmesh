package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

const deferredSessionStartKind = "session_start_command"

type deferredSessionStartPayload struct {
	Version int                  `json:"version"`
	Kind    string               `json:"kind"`
	ID      string               `json:"id"`
	Content string               `json:"content"`
	TokenID string               `json:"token_id,omitempty"`
	Results []session.ToolResult `json:"results,omitempty"`
	Options SendMessageOptions   `json:"options"`
}

func (e *AgentExecutor) deferSessionStartCommand(cmd SessionStartCommand) error {
	if e.sessionStartDeferredStorage == nil {
		return fmt.Errorf("deferred session start storage is not configured")
	}
	kind := strings.TrimSpace(string(cmd.Kind))
	if kind == "" {
		return fmt.Errorf("deferred session start command kind is required")
	}
	if cmd.Session == nil {
		return fmt.Errorf("deferred session start command requires session")
	}
	id := strings.TrimSpace(cmd.RawID)
	if id == "" {
		id = strings.TrimSpace(cmd.Session.ID)
	}
	if id == "" {
		return fmt.Errorf("deferred session start command requires session id")
	}

	payload := deferredSessionStartPayload{
		Version: 1,
		Kind:    kind,
		ID:      id,
		Content: cmd.Content,
		TokenID: strings.TrimSpace(cmd.TokenID),
		Results: append([]session.ToolResult(nil), cmd.Results...),
		Options: cmd.Options,
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode deferred session start command: %w", err)
	}

	key := sessionStartCommandKey(payload)
	if has, err := e.hasDeferredSessionStartByKey(key); err != nil {
		return err
	} else if has {
		return nil
	}

	now := time.Now().UTC()
	env := entity.DeferredOpEnvelope{
		EnvelopeID:     fmt.Sprintf("%s_%d", key, now.UnixNano()),
		Kind:           deferredSessionStartKind,
		EntityID:       payload.ID,
		Op:             entity.ActiveStoreStartCreate,
		Payload:        encodedPayload,
		IdempotencyKey: key,
		EnqueuedAt:     now,
	}
	if err := e.sessionStartDeferredStorage.Save(env); err != nil {
		return fmt.Errorf("save deferred session start command: %w", err)
	}
	return nil
}

func (e *AgentExecutor) hasDeferredSessionStartByKey(key string) (bool, error) {
	if e.sessionStartDeferredStorage == nil {
		return false, nil
	}
	envs, err := e.sessionStartDeferredStorage.List()
	if err != nil {
		return false, fmt.Errorf("list deferred session start commands: %w", err)
	}
	for _, env := range envs {
		if env.Kind != deferredSessionStartKind {
			continue
		}
		if strings.TrimSpace(env.IdempotencyKey) == key {
			return true, nil
		}
	}
	return false, nil
}

func sessionStartCommandKey(payload deferredSessionStartPayload) string {
	h := sha256.New()
	_, _ = h.Write([]byte(payload.Kind))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(payload.ID))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(payload.Content))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(payload.TokenID))
	_, _ = h.Write([]byte("\x00"))
	encodedResults, _ := json.Marshal(payload.Results)
	_, _ = h.Write(encodedResults)
	_, _ = h.Write([]byte("\x00"))
	encodedOptions, _ := json.Marshal(payload.Options)
	_, _ = h.Write(encodedOptions)
	sum := h.Sum(nil)
	return "sscmd_" + hex.EncodeToString(sum[:10])
}

func (e *AgentExecutor) replayDeferredSessionStartCommands(ctx context.Context) error {
	if e.sessionStartDeferredStorage == nil {
		return nil
	}

	envs, err := e.sessionStartDeferredStorage.List()
	if err != nil {
		return fmt.Errorf("list deferred session start commands: %w", err)
	}

	filtered := make([]entity.DeferredOpEnvelope, 0, len(envs))
	for _, env := range envs {
		if env.Kind == deferredSessionStartKind {
			filtered = append(filtered, env)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].EnqueuedAt.Equal(filtered[j].EnqueuedAt) {
			return filtered[i].EnvelopeID < filtered[j].EnvelopeID
		}
		return filtered[i].EnqueuedAt.Before(filtered[j].EnqueuedAt)
	})

	processedKeys := make(map[string]struct{}, len(filtered))
	for _, env := range filtered {
		if env.IdempotencyKey != "" {
			if _, seen := processedKeys[env.IdempotencyKey]; seen {
				_ = e.sessionStartDeferredStorage.Delete(env.EnvelopeID)
				continue
			}
		}

		var payload deferredSessionStartPayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			log.Printf("deferred session start replay: dropping malformed envelope %s: %v", env.EnvelopeID, err)
			_ = e.sessionStartDeferredStorage.Delete(env.EnvelopeID)
			continue
		}

		if strings.TrimSpace(payload.ID) == "" {
			_ = e.sessionStartDeferredStorage.Delete(env.EnvelopeID)
			continue
		}

		var startErr error
		switch SessionStartCommandKind(payload.Kind) {
		case SessionStartCommandMessage:
			_, startErr = e.SendMessageWithOptions(ctx, payload.ID, payload.Content, payload.Options)
		case SessionStartCommandResume:
			if strings.TrimSpace(payload.TokenID) == "" {
				startErr = fmt.Errorf("resume command missing token_id")
			} else {
				_, startErr = e.ResumeSessionWithToken(ctx, payload.ID, payload.TokenID)
			}
		case SessionStartCommandToolResultsResume:
			startErr = e.resumeSessionWithToolResultsManaged(payload.ID, payload.Results)
		default:
			startErr = fmt.Errorf("unsupported deferred session start command kind: %s", payload.Kind)
		}
		if startErr != nil {
			if errors.Is(startErr, ErrExecutorShutdown) {
				return startErr
			}
			log.Printf("deferred session start replay: keeping envelope %s after error: %v", env.EnvelopeID, startErr)
			continue
		}

		if env.IdempotencyKey != "" {
			processedKeys[env.IdempotencyKey] = struct{}{}
		}
		_ = e.sessionStartDeferredStorage.Delete(env.EnvelopeID)
	}

	return nil
}
