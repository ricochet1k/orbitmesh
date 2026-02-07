package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const sseHeartbeatInterval = 15 * time.Second

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

	lastEventID := parseLastEventID(r)

	// Subscribe before writing headers — guarantees the subscription is
	// active by the time the client receives the 200 response.
	subID := generateID()
	sub, replay := h.broadcaster.SubscribeWithReplay(subID, sessionID, lastEventID)
	defer h.broadcaster.Unsubscribe(subID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for _, event := range replay {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
		flusher.Flush()
	}

	ctx := r.Context()
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()
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
		case <-heartbeat.C:
			if err := writeSSEHeartbeat(w, time.Now()); err != nil {
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
	if event.ID > 0 {
		_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, apiEvent.Type, data)
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", apiEvent.Type, data)
	return err
}

func writeSSEHeartbeat(w http.ResponseWriter, timestamp time.Time) error {
	data, err := json.Marshal(map[string]string{
		"timestamp": timestamp.Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data)
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
		return apiTypes.StatusChangeData{
			OldState: d.OldState.String(),
			NewState: d.NewState.String(),
			Reason:   d.Reason,
		}
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

func parseLastEventID(r *http.Request) int64 {
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		if id, err := strconv.ParseInt(header, 10, 64); err == nil {
			return id
		}
	}
	if query := r.URL.Query().Get("last_event_id"); query != "" {
		if id, err := strconv.ParseInt(query, 10, 64); err == nil {
			return id
		}
	}
	return 0
}
