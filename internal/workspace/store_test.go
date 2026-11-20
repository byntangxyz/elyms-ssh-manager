package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T, aliases ...string) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "elyms", "workspaces.json")
	s, err := Load(path, aliases)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestLoadMissingFileIsEmptyStore(t *testing.T) {
	s, path := newStore(t, "web", "db")
	if got := s.Workspaces(); len(got) != 0 {
		t.Errorf("missing file should give no workspaces, got %v", got)
	}
	if len(s.Assignments()) != 0 {
		t.Errorf("missing file should give no assignments")
	}
	if s.Path() != path {
		t.Errorf("path = %q", s.Path())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("loading must not create the file")
	}
}

func TestSaveThenLoadRoundTripsOrderAndAssignments(t *testing.T) {
	s, path := newStore(t, "web", "db", "*")
	for _, n := range []string{"Personal", "Staging", "prod"} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Assign("web", "Staging"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("db", "Personal"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path, []string{"web", "db", "*"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(back.Workspaces(), ","), "Personal,Staging,prod"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	if back.WorkspaceOf("web") != "Staging" || back.WorkspaceOf("db") != "Personal" {
		t.Errorf("assignments lost: %v", back.Assignments())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Version     int               `json:"version"`
		Workspaces  []string          `json:"workspaces"`
		Assignments map[string]string `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("metadata must be JSON: %v\n%s", err, raw)
	}
	if f.Version != Version {
		t.Errorf("version = %d", f.Version)
	}
	if len(f.Assignments) != 2 {
		t.Errorf("assignments = %v", f.Assignments)
	}
}

func TestInvalidWorkspaceNamesAreRejected(t *testing.T) {
	s, _ := newStore(t)
	bad := []string{"", "   ", " padded", "padded ", "tab\there", "line\nbreak", "\x00null", "bell\x07"}
	for _, name := range bad {
		if _, err := s.Create(name); err == nil {
			t.Errorf("Create(%q) should fail", name)
		}
	}
	if got := s.Workspaces(); len(got) != 0 {
		t.Errorf("failed creates must not add workspaces: %v", got)
	}
	good := []string{"NOC", "prod", "My Servers", "staging → eu", "Prod(1)"}
	for _, name := range good {
		if _, err := s.Create(name); err != nil {
			t.Errorf("Create(%q) failed: %v", name, err)
		}
	}
}

func TestDuplicateWorkspaceNamesAreRejectedCaseInsensitively(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create("Prod"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Prod", "prod"} {
		if _, err := s.Create(name); !errors.Is(err, ErrDuplicate) {
			t.Errorf("Create(%q) err = %v, want ErrDuplicate", name, err)
		}
	}
	if err := s.Rename("Prod", "PROD"); err != nil {
		t.Errorf("case-only rename should be allowed: %v", err)
	}
	if _, err := s.Create("staging"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("staging", "proD"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("rename onto existing name err = %v, want ErrDuplicate", err)
	}
}

func TestUnknownAssignmentsAreIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	data := `{"version":1,"workspaces":["Prod"],"assignments":{"gone":"Prod","ghost":"Nope","web":"Prod"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path, []string{"web"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Assignments(); len(got) != 1 || got["web"] != "Prod" {
		t.Errorf("only the known alias and workspace should survive: %v", got)
	}
	if err := s.Assign("gone", "Prod"); err == nil {
		t.Errorf("assigning a host that is not in the config should fail")
	}
}

func TestDeleteWorkspaceUnassignsHostsAndKeepsOthers(t *testing.T) {
	s, _ := newStore(t, "web", "db", "api")
	for _, n := range []string{"Prod", "Staging"} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Assign("web", "Prod")
	_ = s.Assign("db", "Prod")
	_ = s.Assign("api", "Staging")

	if err := s.Delete("prod"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Workspaces(), ","); got != "Staging" {
		t.Errorf("workspaces = %s", got)
	}
	if a := s.Assignments(); len(a) != 1 || a["api"] != "Staging" {
		t.Errorf("hosts of a deleted workspace must become unassigned: %v", a)
	}
	if err := s.Delete("Prod"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice err = %v", err)
	}
}

func TestRenameKeepsAssignments(t *testing.T) {
	s, _ := newStore(t, "web", "db")
	if _, err := s.Create("Staging"); err != nil {
		t.Fatal(err)
	}
	_ = s.Assign("web", "Staging")
	_ = s.Assign("db", "Staging")
	if err := s.Rename("staging", "QA 2"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Workspaces(), ","); got != "QA 2" {
		t.Errorf("workspaces = %s", got)
	}
	if s.WorkspaceOf("web") != "QA 2" || s.WorkspaceOf("db") != "QA 2" {
		t.Errorf("assignments must follow the rename: %v", s.Assignments())
	}
	if err := s.Rename("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename of unknown err = %v", err)
	}
}

func TestAssignUnassignAndSingleWorkspacePerHost(t *testing.T) {
	s, _ := newStore(t, "web")
	for _, n := range []string{"A", "B"} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Assign("web", "A")
	_ = s.Assign("web", "B")
	if got := s.Assignments(); len(got) != 1 || got["web"] != "B" {
		t.Errorf("a host has at most one workspace: %v", got)
	}
	if err := s.Assign("web", ""); err != nil {
		t.Fatal(err)
	}
	if len(s.Assignments()) != 0 {
		t.Errorf("empty name should unassign: %v", s.Assignments())
	}
	if err := s.Assign("web", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("assign to unknown workspace err = %v", err)
	}
}

func TestMalformedMetadataIsAnErrorNotAPanic(t *testing.T) {
	cases := map[string]string{
		"truncated json": `{"version":1,"workspaces":["Prod"`,
		"wrong type":     `{"version":1,"workspaces":"Prod"}`,
		"future version": `{"version":2,"workspaces":[]}`,
		"no version":     `{"workspaces":[]}`,
		"dup names":      `{"version":1,"workspaces":["Prod","prod"]}`,
		"blank name":     `{"version":1,"workspaces":["  "]}`,
		"control char":   "{\"version\":1,\"workspaces\":[\"a\\u0001b\"]}",
	}
	for name, content := range cases {
		path := filepath.Join(t.TempDir(), "workspaces.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path, []string{"web"})
		if err == nil {
			t.Errorf("%s: want error, got store %+v", name, s)
			continue
		}
		if !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "no such") {
			t.Errorf("%s: error should name workspace metadata: %v", name, err)
		}
	}
	// A failed load must not rewrite the file.
	path := filepath.Join(t.TempDir(), "workspaces.json")
	content := `{"version":99,"workspaces":["Prod"]}`
	_ = os.WriteFile(path, []byte(content), 0o600)
	if _, err := Load(path, nil); err == nil {
		t.Fatal("future version should fail")
	}
	if got, _ := os.ReadFile(path); string(got) != content {
		t.Errorf("failed load must leave the file untouched: %s", got)
	}
}

func TestEmptyAndMissingFilesLoadAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(path, nil); err != nil || len(s.Workspaces()) != 0 {
		t.Errorf("empty file should load empty: %v %v", s, err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path, nil)
	if err != nil || s.Workspaces() == nil {
		t.Errorf("version-only file should load: %v %v", s, err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedSaveLeavesPreviousFileIntact(t *testing.T) {
	s, path := newStore(t, "web")
	if _, err := s.Create("Prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("web", "Prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	s.save = func(string, []byte) error { return errors.New("disk full") }
	if _, err := s.Create("Staging"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("save should surface the failure: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("failed save must leave the old file intact:\nbefore %s\nafter  %s", before, after)
	}
}

func TestAtomicSaveThroughTheRealWriter(t *testing.T) {
	// The real writer must not leave temp files next to the metadata.
	s, path := newStore(t, "web")
	if _, err := s.Create("Prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("second save over an existing file must work: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "workspaces.json" {
			t.Errorf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestAtomicWriteFailureKeepsTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspaces.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory where the temp file's parent should be writable is the
	// deterministic failure: make dir read-only is unreliable on Windows, so
	// point the writer at a path whose parent is a file.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeFileAtomic(filepath.Join(blocker, "workspaces.json"), []byte("new"))
	if err == nil {
		t.Fatal("writing under a regular file should fail")
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Errorf("unrelated file touched: %s", got)
	}
}

func TestDefaultPathIsUnderUserConfigDir(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Skip("no user config dir on this platform:", err)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, dir) || filepath.Base(p) != "workspaces.json" || filepath.Base(filepath.Dir(p)) != "elyms" {
		t.Errorf("DefaultPath = %q, want <config dir>/elyms/workspaces.json", p)
	}
}
