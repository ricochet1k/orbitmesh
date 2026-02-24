package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ricochet1k/orbitmesh/internal/toolcall"
)

// ErrEvalNotFound is returned when a requested eval does not exist.
var ErrEvalNotFound = errors.New("eval not found")

// EvalStorage persists and retrieves tool call Evals.
//
// Evals are stored in two locations:
//   - <baseDir>/evals/<evalID>.json          — for fast O(1) lookup by evalID
//   - <baseDir>/sessions/<sessionID>/evals/<evalID>.json — for session-scoped listing
type EvalStorage interface {
	SaveEval(e *toolcall.Eval) error
	LoadEval(evalID string) (*toolcall.Eval, error)
	ListEvalsForSession(sessionID string) ([]*toolcall.Eval, error)
	DeleteEval(evalID string) error
}

// evalByIDPath returns the flat index path for the given evalID.
func (s *JSONFileStorage) evalByIDPath(evalID string) string {
	return filepath.Join(s.baseDir, "evals", evalID+".json")
}

// evalByIDDir returns the directory that holds the flat eval index.
func (s *JSONFileStorage) evalByIDDir() string {
	return filepath.Join(s.baseDir, "evals")
}

// evalSessionPath returns the session-scoped path for an eval.
func (s *JSONFileStorage) evalSessionPath(sessionID, evalID string) string {
	return filepath.Join(s.baseDir, "sessions", sessionID, "evals", evalID+".json")
}

// evalSessionDir returns the session-scoped evals directory.
func (s *JSONFileStorage) evalSessionDir(sessionID string) string {
	return filepath.Join(s.baseDir, "sessions", sessionID, "evals")
}

// SaveEval persists e to both index locations.
// It creates all necessary directories and uses an atomic rename for each write.
func (s *JSONFileStorage) SaveEval(e *toolcall.Eval) error {
	if e == nil {
		return fmt.Errorf("eval is nil")
	}
	if err := validateSessionID(e.ID); err != nil {
		return fmt.Errorf("invalid eval ID: %w", err)
	}
	if err := validateSessionID(e.SessionID); err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	jsonData, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal eval: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Write to both paths.
	if err := s.atomicWriteFile(s.evalByIDDir(), s.evalByIDPath(e.ID), e.ID, jsonData); err != nil {
		return err
	}
	if err := s.atomicWriteFile(s.evalSessionDir(e.SessionID), s.evalSessionPath(e.SessionID, e.ID), e.ID, jsonData); err != nil {
		return err
	}
	return nil
}

// atomicWriteFile creates dir if needed and atomically writes data to destPath.
// prefix is used to name the temp file.
func (s *JSONFileStorage) atomicWriteFile(dir, destPath, prefix string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, prefix+".*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	tmpName := f.Name()
	_ = os.Chmod(tmpName, 0o600)

	defer func() {
		if f != nil {
			f.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	if err := f.Close(); err != nil {
		f = nil
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	f = nil

	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	df, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	defer df.Close()
	if err := df.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	return nil
}

// LoadEval reads the eval from the flat index (<baseDir>/evals/<evalID>.json).
func (s *JSONFileStorage) LoadEval(evalID string) (*toolcall.Eval, error) {
	if err := validateSessionID(evalID); err != nil {
		return nil, fmt.Errorf("invalid eval ID: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.evalByIDPath(evalID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrEvalNotFound
		}
		return nil, fmt.Errorf("failed to read eval %s: %w", evalID, err)
	}

	var out toolcall.Eval
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal eval %s: %w", evalID, err)
	}
	return &out, nil
}

// ListEvalsForSession returns all evals stored under the session-scoped directory.
func (s *JSONFileStorage) ListEvalsForSession(sessionID string) ([]*toolcall.Eval, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.evalSessionDir(sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*toolcall.Eval{}, nil
		}
		return nil, fmt.Errorf("failed to read evals directory for session %s: %w", sessionID, err)
	}

	var evals []*toolcall.Eval
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read eval file %s: %w", entry.Name(), err)
		}

		var e toolcall.Eval
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("failed to unmarshal eval file %s: %w", entry.Name(), err)
		}
		evals = append(evals, &e)
	}

	return evals, nil
}

// DeleteEval removes the eval from both storage locations.
// Returns ErrEvalNotFound if neither location contains the eval.
func (s *JSONFileStorage) DeleteEval(evalID string) error {
	if err := validateSessionID(evalID); err != nil {
		return fmt.Errorf("invalid eval ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load from the flat index to discover the sessionID for the session-scoped path.
	data, err := os.ReadFile(s.evalByIDPath(evalID))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrEvalNotFound
		}
		return fmt.Errorf("failed to read eval %s: %w", evalID, err)
	}

	var e toolcall.Eval
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("failed to unmarshal eval %s: %w", evalID, err)
	}

	// Remove flat index entry.
	if err := os.Remove(s.evalByIDPath(evalID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete eval index entry %s: %w", evalID, err)
	}

	// Remove session-scoped entry (best-effort if sessionID is known).
	if e.SessionID != "" {
		if err := os.Remove(s.evalSessionPath(e.SessionID, evalID)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete session eval entry %s/%s: %w", e.SessionID, evalID, err)
		}
	}

	return nil
}
