package workspace

import (
	"errors"
	"strings"
	"testing"
)

func sampleStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(t.TempDir()+"/workspaces.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateSavesCanonicalName(t *testing.T) {
	s := sampleStore(t)
	name, err := s.Create("  My Servers  ")
	if err != nil {
		t.Fatalf("Create should trim the label: %v", err)
	}
	if name != "My Servers" {
		t.Errorf("canonical name = %q", name)
	}
	if got := strings.Join(s.Workspaces(), ","); got != "My Servers" {
		t.Errorf("stored = %q", got)
	}
	if _, err := s.Create("my servers"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("case variant err = %v", err)
	}
	if _, err := s.Create("a\tb"); err == nil {
		t.Errorf("control characters should be refused")
	}
}

func TestRenameTrimsAndRenamesToItself(t *testing.T) {
	s := sampleStore(t)
	_, _ = s.Create("Prod")
	if err := s.Rename("Prod", "  Prod  "); err != nil {
		t.Fatalf("rename to the same trimmed label should be a no-op: %v", err)
	}
	if err := s.Rename("Prod", " QA "); err != nil {
		t.Fatalf("rename should trim: %v", err)
	}
	if got := strings.Join(s.Workspaces(), ","); got != "QA" {
		t.Errorf("stored = %q", got)
	}
	if err := s.Rename("Prod", "X"); !errors.Is(err, ErrNotFound) {
		t.Errorf("renaming an unknown workspace err = %v", err)
	}
	if err := s.Rename("QA", "  "); err == nil {
		t.Errorf("blank rename should fail")
	}
}
