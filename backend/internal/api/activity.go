package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const defaultActivityLimit = 100
const maxActivityLimit = 500

func (h *Handler) getSessionActivity(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required", "")
		return
	}

	if _, err := h.executor.GetSession(sessionID); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up session", err.Error())
		return
	}

	limit, err := parseActivityLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}

	cursor, err := parseActivityCursor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor", err.Error())
		return
	}

	entries, err := loadActivityEntries(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity", err.Error())
		return
	}

	page, nextCursor := paginateActivity(entries, limit, cursor)
	resp := apiTypes.ActivityHistoryResponse{Entries: page, NextCursor: nextCursor}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseActivityLimit(r *http.Request) (int, error) {
	limit := defaultActivityLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, err
		}
		limit = value
	}
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	if limit > maxActivityLimit {
		limit = maxActivityLimit
	}
	return limit, nil
}

func parseActivityCursor(r *http.Request) (*int, error) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, err
	}
	if value < 0 {
		return nil, errors.New("cursor must be non-negative")
	}
	return &value, nil
}

func paginateActivity(entries []apiTypes.ActivityEntry, limit int, cursor *int) ([]apiTypes.ActivityEntry, *string) {
	total := len(entries)
	end := total
	if cursor != nil && *cursor < total {
		end = *cursor
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	page := entries[start:end]
	if start == 0 {
		return page, nil
	}
	next := strconv.Itoa(start)
	return page, &next
}

func loadActivityEntries(sessionID string) ([]apiTypes.ActivityEntry, error) {
	path := pty.ActivityLogPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []apiTypes.ActivityEntry{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []apiTypes.ActivityEntry{}, nil
	}

	entries := []apiTypes.ActivityEntry{}
	reader := bufio.NewScanner(bytes.NewReader(data))
	reader.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var record pty.ActivityRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		if record.Entry != nil {
			entries = append(entries, toAPIActivityEntry(*record.Entry))
			continue
		}
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].TS.Before(entries[j].TS)
	})
	return entries, nil
}

func toAPIActivityEntry(entry pty.ActivityEntry) apiTypes.ActivityEntry {
	return apiTypes.ActivityEntry{
		ID:        entry.ID,
		SessionID: entry.SessionID,
		Kind:      entry.Kind,
		TS:        entry.TS,
		Rev:       entry.Rev,
		Open:      entry.Open,
		Data:      entry.Data,
	}
}
