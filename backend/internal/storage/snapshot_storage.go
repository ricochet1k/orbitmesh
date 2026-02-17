package storage

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

var (
	ErrSnapshotNotFound       = errors.New("snapshot not found")
	ErrInvalidSnapshotVersion = errors.New("snapshot version incompatible")
	ErrSnapshotCorrupted      = errors.New("snapshot data corrupted")
)

// SnapshotStorage handles persistence of session snapshots.
type SnapshotStorage interface {
	Save(snapshot *session.SessionSnapshot) error
	Load(sessionID string) (*session.SessionSnapshot, error)
	Delete(sessionID string) error
	List() ([]*session.SessionSnapshot, error)
	Exists(sessionID string) bool
}

// JSONFileSnapshotStorage implements SnapshotStorage using JSON files.
type JSONFileSnapshotStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewJSONFileSnapshotStorage creates a new JSON file-based snapshot storage.
func NewJSONFileSnapshotStorage(baseDir string) (*JSONFileSnapshotStorage, error) {
	snapshotsDir := filepath.Join(baseDir, "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	// Verify permissions
	info, err := os.Stat(snapshotsDir)
	if err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(snapshotsDir, 0o700)
		}
	}

	return &JSONFileSnapshotStorage{
		baseDir: baseDir,
	}, nil
}

// Save persists a session snapshot to disk.
func (s *JSONFileSnapshotStorage) Save(snapshot *session.SessionSnapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	if err := validateSessionID(snapshot.SessionID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snapshotsDir := filepath.Join(s.baseDir, "snapshots")
	filename := filepath.Join(snapshotsDir, fmt.Sprintf("%s.json.gz", snapshot.SessionID))

	// Create temp file
	tempFile := filename + ".tmp"
	f, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer f.Close()

	// Write gzip-compressed JSON
	gzWriter := gzip.NewWriter(f)
	defer gzWriter.Close()

	encoder := json.NewEncoder(gzWriter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	if err := gzWriter.Close(); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to close snapshot file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, filename); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename snapshot file: %w", err)
	}

	return nil
}

// Load reads a session snapshot from disk.
func (s *JSONFileSnapshotStorage) Load(sessionID string) (*session.SessionSnapshot, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshotsDir := filepath.Join(s.baseDir, "snapshots")
	filename := filepath.Join(snapshotsDir, fmt.Sprintf("%s.json.gz", sessionID))

	// Check if file exists
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return nil, ErrSnapshotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat snapshot file: %w", err)
	}

	// Security check: no symlinks
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlinkNotAllowed
	}

	// Size check
	if info.Size() > maxSessionFileSize {
		return nil, ErrSessionFileTooLarge
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer f.Close()

	// Read gzip-compressed JSON
	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	var snapshot session.SessionSnapshot
	decoder := json.NewDecoder(gzReader)
	if err := decoder.Decode(&snapshot); err != nil {
		if err == io.EOF {
			return nil, ErrSnapshotCorrupted
		}
		return nil, fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Version check
	if snapshot.Version > session.CurrentSnapshotVersion {
		return nil, fmt.Errorf("%w: snapshot version %d > current %d",
			ErrInvalidSnapshotVersion, snapshot.Version, session.CurrentSnapshotVersion)
	}

	// Migrate if needed
	if snapshot.Version < session.CurrentSnapshotVersion {
		if err := s.migrateSnapshot(&snapshot); err != nil {
			return nil, fmt.Errorf("failed to migrate snapshot: %w", err)
		}
	}

	return &snapshot, nil
}

// Delete removes a session snapshot from disk.
func (s *JSONFileSnapshotStorage) Delete(sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snapshotsDir := filepath.Join(s.baseDir, "snapshots")
	filename := filepath.Join(snapshotsDir, fmt.Sprintf("%s.json.gz", sessionID))

	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			return ErrSnapshotNotFound
		}
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	return nil
}

// Exists checks if a snapshot exists for the given session ID.
func (s *JSONFileSnapshotStorage) Exists(sessionID string) bool {
	if err := validateSessionID(sessionID); err != nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshotsDir := filepath.Join(s.baseDir, "snapshots")
	filename := filepath.Join(snapshotsDir, fmt.Sprintf("%s.json.gz", sessionID))

	_, err := os.Stat(filename)
	return err == nil
}

// List returns all session snapshots.
func (s *JSONFileSnapshotStorage) List() ([]*session.SessionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshotsDir := filepath.Join(s.baseDir, "snapshots")

	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*session.SessionSnapshot{}, nil
		}
		return nil, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	var snapshots []*session.SessionSnapshot
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".json.gz") {
			continue
		}

		// Extract session ID from filename
		sessionID := strings.TrimSuffix(name, ".json.gz")

		// Try to load snapshot (skip if corrupted)
		snapshot, err := s.Load(sessionID)
		if err != nil {
			continue
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

// migrateSnapshot migrates an older snapshot to the current version.
func (s *JSONFileSnapshotStorage) migrateSnapshot(snapshot *session.SessionSnapshot) error {
	// Currently only version 1 exists, so no migration needed
	// Future versions would add migration logic here
	snapshot.Version = session.CurrentSnapshotVersion
	return nil
}
