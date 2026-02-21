package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrResumeTokenNotFound = errors.New("resume token not found")

type ResumeTokenStorage interface {
	SaveResumeToken(token *ResumeTokenMetadata) error
	LoadResumeToken(tokenID string) (*ResumeTokenMetadata, error)
}

type ResumeTokenMetadata struct {
	TokenID          string     `json:"token_id"`
	SessionID        string     `json:"session_id"`
	AttemptID        string     `json:"attempt_id"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	ConsumedAt       *time.Time `json:"consumed_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	RevocationReason string     `json:"revocation_reason,omitempty"`
}

func (s *JSONFileStorage) resumeTokensDir() string {
	return filepath.Join(s.baseDir, "sessions", "resume_tokens")
}

func (s *JSONFileStorage) resumeTokenPath(tokenID string) string {
	return filepath.Join(s.resumeTokensDir(), tokenID+".json")
}

func validateResumeTokenID(id string) error {
	if !sessionIDRegex.MatchString(id) {
		return fmt.Errorf("%w: %s", ErrInvalidSessionID, id)
	}
	return nil
}

func (s *JSONFileStorage) SaveResumeToken(token *ResumeTokenMetadata) error {
	if token == nil {
		return fmt.Errorf("resume token metadata is nil")
	}
	if err := validateResumeTokenID(token.TokenID); err != nil {
		return err
	}
	if err := validateSessionID(token.SessionID); err != nil {
		return err
	}
	if err := validateRunAttemptID(token.AttemptID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.resumeTokensDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create resume-token directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal resume token: %w", err)
	}

	f, err := os.CreateTemp(dir, token.TokenID+".*.tmp")
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

	if err := os.Rename(tmpName, s.resumeTokenPath(token.TokenID)); err != nil {
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

func (s *JSONFileStorage) LoadResumeToken(tokenID string) (*ResumeTokenMetadata, error) {
	if err := validateResumeTokenID(tokenID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.resumeTokenPath(tokenID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrResumeTokenNotFound
		}
		return nil, err
	}

	var out ResumeTokenMetadata
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
