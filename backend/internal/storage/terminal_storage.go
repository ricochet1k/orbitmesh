package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

var (
	ErrTerminalNotFound  = errors.New("terminal not found")
	ErrInvalidTerminalID = errors.New("invalid terminal id")
	ErrTerminalKind      = errors.New("invalid terminal kind")
)

type TerminalStorage interface {
	SaveTerminal(term *domain.Terminal) error
	LoadTerminal(id string) (*domain.Terminal, error)
	DeleteTerminal(id string) error
	ListTerminals() ([]*domain.Terminal, error)
}

type terminalData struct {
	ID            string                `json:"id"`
	SessionID     string                `json:"session_id,omitempty"`
	Kind          string                `json:"terminal_kind"`
	CreatedAt     time.Time             `json:"created_at"`
	LastUpdatedAt time.Time             `json:"last_updated_at"`
	LastSeq       int64                 `json:"last_seq,omitempty"`
	LastSnapshot  *terminalSnapshotData `json:"last_snapshot,omitempty"`
}

type terminalSnapshotData struct {
	Rows  int      `json:"rows"`
	Cols  int      `json:"cols"`
	Lines []string `json:"lines"`
}

func (s *JSONFileStorage) terminalPath(id string) string {
	return filepath.Join(s.baseDir, "terminals", id+".json")
}

func validateTerminalID(id string) error {
	if !sessionIDRegex.MatchString(id) {
		return fmt.Errorf("%w: %s", ErrInvalidTerminalID, id)
	}
	return nil
}

func (s *JSONFileStorage) SaveTerminal(term *domain.Terminal) error {
	if err := validateTerminalID(term.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data := terminalToData(term)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal terminal: %w", err)
	}

	terminalsDir := filepath.Join(s.baseDir, "terminals")
	f, err := os.CreateTemp(terminalsDir, term.ID+".*.tmp")
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

	filePath := s.terminalPath(term.ID)
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	df, err := os.Open(terminalsDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}
	defer df.Close()
	if err := df.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageWrite, err)
	}

	return nil
}

func (s *JSONFileStorage) LoadTerminal(id string) (*domain.Terminal, error) {
	if err := validateTerminalID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadTerminalUnlocked(id)
}

func (s *JSONFileStorage) DeleteTerminal(id string) error {
	if err := validateTerminalID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := s.terminalPath(id)
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrTerminalNotFound
		}
		return fmt.Errorf("failed to delete terminal file: %w", err)
	}
	return nil
}

func (s *JSONFileStorage) ListTerminals() ([]*domain.Terminal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	terminalsDir := filepath.Join(s.baseDir, "terminals")
	entries, err := os.ReadDir(terminalsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.Terminal{}, nil
		}
		return nil, fmt.Errorf("failed to read terminals directory: %w", err)
	}

	terminals := make([]*domain.Terminal, 0, len(entries))
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5]
		if err := validateTerminalID(id); err != nil {
			continue
		}

		term, err := s.loadTerminalUnlocked(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("terminal %s: %w", id, err))
			continue
		}
		terminals = append(terminals, term)
	}

	if len(errs) > 0 {
		return terminals, &ListError{Errors: errs}
	}

	return terminals, nil
}

func (s *JSONFileStorage) loadTerminalUnlocked(id string) (*domain.Terminal, error) {
	filePath := s.terminalPath(id)

	info, err := os.Lstat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTerminalNotFound
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

	var td terminalData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, err
	}

	return dataToTerminal(&td)
}

func terminalToData(term *domain.Terminal) *terminalData {
	var snapshot *terminalSnapshotData
	if term.LastSnapshot != nil {
		snapshot = &terminalSnapshotData{
			Rows:  term.LastSnapshot.Rows,
			Cols:  term.LastSnapshot.Cols,
			Lines: term.LastSnapshot.Lines,
		}
	}

	return &terminalData{
		ID:            term.ID,
		SessionID:     term.SessionID,
		Kind:          string(term.Kind),
		CreatedAt:     term.CreatedAt,
		LastUpdatedAt: term.LastUpdatedAt,
		LastSeq:       term.LastSeq,
		LastSnapshot:  snapshot,
	}
}

func dataToTerminal(data *terminalData) (*domain.Terminal, error) {
	kind, err := parseTerminalKind(data.Kind)
	if err != nil {
		return nil, err
	}

	term := &domain.Terminal{
		ID:            data.ID,
		SessionID:     data.SessionID,
		Kind:          kind,
		CreatedAt:     data.CreatedAt,
		LastUpdatedAt: data.LastUpdatedAt,
		LastSeq:       data.LastSeq,
	}
	if data.LastSnapshot != nil {
		term.LastSnapshot = &terminal.Snapshot{
			Rows:  data.LastSnapshot.Rows,
			Cols:  data.LastSnapshot.Cols,
			Lines: data.LastSnapshot.Lines,
		}
	}
	return term, nil
}

func parseTerminalKind(kind string) (domain.TerminalKind, error) {
	switch kind {
	case string(domain.TerminalKindPTY):
		return domain.TerminalKindPTY, nil
	case string(domain.TerminalKindAdHoc):
		return domain.TerminalKindAdHoc, nil
	default:
		return domain.TerminalKindAdHoc, fmt.Errorf("%w: %s", ErrTerminalKind, kind)
	}
}
