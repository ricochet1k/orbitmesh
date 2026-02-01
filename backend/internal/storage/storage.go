package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrStorageWrite    = errors.New("failed to write session")
)

type Storage interface {
	Save(session *domain.Session) error
	Load(id string) (*domain.Session, error)
	Delete(id string) error
	List() ([]*domain.Session, error)
}

type sessionData struct {
	ID           string           `json:"id"`
	ProviderType string           `json:"provider_type"`
	State        string           `json:"state"`
	WorkingDir   string           `json:"working_dir"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	CurrentTask  string           `json:"current_task,omitempty"`
	Output       string           `json:"output,omitempty"`
	ErrorMessage string           `json:"error_message,omitempty"`
	Transitions  []transitionData `json:"transitions"`
}

type transitionData struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

type JSONFileStorage struct {
	baseDir string
	mu      sync.RWMutex
}

func NewJSONFileStorage(baseDir string) (*JSONFileStorage, error) {
	sessionsDir := filepath.Join(baseDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	return &JSONFileStorage{
		baseDir: baseDir,
	}, nil
}

func DefaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".orbitmesh"
	}
	return filepath.Join(home, ".orbitmesh")
}

func (s *JSONFileStorage) sessionPath(id string) string {
	return filepath.Join(s.baseDir, "sessions", id+".json")
}

func (s *JSONFileStorage) Save(session *domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := sessionToData(session)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	filePath := s.sessionPath(session.ID)
	tmpPath := filePath + ".tmp"

	if err := os.WriteFile(tmpPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	return nil
}

func (s *JSONFileStorage) Load(id string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filePath := s.sessionPath(id)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var sd sessionData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return dataToSession(&sd), nil
}

func (s *JSONFileStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := s.sessionPath(id)
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	return nil
}

func (s *JSONFileStorage) List() ([]*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionsDir := filepath.Join(s.baseDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.Session{}, nil
		}
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	sessions := make([]*domain.Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5]
		session, err := s.loadUnlocked(id)
		if err != nil {
			continue
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (s *JSONFileStorage) loadUnlocked(id string) (*domain.Session, error) {
	filePath := s.sessionPath(id)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var sd sessionData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, err
	}

	return dataToSession(&sd), nil
}

func sessionToData(session *domain.Session) *sessionData {
	transitions := make([]transitionData, len(session.Transitions))
	for i, t := range session.Transitions {
		transitions[i] = transitionData{
			From:      t.From.String(),
			To:        t.To.String(),
			Reason:    t.Reason,
			Timestamp: t.Timestamp,
		}
	}

	return &sessionData{
		ID:           session.ID,
		ProviderType: session.ProviderType,
		State:        session.State.String(),
		WorkingDir:   session.WorkingDir,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
		CurrentTask:  session.CurrentTask,
		Output:       session.Output,
		ErrorMessage: session.ErrorMessage,
		Transitions:  transitions,
	}
}

func dataToSession(data *sessionData) *domain.Session {
	state := parseSessionState(data.State)

	transitions := make([]domain.StateTransition, len(data.Transitions))
	for i, t := range data.Transitions {
		transitions[i] = domain.StateTransition{
			From:      parseSessionState(t.From),
			To:        parseSessionState(t.To),
			Reason:    t.Reason,
			Timestamp: t.Timestamp,
		}
	}

	return &domain.Session{
		ID:           data.ID,
		ProviderType: data.ProviderType,
		State:        state,
		WorkingDir:   data.WorkingDir,
		CreatedAt:    data.CreatedAt,
		UpdatedAt:    data.UpdatedAt,
		CurrentTask:  data.CurrentTask,
		Output:       data.Output,
		ErrorMessage: data.ErrorMessage,
		Transitions:  transitions,
	}
}

func parseSessionState(s string) domain.SessionState {
	switch s {
	case "created":
		return domain.SessionStateCreated
	case "starting":
		return domain.SessionStateStarting
	case "running":
		return domain.SessionStateRunning
	case "paused":
		return domain.SessionStatePaused
	case "stopping":
		return domain.SessionStateStopping
	case "stopped":
		return domain.SessionStateStopped
	case "error":
		return domain.SessionStateError
	default:
		return domain.SessionStateCreated
	}
}
