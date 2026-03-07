package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) queryCodeflowGraph(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.CodeflowQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required", "")
		return
	}

	dbPath := filepath.Join(h.gitDir, "backend", ".codeflow-mvp.goraphdb")

	opts := codeflowmvp.QueryOptions{
		DBPath: dbPath,
	}

	result, err := codeflowmvp.QueryGraph(req.Query, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query graph", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}
