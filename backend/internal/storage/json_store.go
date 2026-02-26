package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ricochet1k/orbitmesh/internal/entity"
)

// JSONStore[S] is a generic, filesystem-backed implementation of entity.TypedStorage[S].
// Each entity is stored as a single JSON file: <dir>/<id>.json.
// Writes are atomic (temp → fsync → rename → dir fsync).
// S must be JSON-serialisable and have a stable string ID extractable by idOf.
type JSONStore[S any] struct {
	dir  string         // absolute directory path, e.g. /home/user/.orbitmesh/evals
	idOf func(S) string // extracts the stable entity ID from a snapshot
	mu   sync.RWMutex
}

// NewJSONStore creates a JSONStore that stores snapshots under dir.
// idOf must return the same value as the ID used in Load/Delete calls.
// The directory is created lazily on first Save.
func NewJSONStore[S any](dir string, idOf func(S) string) *JSONStore[S] {
	return &JSONStore[S]{
		dir:  dir,
		idOf: idOf,
	}
}

// Save marshals s to indented JSON and atomically writes it to <dir>/<id>.json.
func (js *JSONStore[S]) Save(s S) error {
	id := js.idOf(s)
	if err := validateSessionID(id); err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal entity %s: %w", id, err)
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	destPath := filepath.Join(js.dir, id+".json")
	return atomicWriteFileAt(js.dir, destPath, id, data)
}

// Load reads and unmarshals the entity with the given ID.
// Returns entity.ErrNotFound if no file exists for that ID.
func (js *JSONStore[S]) Load(id string) (S, error) {
	var zero S
	if err := validateSessionID(id); err != nil {
		return zero, fmt.Errorf("invalid entity ID: %w", err)
	}

	js.mu.RLock()
	defer js.mu.RUnlock()

	filePath := filepath.Join(js.dir, id+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, entity.ErrNotFound
		}
		return zero, fmt.Errorf("failed to read entity %s: %w", id, err)
	}

	var s S
	if err := json.Unmarshal(data, &s); err != nil {
		return zero, fmt.Errorf("failed to unmarshal entity %s: %w", id, err)
	}
	return s, nil
}

// Delete removes the file for the given ID.
// Returns entity.ErrNotFound if the file does not exist.
func (js *JSONStore[S]) Delete(id string) error {
	if err := validateSessionID(id); err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	filePath := filepath.Join(js.dir, id+".json")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return entity.ErrNotFound
		}
		return fmt.Errorf("failed to delete entity %s: %w", id, err)
	}
	return nil
}

// List reads every .json file in the directory and returns the unmarshalled snapshots.
// If the directory does not exist, an empty slice is returned with no error.
func (js *JSONStore[S]) List() ([]S, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	entries, err := os.ReadDir(js.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []S{}, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", js.dir, err)
	}

	var results []S
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(js.dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read entity file %s: %w", entry.Name(), err)
		}

		var s S
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal entity file %s: %w", entry.Name(), err)
		}
		results = append(results, s)
	}

	if results == nil {
		return []S{}, nil
	}
	return results, nil
}

// ListIDs scans the directory for .json files and returns StoredMeta (ID +
// timestamps) for each valid entry. Files with invalid IDs are silently skipped.
// If the directory does not exist, an empty slice is returned with no error.
func (js *JSONStore[S]) ListIDs() ([]entity.StoredMeta, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	entries, err := os.ReadDir(js.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []entity.StoredMeta{}, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", js.dir, err)
	}

	metas := make([]entity.StoredMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if err := validateSessionID(id); err != nil {
			// Skip files with invalid names (e.g. temp files).
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // file disappeared between ReadDir and Info
		}
		modTime := info.ModTime()
		birthTime := fileBirthTime(info) // platform-specific helper
		metas = append(metas, entity.StoredMeta{
			ID:         id,
			CreatedAt:  birthTime,
			ModifiedAt: modTime,
		})
	}
	return metas, nil
}
