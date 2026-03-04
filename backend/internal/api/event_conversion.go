package api

import (
	"net/http"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func domainEventToAPIEvent(e domain.Event, includeRaw bool) apiTypes.Event {
	apiEvent := apiTypes.Event{
		EventID:   e.ID,
		Type:      apiTypes.EventType(e.Type.String()),
		Timestamp: e.Timestamp,
		SessionID: e.SessionID,
		Data:      convertEventData(e),
	}
	if includeRaw {
		apiEvent.Raw = e.Raw
	}
	return apiEvent
}

func includeRawRequested(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("include_raw") == "1" || q.Get("include_raw") == "true"
}

func convertEventData(e domain.Event) any {
	switch d := e.Data.(type) {
	case domain.StatusChangeData:
		return apiTypes.StatusChangeData{
			OldState: d.OldState.String(),
			NewState: d.NewState.String(),
			Reason:   d.Reason,
		}
	case domain.OutputData:
		return apiTypes.OutputData{Content: d.Content, IsDelta: d.IsDelta, MessageID: d.MessageID}
	case domain.MetricData:
		return apiTypes.MetricData{TokensIn: d.TokensIn, TokensOut: d.TokensOut, RequestCount: d.RequestCount}
	case domain.ErrorData:
		return apiTypes.ErrorData{Message: d.Message, Code: d.Code}
	case domain.MetadataData:
		return apiTypes.MetadataData{Key: d.Key, Value: d.Value}
	case domain.UnknownData:
		return apiTypes.UnknownData{Source: d.Source, Summary: d.Summary, Payload: d.Payload}
	case domain.ToolCallData:
		return apiTypes.ToolCallData{
			ID:     d.ID,
			Name:   d.Name,
			Status: d.Status,
			Title:  d.Title,
			Input:  d.Input,
			Output: d.Output,
		}
	case domain.ThoughtData:
		return apiTypes.ThoughtData{Content: d.Content, IsDelta: d.IsDelta, MessageID: d.MessageID}
	case domain.UserMessageData:
		return apiTypes.UserMessageData{Content: d.Content}
	case domain.SystemMessageData:
		return apiTypes.SystemMessageData{Content: d.Content}
	case domain.PlanData:
		steps := make([]apiTypes.PlanStep, len(d.Steps))
		for i, s := range d.Steps {
			steps[i] = apiTypes.PlanStep{ID: s.ID, Description: s.Description, Status: s.Status}
		}
		return apiTypes.PlanData{Description: d.Description, Steps: steps}
	case domain.ProgressData:
		return apiTypes.ProgressData{
			Channel:  d.Channel,
			StreamID: d.StreamID,
			Content:  d.Content,
			IsDelta:  d.IsDelta,
			Done:     d.Done,
			Status:   d.Status,
		}
	case domain.ResourceUsageData:
		return apiTypes.ResourceUsageData{Scope: d.Scope, Data: d.Data, Metadata: d.Metadata}
	case domain.ActionRequestData:
		return apiTypes.ActionRequestData{
			ID:      d.ID,
			Kind:    d.Kind,
			Title:   d.Title,
			Status:  d.Status,
			Payload: d.Payload,
		}
	case domain.ArtifactUpdateData:
		return apiTypes.ArtifactUpdateData{
			ID:      d.ID,
			Kind:    d.Kind,
			Title:   d.Title,
			IsDelta: d.IsDelta,
			Payload: d.Payload,
		}
	default:
		return d
	}
}
