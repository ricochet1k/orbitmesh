package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestReadProjectFile_TreatsKnownTextExtensionAsText(t *testing.T) {
	env := newTestEnv(t)

	projectStorage := storage.NewProjectStorage(t.TempDir())
	env.handler.projectStorage = projectStorage

	projectRoot := t.TempDir()
	project := domain.Project{
		ID:        "project-1",
		Name:      "Project One",
		Path:      projectRoot,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := projectStorage.Save(project); err != nil {
		t.Fatalf("failed to save project: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectRoot, ".env"), []byte("API_KEY=test\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/files/.env", project.ID), nil)
	w := httptest.NewRecorder()
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.FileReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Encoding != "utf8" {
		t.Fatalf("expected utf8 encoding, got %q", resp.Encoding)
	}
	if resp.Content != "API_KEY=test\n" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
}
