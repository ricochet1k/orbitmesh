package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type SessionStartCommandKind string

const (
	SessionStartCommandMessage           SessionStartCommandKind = "message"
	SessionStartCommandResume            SessionStartCommandKind = "resume"
	SessionStartCommandToolResultsResume SessionStartCommandKind = "tool_results_resume"
)

type SessionStartCommand struct {
	Kind     SessionStartCommandKind
	Session  *domain.Session
	Content  string
	Results  []session.ToolResult
	TokenID  string
	Options  SendMessageOptions
	State    domain.SessionState
	RawID    string
	StateErr error
}

type SessionStartResult struct {
	Session  *domain.Session
	State    domain.SessionState
	Err      error
	Deferred bool
}

// submitSessionStartCommand is the single admission point for starting
// session-run work. Message starts are routed through this method so we can
// later attach durable defer/replay semantics in one place.
func (e *AgentExecutor) submitSessionStartCommand(ctx context.Context, cmd SessionStartCommand) SessionStartResult {
	sess := cmd.Session
	if sess == nil {
		return SessionStartResult{Err: ErrSessionNotFound}
	}

	state := cmd.State
	if cmd.StateErr != nil {
		state = sess.GetState()
	} else if state == domain.SessionStateIdle && sess.GetState() == domain.SessionStateSuspended {
		// Respect only live in-memory suspension context; persisted session state is advisory.
		if sess.GetSuspensionContext() != nil {
			state = domain.SessionStateSuspended
		}
	}

	if cmd.Kind == SessionStartCommandResume || cmd.Kind == SessionStartCommandToolResultsResume {
		if e.isSessionStartDraining() {
			if cmd.Kind == SessionStartCommandResume && strings.TrimSpace(cmd.TokenID) != "" {
				if err := e.deferAndNotifySessionStart(cmd, "[shutdown] Resume queued for retry after restart."); err == nil {
					return SessionStartResult{Session: sess, State: state, Err: nil, Deferred: true}
				}
			}
			if cmd.Kind == SessionStartCommandToolResultsResume {
				if err := e.deferAndNotifySessionStart(cmd, ""); err == nil {
					return SessionStartResult{Session: sess, State: state, Err: nil, Deferred: true}
				}
			}
			return SessionStartResult{Session: sess, State: state, Err: ErrExecutorShutdown}
		}
		return SessionStartResult{Session: sess, State: state, Err: nil}
	}

	if cmd.Kind != SessionStartCommandMessage {
		return SessionStartResult{Session: sess, State: state, Err: fmt.Errorf("unsupported session start command kind: %s", cmd.Kind)}
	}

	switch state {
	case domain.SessionStateIdle:
		if e.isSessionStartDraining() {
			if err := e.deferAndNotifySessionStart(cmd, "[shutdown] Message queued for delivery after restart."); err != nil {
				return SessionStartResult{Session: sess, State: state, Err: ErrExecutorShutdown}
			}
			return SessionStartResult{Session: sess, State: state, Err: nil, Deferred: true}
		}
		s, deferred, err := e.startRunWithMessage(ctx, cmd.RawID, sess, cmd.Content, cmd.Options)
		if deferred {
			if deferErr := e.deferAndNotifySessionStart(cmd, "[shutdown] Message queued for delivery after restart."); deferErr != nil {
				return SessionStartResult{Session: sess, State: state, Err: ErrExecutorShutdown}
			}
			return SessionStartResult{Session: sess, State: state, Err: nil, Deferred: true}
		}
		return SessionStartResult{Session: s, State: state, Err: err}

	case domain.SessionStateRunning:
		return SessionStartResult{Session: sess, State: state, Err: fmt.Errorf("cannot send message to running session: %s", e.describeSessionBlocker(cmd.RawID, domain.SessionStateRunning))}

	case domain.SessionStateSuspended:
		return SessionStartResult{Session: sess, State: state, Err: fmt.Errorf("cannot send message to suspended session: %s", e.describeSessionBlocker(cmd.RawID, domain.SessionStateSuspended))}

	default:
		return SessionStartResult{Session: sess, State: state, Err: fmt.Errorf("invalid session state: %v", state)}
	}
}

func (e *AgentExecutor) isSessionStartDraining() bool {
	return e.sessionRuns != nil && e.sessionRuns.LifecycleMode() != entity.ActiveStoreModeRunning
}

func (e *AgentExecutor) deferAndNotifySessionStart(cmd SessionStartCommand, message string) error {
	if err := e.deferSessionStartCommand(cmd); err != nil {
		return err
	}
	if strings.TrimSpace(message) != "" {
		e.emitSynthesized(cmd.Session, domain.NewSystemMessageEvent(cmd.RawID, message))
	}
	return nil
}
