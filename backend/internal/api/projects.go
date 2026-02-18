package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projectStorage.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects", err.Error())
		return
	}

	responses := make([]apiTypes.ProjectResponse, len(projects))
	for i, p := range projects {
		responses[i] = projectToResponse(p)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.ProjectListResponse{Projects: responses})
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p, err := h.projectStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(projectToResponse(*p))
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.ProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Path = strings.TrimSpace(req.Path)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required", "")
		return
	}

	if info, err := os.Stat(req.Path); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "path does not exist or is not a directory", "")
		return
	}

	now := time.Now()
	p := domain.Project{
		ID:        generateProjectID(),
		Name:      req.Name,
		Path:      req.Path,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.projectStorage.Save(p); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save project", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(projectToResponse(p))
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.projectStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	var req apiTypes.ProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Path = strings.TrimSpace(req.Path)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required", "")
		return
	}

	if info, err := os.Stat(req.Path); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "path does not exist or is not a directory", "")
		return
	}

	p := domain.Project{
		ID:        id,
		Name:      req.Name,
		Path:      req.Path,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: time.Now(),
	}

	if err := h.projectStorage.Save(p); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(projectToResponse(p))
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.projectStorage.Get(id); err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	// Cascade: stop and delete all sessions belonging to this project.
	if err := h.executor.DeleteProjectSessions(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project sessions", err.Error())
		return
	}

	if err := h.projectStorage.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func generateProjectID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "proj_" + hex.EncodeToString(b[:])
}

func projectToResponse(p domain.Project) apiTypes.ProjectResponse {
	return apiTypes.ProjectResponse{
		ID:        p.ID,
		Name:      p.Name,
		Path:      p.Path,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}
