package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	dockMCPKindList      = "list"
	dockMCPKindDispatch  = "dispatch"
	dockMCPKindMultiEdit = "multi_edit"
)

func (h *Handler) requireDockSession(id string) (*domain.SessionSnapshot, bool) {
	sess, err := h.executor.GetSession(id)
	if err != nil {
		return nil, false
	}
	snap := sess.Snapshot()
	if snap.Kind != domain.SessionKindDock {
		return nil, false
	}
	return &snap, true
}

func (h *Handler) nextDockMCP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.requireDockSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found", "")
		return
	}

	timeout := 25 * time.Second
	if raw := r.URL.Query().Get("timeout_ms"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeout = time.Duration(parsed) * time.Millisecond
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	req, err := h.dockBridge.Next(ctx, id)
	if err != nil {
		if errors.Is(err, ErrDockRequestEmpty) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read dock request", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(req)
}

func (h *Handler) requestDockMCP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.requireDockSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found", "")
		return
	}

	var req apiTypes.DockMCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "dock request kind is required", "")
		return
	}
	switch req.Kind {
	case dockMCPKindList, dockMCPKindDispatch, dockMCPKindMultiEdit:
		// ok
	default:
		writeError(w, http.StatusBadRequest, "invalid dock request kind", "")
		return
	}
	req.ID = generateID()

	resp, err := h.dockBridge.Enqueue(r.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrDockQueueFull):
			writeError(w, http.StatusTooManyRequests, "dock queue full", err.Error())
		case errors.Is(err, ErrDockTimeout):
			writeError(w, http.StatusGatewayTimeout, "dock request timed out", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "dock request failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) respondDockMCP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.requireDockSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found", "")
		return
	}

	var resp apiTypes.DockMCPResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if strings.TrimSpace(resp.ID) == "" {
		writeError(w, http.StatusBadRequest, "dock response id is required", "")
		return
	}

	if err := h.dockBridge.Respond(id, resp); err != nil {
		if errors.Is(err, ErrDockRequestGone) {
			writeError(w, http.StatusNotFound, "dock request not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to deliver dock response", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
