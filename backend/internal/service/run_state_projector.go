package service

import (
	"fmt"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func (e *AgentExecutor) updateSessionFromEvent(sc *sessionContext, event domain.Event) {
	switch data := event.Data.(type) {
	case domain.OutputData:
		if data.IsDelta {
			e.appendOutputDelta(sc.session, data.Content, event.Raw, event.Timestamp)
		} else {
			e.appendSessionMessageRaw(sc.session, domain.MessageKindOutput, data.Content, event.Raw, event.Timestamp)
		}
	case domain.ThoughtData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindThought, data.Content, event.Raw, event.Timestamp)
	case domain.ErrorData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindError, data.Message, event.Raw, event.Timestamp)
	case domain.ToolCallData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindToolUse, fmt.Sprintf("%s: %s", data.Name, data.ID), event.Raw, event.Timestamp)
		if data.Status == "pending" || data.Status == "waiting" {
			e.suspendSession(sc, data.ID)
		}
	case domain.MetadataData:
		if data.Key == "current_task" {
			if task, ok := data.Value.(string); ok {
				sc.session.SetCurrentTask(task)
			}
		}
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, data.Key, event.Raw, event.Timestamp)
	case domain.MetricData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindMetric,
			fmt.Sprintf("in=%d out=%d requests=%d", data.TokensIn, data.TokensOut, data.RequestCount), event.Raw, event.Timestamp)
	case domain.StatusChangeData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem,
			fmt.Sprintf("status: %s -> %s", data.OldState, data.NewState), event.Raw, event.Timestamp)
	case domain.PlanData:
		steps := make([]string, 0, len(data.Steps))
		for _, step := range data.Steps {
			steps = append(steps, fmt.Sprintf("%s: %s", step.ID, step.Description))
		}
		content := data.Description
		if len(steps) > 0 {
			content = fmt.Sprintf("%s\n%s", data.Description, strings.Join(steps, "\n"))
		}
		e.appendSessionMessageRaw(sc.session, domain.MessageKindPlan, content, event.Raw, event.Timestamp)
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
	e.touchRunAttempt(sc)
}
