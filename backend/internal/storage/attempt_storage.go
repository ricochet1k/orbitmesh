package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var ErrRunAttemptNotFound = errors.New("run attempt not found")

type RunAttemptStorage interface {
	SaveRunAttempt(attempt *RunAttemptMetadata) error
	LoadRunAttempt(sessionID, attemptID string) (*RunAttemptMetadata, error)
	ListRunAttempts(sessionID string) ([]*RunAttemptMetadata, error)
}

type RunAttemptMetadata struct {
	AttemptID          string     `json:"attempt_id"`
	SessionID          string     `json:"session_id"`
	ProviderType       string     `json:"provider_type"`
	ProviderID         string     `json:"provider_id,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	TerminalReason     string     `json:"terminal_reason,omitempty"`
	InterruptionReason string     `json:"interruption_reason,omitempty"`
	WaitKind           string     `json:"wait_kind,omitempty"`
	WaitRef            string     `json:"wait_ref,omitempty"`
	ResumeTokenID      string     `json:"resume_token_id,omitempty"`
	HeartbeatAt        time.Time  `json:"heartbeat_at"`
	BootID             string     `json:"boot_id,omitempty"`
}

func (s *JSONFileStorage) attemptsSessionDir(sessionID string) string {
	return filepath.Join(s.baseDir, "sessions", "attempts", sessionID)
}

func (s *JSONFileStorage) runAttemptPath(sessionID, attemptID string) string {
	return filepath.Join(s.attemptsSessionDir(sessionID), attemptID+".json")
}

func validateRunAttemptID(id string) error {
	if !sessionIDRegex.MatchString(id) {
		return fmt.Errorf("%w: %s", ErrInvalidSessionID, id)
	}
	return nil
}

func (s *JSONFileStorage) SaveRunAttempt(attempt *RunAttemptMetadata) error {
	if attempt == nil {
		return fmt.Errorf("attempt metadata is nil")
	}
	if err := validateSessionID(attempt.SessionID); err != nil {
		return err
	}
	if err := validateRunAttemptID(attempt.AttemptID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	attemptDir := s.attemptsSessionDir(attempt.SessionID)
	if err := os.MkdirAll(attemptDir, 0o700); err != nil {
		return fmt.Errorf("failed to create run-attempt directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(attempt, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run attempt: %w", err)
	}

	f, err := os.CreateTemp(attemptDir, attempt.AttemptID+".*.tmp")
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

	if _, err := f.Write(jsonData); err != nil {
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

	if err := os.Rename(tmpName, s.runAttemptPath(attempt.SessionID, attempt.AttemptID)); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	df, err := os.Open(attemptDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	defer df.Close()
	if err := df.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	return nil
}

func (s *JSONFileStorage) LoadRunAttempt(sessionID, attemptID string) (*RunAttemptMetadata, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	if err := validateRunAttemptID(attemptID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.runAttemptPath(sessionID, attemptID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRunAttemptNotFound
		}
		return nil, err
	}

	var out RunAttemptMetadata
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *JSONFileStorage) ListRunAttempts(sessionID string) ([]*RunAttemptMetadata, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	attemptDir := s.attemptsSessionDir(sessionID)
	entries, err := os.ReadDir(attemptDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RunAttemptMetadata{}, nil
		}
		return nil, err
	}

	attempts := make([]*RunAttemptMetadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		attemptID := entry.Name()[:len(entry.Name())-5]
		if err := validateRunAttemptID(attemptID); err != nil {
			continue
		}

		data, err := os.ReadFile(s.runAttemptPath(sessionID, attemptID))
		if err != nil {
			continue
		}
		var item RunAttemptMetadata
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		attempts = append(attempts, &item)
	}

	sort.Slice(attempts, func(i, j int) bool {
		if attempts[i].StartedAt.Equal(attempts[j].StartedAt) {
			return attempts[i].AttemptID < attempts[j].AttemptID
		}
		return attempts[i].StartedAt.Before(attempts[j].StartedAt)
	})

	return attempts, nil
}
