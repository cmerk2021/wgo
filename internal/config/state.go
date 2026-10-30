package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// State is persisted runtime state.
type State struct {
	LastProfile string `json:"last_profile"`
}

// LoadState reads StatePath, returning an empty state if missing.
func LoadState() (*State, error) {
	st := &State{}
	data, err := os.ReadFile(StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return &State{}, nil // corrupt state is non-fatal
	}
	return st, nil
}

// Save writes the state to StatePath, creating directories.
func (s *State) Save() error {
	if err := os.MkdirAll(filepath.Dir(StatePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(StatePath, data, 0o644)
}
