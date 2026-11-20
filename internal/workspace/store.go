package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Version is the metadata schema version written by this build.
const Version = 1

// Workspace is a named group of hosts.
type Workspace struct {
	Name string
}

var (
	// ErrDuplicate is returned when creating or renaming to a name already in use.
	ErrDuplicate = errors.New("a workspace with this name already exists")
	// ErrNotFound is returned when a workspace name is unknown.
	ErrNotFound = errors.New("no such workspace")
)

// Store owns the workspace metadata file. Mutations change the in-memory copy
// only; call Save to persist.
type Store struct {
	path        string
	names       []string
	assignments map[string]string
	aliases     map[string]bool

	// save is the write hook; tests replace it to simulate a failing disk.
	save func(path string, data []byte) error
}

type fileSchema struct {
	Version        int               `json:"version"`
	Workspaces     []string          `json:"workspaces"`
	WorkspaceOrder int               `json:"-"`
	Assignments    map[string]string `json:"assignments"`
}

// DefaultPath returns %APPDATA%\elyms\workspaces.json on Windows (the OS user
// config directory with an elyms subdirectory).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "elyms", "workspaces.json"), nil
}

// Load reads metadata from path. A missing file is an empty store, not an
// error. aliases is the set of host aliases currently in the SSH config;
// assignments to aliases outside it are ignored (not rewritten on disk).
func Load(path string, aliases []string) (*Store, error) {
	s := &Store{
		path:        path,
		assignments: map[string]string{},
		aliases:     map[string]bool{},
		save:        writeFileAtomic,
	}
	for _, a := range aliases {
		s.aliases[a] = true
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workspace metadata: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}

	var f fileSchema
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("workspace metadata %s is not valid JSON: %w", path, err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("workspace metadata %s uses unsupported version %d (this elyms writes version %d)", path, f.Version, Version)
	}

	seen := map[string]bool{}
	for _, raw := range f.Workspaces {
		name := strings.TrimSpace(raw)
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("workspace metadata %s: %w", path, err)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("workspace metadata %s: duplicate workspace %q", path, name)
		}
		seen[key] = true
		s.names = append(s.names, name)
	}
	for alias, name := range f.Assignments {
		name = strings.TrimSpace(name)
		if name == "" || !s.aliases[alias] || !seen[strings.ToLower(name)] {
			continue
		}
		s.assignments[alias] = s.canonical(name)
	}
	return s, nil
}

// ValidateName accepts a non-empty label without control characters; the name
// is trimmed before it is validated and stored.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("workspace name is required")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("workspace name must not contain control characters")
		}
	}
	return nil
}

func (s *Store) Path() string { return s.path }

// Workspaces returns the workspace names in display order.
func (s *Store) Workspaces() []string {
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}

// Workspace returns the workspace called name (case-insensitive).
func (s *Store) Workspace(name string) (Workspace, bool) {
	c := s.canonical(name)
	if c == "" {
		return Workspace{}, false
	}
	return Workspace{Name: c}, true
}

// Assignments returns a copy of the alias-to-workspace map.
func (s *Store) Assignments() map[string]string {
	out := make(map[string]string, len(s.assignments))
	for k, v := range s.assignments {
		out[k] = v
	}
	return out
}

// WorkspaceOf returns the workspace assigned to alias, or "".
func (s *Store) WorkspaceOf(alias string) string { return s.assignments[alias] }

func (s *Store) index(name string) int {
	c := strings.ToLower(strings.TrimSpace(name))
	for i, n := range s.names {
		if strings.ToLower(n) == c {
			return i
		}
	}
	return -1
}

func (s *Store) canonical(name string) string {
	if i := s.index(name); i >= 0 {
		return s.names[i]
	}
	return ""
}

// Create adds a new workspace and returns its canonical name.
func (s *Store) Create(name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return "", err
	}
	if s.index(name) >= 0 {
		return "", ErrDuplicate
	}
	s.names = append(s.names, name)
	return name, nil
}

// Rename renames old to new, keeping every assignment. The case-only rename
// of an existing workspace is allowed.
func (s *Store) Rename(oldName, newName string) error {
	i := s.index(oldName)
	if i < 0 {
		return ErrNotFound
	}
	newName = strings.TrimSpace(newName)
	if err := ValidateName(newName); err != nil {
		return err
	}
	if j := s.index(newName); j >= 0 && j != i {
		return ErrDuplicate
	}
	from := s.names[i]
	s.names[i] = newName
	for alias, w := range s.assignments {
		if w == from {
			s.assignments[alias] = newName
		}
	}
	return nil
}

// Delete removes a workspace. Its hosts stay assigned to nothing; host
// entries themselves are never touched here.
func (s *Store) Delete(name string) error {
	i := s.index(name)
	if i < 0 {
		return ErrNotFound
	}
	gone := s.names[i]
	s.names = append(s.names[:i], s.names[i+1:]...)
	for alias, w := range s.assignments {
		if w == gone {
			delete(s.assignments, alias)
		}
	}
	return nil
}

// Assign puts alias in workspace name. An empty name clears the assignment.
func (s *Store) Assign(alias, name string) error {
	if !s.aliases[alias] {
		return fmt.Errorf("host %q is not in your SSH config", alias)
	}
	if name == "" {
		delete(s.assignments, alias)
		return nil
	}
	c := s.canonical(name)
	if c == "" {
		return ErrNotFound
	}
	s.assignments[alias] = c
	return nil
}

// Unassign clears alias from any workspace.
func (s *Store) Unassign(alias string) { delete(s.assignments, alias) }

// Save writes the metadata atomically: a temp file in the same directory is
// written and then renamed over the target, so a failure never leaves a
// half-written workspaces.json behind.
func (s *Store) Save() error {
	f := fileSchema{Version: Version, Workspaces: s.names, Assignments: map[string]string{}}
	if f.Workspaces == nil {
		f.Workspaces = []string{}
	}
	for k, v := range s.assignments {
		f.Assignments[k] = v
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if s.save != nil {
		return s.save(s.path, data)
	}
	return writeFileAtomic(s.path, data)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".workspaces-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// A leftover temp file is never metadata, so removing it is always safe.
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
