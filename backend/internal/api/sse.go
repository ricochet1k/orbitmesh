package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// sseEvents streams domain events for a session as Server-Sent Events.
// The subscription is registered before headers are flushed so that no
// events are lost between the client seeing the 200 and the first broadcast.
func (h *Handler) sseEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	if _, err := h.executor.GetSession(sessionID); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up session", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported", "")
		return
	}

	// Subscribe before writing headers — guarantees the subscription is
	// active by the time the client receives the 200 response.
	subID := generateID()
	sub := h.broadcaster.Subscribe(subID, sessionID)
	defer h.broadcaster.Unsubscribe(subID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent serialises a single domain event in the SSE wire format:
//
//	event: <type>\n
//	data: <json>\n
//	\n
func writeSSEEvent(w http.ResponseWriter, event domain.Event) error {
	apiEvent := domainEventToAPIEvent(event)
	data, err := json.Marshal(apiEvent)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", apiEvent.Type, data)
	return err
}

func domainEventToAPIEvent(e domain.Event) apiTypes.Event {
	return apiTypes.Event{
		Type:      apiTypes.EventType(e.Type.String()),
		Timestamp: e.Timestamp,
		SessionID: e.SessionID,
		Data:      convertEventData(e),
	}
}

func convertEventData(e domain.Event) any {
	switch d := e.Data.(type) {
	case domain.StatusChangeData:
		return apiTypes.StatusChangeData{OldState: d.OldState, NewState: d.NewState, Reason: d.Reason}
	case domain.OutputData:
		return apiTypes.OutputData{Content: d.Content}
	case domain.MetricData:
		return apiTypes.MetricData{TokensIn: d.TokensIn, TokensOut: d.TokensOut, RequestCount: d.RequestCount}
	case domain.ErrorData:
		return apiTypes.ErrorData{Message: d.Message, Code: d.Code}
	case domain.MetadataData:
		return apiTypes.MetadataData{Key: d.Key, Value: d.Value}
	default:
		return d
	}
}
