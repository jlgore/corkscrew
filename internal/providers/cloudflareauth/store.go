package cloudflareauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type OAuthStore interface {
	Load(profile string) (*OAuthProfile, error)
	Save(profile *OAuthProfile) error
	Delete(profile string) error
}

type FileOAuthStore struct {
	BaseDir string
	mu      sync.RWMutex
}

func (s *FileOAuthStore) Load(profile string) (*OAuthProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path, err := s.profilePath(profile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p OAuthProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Profile == "" {
		p.Profile = normalizeProfile(profile)
	}
	return &p, nil
}

func (s *FileOAuthStore) Save(profile *OAuthProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.profilePath(profile.Profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *FileOAuthStore) Delete(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.profilePath(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *FileOAuthStore) profilePath(profile string) (string, error) {
	baseDir := s.BaseDir
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		baseDir = filepath.Join(home, ".corkscrew", "providers", "cloudflare", "profiles")
	}
	return filepath.Join(baseDir, normalizeProfile(profile)+".json"), nil
}

func normalizeProfile(profile string) string {
	if profile == "" {
		return DefaultProfileName
	}
	return profile
}
