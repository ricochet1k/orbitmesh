package sample

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type apiHandler struct{}

func (h *apiHandler) Mount(r chi.Router) {
	r.Get("/api/sessions/{id}", h.getSession)
	r.Post("/api/sessions", h.createSession)
}

func (h *apiHandler) getSession(http.ResponseWriter, *http.Request) {}

func (h *apiHandler) createSession(http.ResponseWriter, *http.Request) {}
