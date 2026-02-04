package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

var (
	ErrSessionNotFound     = errors.New("session not found")
	ErrStorageWrite        = errors.New("failed to write session")
	ErrInvalidSessionID    = errors.New("invalid session id")
	ErrSessionFileTooLarge = errors.New("session file too large")
	ErrSymlinkNotAllowed   = errors.New("symlinks not allowed for session files")
)

const maxSessionFileSize = 10 * 1024 * 1024 // 10MB

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

var (
	ErrInvalidSessionState = errors.New("invalid session state")
	sessionIDRegex         = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func validateSessionID(id string) error {
	if !sessionIDRegex.MatchString(id) {
		return fmt.Errorf("%w: %s", ErrInvalidSessionID, id)
	}
	return nil
}

func NewJSONFileStorage(baseDir string) (*JSONFileStorage, error) {
	sessionsDir := filepath.Join(baseDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	// Verify permissions if it already existed
	info, err := os.Stat(sessionsDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			// Directory is too permissive, try to fix it
			_ = os.Chmod(sessionsDir, 0o700)
		}
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
	if err := validateSessionID(session.ID); err != nil {
		return err
	}

	// Snapshot the session while holding only the domain session lock,
	// not the storage lock yet.
	snap := session.Snapshot()

	s.mu.Lock()
	defer s.mu.Unlock()

	data := snapshotToData(snap)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	sessionsDir := filepath.Join(s.baseDir, "sessions")
	f, err := os.CreateTemp(sessionsDir, snap.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	tmpName := f.Name()
	// Ensure restricted permissions on the temp file
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

	filePath := s.sessionPath(snap.ID)
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	// Sync the directory to ensure the rename is durable
	df, err := os.Open(sessionsDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	defer df.Close()
	if err := df.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	return nil
}

func (s *JSONFileStorage) Load(id string) (*domain.Session, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadUnlocked(id)
}

func (s *JSONFileStorage) Delete(id string) error {
	if err := validateSessionID(id); err != nil {
		return err
	}

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

type ListError struct {
	Errors []error
}

func (e *ListError) Error() string {
	return fmt.Sprintf("failed to load %d sessions", len(e.Errors))
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
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5]
		if err := validateSessionID(id); err != nil {
			// Skip files with invalid names
			continue
		}

		session, err := s.loadUnlocked(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", id, err))
			continue
		}
		sessions = append(sessions, session)
	}

	if len(errs) > 0 {
		return sessions, &ListError{Errors: errs}
	}

	return sessions, nil
}

func (s *JSONFileStorage) loadUnlocked(id string) (*domain.Session, error) {
	filePath := s.sessionPath(id)

	info, err := os.Lstat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrSymlinkNotAllowed, id)
	}

	if info.Size() > maxSessionFileSize {
		return nil, fmt.Errorf("%w: %s (%d bytes)", ErrSessionFileTooLarge, id, info.Size())
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var sd sessionData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, err
	}

	return dataToSession(&sd)
}

func snapshotToData(snap domain.SessionSnapshot) *sessionData {
	transitions := make([]transitionData, len(snap.Transitions))
	for i, t := range snap.Transitions {
		transitions[i] = transitionData{
			From:      t.From.String(),
			To:        t.To.String(),
			Reason:    t.Reason,
			Timestamp: t.Timestamp,
		}
	}

	return &sessionData{
		ID:           snap.ID,
		ProviderType: snap.ProviderType,
		State:        snap.State.String(),
		WorkingDir:   snap.WorkingDir,
		CreatedAt:    snap.CreatedAt,
		UpdatedAt:    snap.UpdatedAt,
		CurrentTask:  snap.CurrentTask,
		Output:       snap.Output,
		ErrorMessage: snap.ErrorMessage,
		Transitions:  transitions,
	}
}

func dataToSession(data *sessionData) (*domain.Session, error) {
	state, err := parseSessionState(data.State)
	if err != nil {
		return nil, err
	}

	transitions := make([]domain.StateTransition, len(data.Transitions))
	for i, t := range data.Transitions {
		from, err := parseSessionState(t.From)
		if err != nil {
			return nil, fmt.Errorf("transition %d from state: %w", i, err)
		}
		to, err := parseSessionState(t.To)
		if err != nil {
			return nil, fmt.Errorf("transition %d to state: %w", i, err)
		}
		transitions[i] = domain.StateTransition{
			From:      from,
			To:        to,
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
	}, nil
}

func parseSessionState(s string) (domain.SessionState, error) {
	switch s {
	case "created":
		return domain.SessionStateCreated, nil
	case "starting":
		return domain.SessionStateStarting, nil
	case "running":
		return domain.SessionStateRunning, nil
	case "paused":
		return domain.SessionStatePaused, nil
	case "stopping":
		return domain.SessionStateStopping, nil
	case "stopped":
		return domain.SessionStateStopped, nil
	case "error":
		return domain.SessionStateError, nil
	default:
		return domain.SessionStateCreated, fmt.Errorf("%w: %s", ErrInvalidSessionState, s)
	}
}
