package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

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
	GetMessages(id string) ([]domain.Message, error)
}

type JSONFileStorage struct {
	baseDir string
	mu      sync.RWMutex
}

var (
	sessionIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
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

	attemptsDir := filepath.Join(sessionsDir, "attempts")
	if err := os.MkdirAll(attemptsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create attempts directory: %w", err)
	}

	resumeTokensDir := filepath.Join(sessionsDir, "resume_tokens")
	if err := os.MkdirAll(resumeTokensDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create resume_tokens directory: %w", err)
	}

	terminalsDir := filepath.Join(baseDir, "terminals")
	if err := os.MkdirAll(terminalsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create terminals directory: %w", err)
	}

	// Verify permissions if it already existed
	info, err := os.Stat(sessionsDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			// Directory is too permissive, try to fix it
			_ = os.Chmod(sessionsDir, 0o700)
		}
	}

	info, err = os.Stat(terminalsDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(terminalsDir, 0o700)
		}
	}

	info, err = os.Stat(attemptsDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(attemptsDir, 0o700)
		}
	}

	info, err = os.Stat(resumeTokensDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(resumeTokensDir, 0o700)
		}
	}

	return &JSONFileStorage{
		baseDir: baseDir,
	}, nil
}

func DefaultBaseDir() string {
	if baseDir := os.Getenv("ORBITMESH_BASE_DIR"); baseDir != "" {
		return baseDir
	}
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

	jsonData, err := json.MarshalIndent(snap, "", "  ")
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

func (s *JSONFileStorage) GetMessages(id string) ([]domain.Message, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	messagesFromLog, logErr := s.readMessagesFromJSONLUnlocked(id)
	if logErr == nil {
		return messagesFromLog, nil
	}

	var corruptionErr *MessageLogCorruptionError
	if errors.As(logErr, &corruptionErr) {
		if len(messagesFromLog) > 0 {
			return messagesFromLog, nil
		}
	} else if !errors.Is(logErr, os.ErrNotExist) && !errors.Is(logErr, ErrSessionNotFound) {
		return nil, logErr
	}

	return s.getMessagesUnlocked(id)
}

func (s *JSONFileStorage) getMessagesUnlocked(id string) ([]domain.Message, error) {
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

	var snap domain.SessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	if snap.Messages == nil {
		return []domain.Message{}, nil
	}

	return snap.Messages, nil
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

	var snap domain.SessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	return domain.SessionFromSnapshot(snap), nil
}
