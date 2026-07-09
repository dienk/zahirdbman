// Package profile manages named PostgreSQL connection profiles, persisted as
// JSON, with one profile marked active at a time.
package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Profile is a saved connection to a PostgreSQL server.
type Profile struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
	AdminDB  string `json:"adminDB"`
}

// Validate checks required fields and character safety of the name.
func (p Profile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("profile name is required")
	}
	if len(p.Name) > 64 {
		return fmt.Errorf("profile name is too long")
	}
	if p.Host == "" || p.Port == "" {
		return fmt.Errorf("host and port are required")
	}
	if p.User == "" {
		return fmt.Errorf("user is required")
	}
	return nil
}

// file is the on-disk representation.
type file struct {
	Active   string    `json:"active"`
	Profiles []Profile `json:"profiles"`
}

// Store is a thread-safe, file-backed collection of profiles.
type Store struct {
	path string
	mu   sync.Mutex
	data file
}

// Load opens (or initialises) the profile store at path.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // empty store; persisted on first write
		}
		return nil, fmt.Errorf("read profiles %q: %w", path, err)
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("parse profiles %q: %w", path, err)
	}
	return s, nil
}

// save writes the store to disk. Caller must hold the mutex.
func (s *Store) save() error {
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the file may contain passwords.
	return os.WriteFile(s.path, b, 0o600)
}

// List returns all profiles ordered by name.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Profile, len(s.data.Profiles))
	copy(out, s.data.Profiles)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Empty reports whether no profiles exist.
func (s *Store) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data.Profiles) == 0
}

// Get returns the named profile and whether it was found.
func (s *Store) Get(name string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(name)
}

func (s *Store) getLocked(name string) (Profile, bool) {
	for _, p := range s.data.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// ActiveName returns the name of the active profile ("" if none).
func (s *Store) ActiveName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Active
}

// Active returns the active profile and whether one is set.
func (s *Store) Active() (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Active == "" {
		return Profile{}, false
	}
	return s.getLocked(s.data.Active)
}

// Upsert adds a new profile or replaces an existing one with the same name.
// If it is the only profile, it becomes active automatically.
func (s *Store) Upsert(p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i, existing := range s.data.Profiles {
		if existing.Name == p.Name {
			s.data.Profiles[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		s.data.Profiles = append(s.data.Profiles, p)
	}
	if s.data.Active == "" {
		s.data.Active = p.Name
	}
	return s.save()
}

// Delete removes a profile. If it was active, the active marker is cleared or
// moved to the first remaining profile.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Profiles[:0]
	found := false
	for _, p := range s.data.Profiles {
		if p.Name == name {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return fmt.Errorf("profile %q not found", name)
	}
	s.data.Profiles = out
	if s.data.Active == name {
		if len(s.data.Profiles) > 0 {
			s.data.Active = s.data.Profiles[0].Name
		} else {
			s.data.Active = ""
		}
	}
	return s.save()
}

// SetActive marks the named profile active.
func (s *Store) SetActive(name string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.getLocked(name)
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	s.data.Active = name
	if err := s.save(); err != nil {
		return Profile{}, err
	}
	return p, nil
}
