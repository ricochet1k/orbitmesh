package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type ExecutorSubAgentSpawner struct {
	Executor *AgentExecutor
}

func (s *ExecutorSubAgentSpawner) SpawnAndAwait(ctx context.Context, agentID, projectID, task string) (string, error) {
	sessionID := uuid.New().String()

	cfg := session.Config{
		AgentID: agentID,
		Title:   "Sub-agent for task",
	}

	if projectID != "" {
		cfg.ProjectID = projectID
	}

	_, err := s.Executor.CreateSession(ctx, sessionID, cfg)
	if err != nil {
		return "", err
	}

	_, err = s.Executor.SendMessageWithOptions(ctx, sessionID, task, SendMessageOptions{})
	if err != nil {
		return "", err
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	time.Sleep(1 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			sess, err := s.Executor.GetSession(sessionID)
			if err != nil {
				continue
			}

			if sess.State == domain.SessionStateIdle || sess.State == domain.SessionStateSuspended {
				goto FetchMessages
			}
		}
	}

FetchMessages:
	msgs, err := s.Executor.GetSessionMessages(sessionID)
	if err != nil {
		return "Sub-agent session completed, but failed to fetch messages.", nil
	}

	if msgs == nil || len(msgs.Messages) == 0 {
		return "Sub-agent session completed (no messages).", nil
	}

	for i := len(msgs.Messages) - 1; i >= 0; i-- {
		m := msgs.Messages[i]
		if m.Kind == "text" || m.Kind == "markdown" || m.Kind == "error" {
			return strings.TrimSpace(string(m.Contents)), nil
		}
	}

	return "Sub-agent session completed, but found no text messages.", nil
}
