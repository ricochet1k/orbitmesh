package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) getTerminalSnapshotByID(w http.ResponseWriter, r *http.Request) {
	terminalID := chi.URLParam(r, "id")
	if terminalID == "" {
		writeError(w, http.StatusBadRequest, "terminal id is required", "")
		return
	}

	terminalKnown := false
	term, err := h.executor.GetTerminal(terminalID)
	if err == nil {
		terminalKnown = true
		if term.LastSnapshot != nil {
			resp := apiTypes.TerminalSnapshot{Rows: term.LastSnapshot.Rows, Cols: term.LastSnapshot.Cols, Lines: term.LastSnapshot.Lines}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	} else if !errors.Is(err, storage.ErrTerminalNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to get terminal", err.Error())
		return
	}

	snapshot, err := h.executor.TerminalSnapshot(terminalID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTerminalNotSupported):
			writeError(w, http.StatusBadRequest, "terminal snapshot not supported", "")
		case errors.Is(err, service.ErrSessionNotFound):
			if terminalKnown {
				writeError(w, http.StatusNotFound, "terminal snapshot not available", "")
			} else {
				writeError(w, http.StatusNotFound, "terminal not found", "")
			}
		default:
			writeError(w, http.StatusInternalServerError, "failed to get terminal snapshot", err.Error())
		}
		return
	}

	resp := apiTypes.TerminalSnapshot{Rows: snapshot.Rows, Cols: snapshot.Cols, Lines: snapshot.Lines}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
