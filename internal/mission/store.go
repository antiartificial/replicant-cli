package mission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Store persists missions as JSON files under a configurable directory.
type Store struct {
	dir string
}

// NewStore creates a Store rooted at dir. The directory is created if it does
// not exist.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Save writes a mission to disk atomically (tmp file + rename).
func (s *Store) Save(m *Mission) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("mission store: mkdir %s: %w", s.dir, err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("mission store: marshal %s: %w", m.ID, err)
	}

	dest := filepath.Join(s.dir, m.ID+".json")
	tmp := dest + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("mission store: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("mission store: rename %s: %w", tmp, err)
	}
	return nil
}

// Load reads and decodes a mission by ID.
func (s *Store) Load(id string) (*Mission, error) {
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mission store: load %s: %w", id, err)
	}
	var m Mission
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("mission store: decode %s: %w", id, err)
	}
	return &m, nil
}

// List returns all stored missions sorted by CreatedAt descending.
func (s *Store) List() ([]*Mission, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mission store: list dir: %w", err)
	}

	var missions []*Mission
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		id := name[:len(name)-5]
		m, err := s.Load(id)
		if err != nil {
			// Skip corrupt files but don't abort.
			continue
		}
		missions = append(missions, m)
	}

	sort.Slice(missions, func(i, j int) bool {
		return missions[i].CreatedAt.After(missions[j].CreatedAt)
	})
	return missions, nil
}
