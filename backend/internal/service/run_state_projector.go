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
			sc.session.AppendOutputDelta(data.Content)
		} else {
			sc.session.AppendMessageRaw(domain.MessageKindOutput, data.Content, event.Raw)
		}
	case domain.ThoughtData:
		sc.session.AppendMessageRaw(domain.MessageKindThought, data.Content, event.Raw)
	case domain.ErrorData:
		sc.session.AppendMessageRaw(domain.MessageKindError, data.Message, event.Raw)
	case domain.ToolCallData:
		sc.session.AppendMessageRaw(domain.MessageKindToolUse, fmt.Sprintf("%s: %s", data.Name, data.ID), event.Raw)
		if data.Status == "pending" || data.Status == "waiting" {
			e.suspendSession(sc, data.ID)
		}
	case domain.MetadataData:
		if data.Key == "current_task" {
			if task, ok := data.Value.(string); ok {
				sc.session.SetCurrentTask(task)
			}
		}
		sc.session.AppendMessageRaw(domain.MessageKindSystem, data.Key, event.Raw)
	case domain.MetricData:
		sc.session.AppendMessageRaw(domain.MessageKindMetric,
			fmt.Sprintf("in=%d out=%d requests=%d", data.TokensIn, data.TokensOut, data.RequestCount), event.Raw)
	case domain.StatusChangeData:
		sc.session.AppendMessageRaw(domain.MessageKindSystem,
			fmt.Sprintf("status: %s -> %s", data.OldState, data.NewState), event.Raw)
	case domain.PlanData:
		steps := make([]string, 0, len(data.Steps))
		for _, step := range data.Steps {
			steps = append(steps, fmt.Sprintf("%s: %s", step.ID, step.Description))
		}
		content := data.Description
		if len(steps) > 0 {
			content = fmt.Sprintf("%s\n%s", data.Description, strings.Join(steps, "\n"))
		}
		sc.session.AppendMessageRaw(domain.MessageKindPlan, content, event.Raw)
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
	e.touchRunAttempt(sc)
}
