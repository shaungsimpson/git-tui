package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// State is what the app remembers between runs. It lives apart from the configuration, which holds the
// repository list only.
type State struct {
	UTC      bool              `json:"utc"`
	Profile  string            `json:"profile,omitempty"`
	ScanRoot string            `json:"scanRoot,omitempty"` // where adding repositories last looked
	Sorts    map[string]string `json:"sorts,omitempty"`    // each list's sort, by view
}

// StatePath is $XDG_STATE_HOME/git-tui/state.json, which defaults to ~/.local/state/git-tui/state.json.
func StatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "git-tui", "state.json")
}

// LoadState reads the remembered state; a missing file is the zero State.
func LoadState(path string) (State, error) {
	var s State
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

// SaveState writes the state through a temporary file, so a crash never leaves half a file.
func SaveState(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

// writeFile replaces path through a temporary file in the same folder, so a crash never leaves half a file.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // fails harmlessly after the rename
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // the write error is the one worth reporting
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
