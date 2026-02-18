package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type projectData struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectStorage manages project configurations.
type ProjectStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewProjectStorage creates a new project storage.
func NewProjectStorage(baseDir string) *ProjectStorage {
	return &ProjectStorage{baseDir: baseDir}
}

func (s *ProjectStorage) configPath() string {
	return filepath.Join(s.baseDir, "projects.json")
}

// List returns all projects.
func (s *ProjectStorage) List() ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listUnlocked()
}

// Get returns a project by ID.
func (s *ProjectStorage) Get(id string) (*domain.Project, error) {
	projects, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("project not found: %s", id)
}

// Save creates or updates a project.
func (s *ProjectStorage) Save(p domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projects, err := s.listUnlocked()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range projects {
		if existing.ID == p.ID {
			projects[i] = p
			found = true
			break
		}
	}
	if !found {
		projects = append(projects, p)
	}

	return s.writeUnlocked(projects)
}

// Delete removes a project by ID.
func (s *ProjectStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projects, err := s.listUnlocked()
	if err != nil {
		return err
	}

	out := make([]domain.Project, 0, len(projects))
	found := false
	for _, p := range projects {
		if p.ID != id {
			out = append(out, p)
		} else {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("project not found: %s", id)
	}

	return s.writeUnlocked(out)
}

func (s *ProjectStorage) listUnlocked() ([]domain.Project, error) {
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.Project{}, nil
		}
		return nil, fmt.Errorf("failed to read projects config: %w", err)
	}

	var raw []projectData
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse projects config: %w", err)
	}

	projects := make([]domain.Project, len(raw))
	for i, r := range raw {
		projects[i] = domain.Project{
			ID:        r.ID,
			Name:      r.Name,
			Path:      r.Path,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return projects, nil
}

func (s *ProjectStorage) writeUnlocked(projects []domain.Project) error {
	raw := make([]projectData, len(projects))
	for i, p := range projects {
		raw[i] = projectData{
			ID:        p.ID,
			Name:      p.Name,
			Path:      p.Path,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
		}
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal projects: %w", err)
	}

	filePath := s.configPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write projects config: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename projects config: %w", err)
	}

	return nil
}
