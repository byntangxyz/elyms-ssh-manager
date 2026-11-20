package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/ssh"
	"elyms/internal/workspace"
)

// wsEnv is env plus a workspace metadata path outside the SSH directory, the
// way the real app keeps it.
type wsEnv struct {
	*env
	wsPath string
}

func newWSEnv(t *testing.T, content string) (*wsEnv, Model) {
	t.Helper()
	e, _ := newEnv(t, content)
	wsPath := filepath.Join(filepath.Dir(e.sshDir), "elyms", "workspaces.json")
	return &wsEnv{env: e, wsPath: wsPath}, openWS(t, e, wsPath)
}

// openWS starts a model over env's SSH config with workspace metadata at
// wsPath, used both for the first start and to simulate a reload.
func openWS(t *testing.T, e *env, wsPath string) Model {
	t.Helper()
	m, err := New(Options{
		ConfigPath:    e.cfgPath,
		SSHDir:        e.sshDir,
		WorkspacePath: wsPath,
		Connect: func(alias string) tea.Cmd {
			e.connected = append(e.connected, alias)
			return func() tea.Msg { return ssh.FinishedMsg{Alias: alias} }
		},
		Copy:         func(s string) error { e.copied = s; return nil },
		SSHAvailable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
}

// meta reads the workspace metadata file as raw JSON: version, names, and
// assignments.
func (e *wsEnv) meta() workspaceFile {
	e.t.Helper()
	b, err := os.ReadFile(e.wsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return workspaceFile{Assignments: map[string]string{}}
		}
		e.t.Fatal(err)
	}
	var f workspaceFile
	if err := json.Unmarshal(b, &f); err != nil {
		e.t.Fatalf("metadata is not JSON: %v\n%s", err, b)
	}
	if f.Assignments == nil {
		f.Assignments = map[string]string{}
	}
	return f
}

type workspaceFile struct {
	Version     int               `json:"version"`
	Workspaces  []string          `json:"workspaces"`
	Assignments map[string]string `json:"assignments"`
}

// createWS walks the manager: w, n, type the name, enter. The manager stays
// open afterwards, so callers add the key they need next.
func createWS(m Model, name string) Model {
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, name)
	m = key(m, "enter")
	return m
}

// createAndLeave creates a workspace and closes the manager, leaving the
// list in the context it had before.
func createAndLeave(m Model, name string) Model {
	return key(createWS(m, name), "esc")
}

func TestWOpensWorkspaceManager(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = key(m, "w")
	if m.mode != modeWorkspace {
		t.Fatalf("w should open the workspace manager, mode = %v", m.mode)
	}
	v := m.View()
	for _, want := range []string{"Workspaces", "All", "n", "new", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("manager missing %q:\n%s", want, v)
		}
	}
	m = key(m, "esc")
	if m.mode != modeList {
		t.Errorf("esc should return to the list")
	}
	m = key(m, "esc")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	_, quit := next.(Model)
	if cmd == nil || !quit {
		t.Errorf("q after closing the manager should still quit")
	}
}

func TestCreateWorkspacePersistsWithoutSwitching(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	if m.mode != modeList {
		t.Fatalf("after create+esc mode = %v, want list\n%s", m.mode, m.View())
	}
	if got := m.list.Workspace(); got != "" {
		t.Errorf("creating must not switch context, got %q", got)
	}
	if !strings.Contains(m.View(), "web") {
		t.Errorf("hosts should still be listed in All:\n%s", m.View())
	}
	f := e.meta()
	if f.Version != workspace.Version || len(f.Workspaces) != 1 || f.Workspaces[0] != "Dev" {
		t.Errorf("metadata = %+v", f)
	}
	if e.file() != cfgSample {
		t.Errorf("creating a workspace must not touch the SSH config:\n%s", e.file())
	}
}

func TestCreateWorkspaceThenSwitchExplicitly(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "j") // Dev in the manager list
	m = key(m, "enter")
	if got := m.list.Workspace(); got != "Dev" {
		t.Fatalf("enter on Dev should switch context, got %q", got)
	}
	if m.mode != modeList {
		t.Errorf("switching should close the manager, mode = %v", m.mode)
	}
}

func TestCreateWorkspaceKeepsManagerOpenForMore(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, "Prod")
	m = key(m, "enter")
	if m.mode != modeWorkspace {
		t.Fatalf("manager should stay open after create, mode = %v", m.mode)
	}
	if v := m.View(); !strings.Contains(v, "Prod") {
		t.Errorf("new workspace should be listed:\n%s", v)
	}
}

func TestCreateDuplicateWorkspaceShowsErrorAndKeepsTypedName(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc") // back to the list
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, "dev")
	m = key(m, "enter")
	if m.mode != modeWorkspace {
		t.Fatalf("mode = %v", m.mode)
	}
	v := m.View()
	if !strings.Contains(strings.ToLower(v), "already exists") {
		t.Errorf("duplicate should be explained:\n%s", v)
	}
	if !strings.Contains(v, "dev") {
		t.Errorf("the typed name should stay in the field:\n%s", v)
	}
	if f := e.meta(); len(f.Workspaces) != 1 {
		t.Errorf("metadata changed: %+v", f)
	}
}

func TestCreateWorkspaceWithoutNameExplains(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = key(m, "w")
	m = key(m, "n")
	m = key(m, "enter")
	if v := m.View(); !strings.Contains(v, "required") {
		t.Errorf("empty name should be explained:\n%s", v)
	}
	if f := e.meta(); len(f.Workspaces) != 0 {
		t.Errorf("nothing should be stored: %+v", f)
	}
}

// TestWInWorkspaceContextLeavesItThenOpensManager documents the one-key
// design: inside a workspace context w goes back to All, and a second w opens
// the manager.
func TestWInWorkspaceContextLeavesItThenOpensManager(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "j") // Dev
	m = key(m, "enter")
	if m.list.Workspace() != "Dev" {
		t.Fatalf("setup: ws = %q", m.list.Workspace())
	}
	m = key(m, "w")
	if m.list.Workspace() != "" {
		t.Errorf("w should leave the workspace context, ws = %q", m.list.Workspace())
	}
	if m.mode != modeList {
		t.Errorf("leaving the context should not open a modal, mode = %v", m.mode)
	}
	m = key(m, "w")
	if m.mode != modeWorkspace {
		t.Fatalf("w from All should open the manager, mode = %v", m.mode)
	}
}

func TestSelectAllFromManagerClearsContext(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "w")
	m = key(m, "g") // home = All
	m = key(m, "enter")
	if m.mode != modeList || m.list.Workspace() != "" {
		t.Fatalf("All should clear the context, mode = %v ws = %q", m.mode, m.list.Workspace())
	}
}

func TestSelectWorkspaceFromManagerNarrowsList(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "w")
	m = key(m, "g")
	m = key(m, "j") // Dev
	m = key(m, "enter")
	v := m.View()
	if m.list.Workspace() != "Dev" {
		t.Fatalf("ws = %q", m.list.Workspace())
	}
	if !strings.Contains(v, "No hosts in Dev") {
		t.Errorf("empty workspace should be explained:\n%s", v)
	}
}

func TestGAssignsSelectedHost(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "g")
	if m.mode != modeWorkspace {
		t.Fatalf("g should open the assign chooser, mode = %v", m.mode)
	}
	m = key(m, "j") // Dev
	m = key(m, "enter")
	if got := e.meta().Assignments["web"]; got != "Dev" {
		t.Errorf("assignment = %q, want Dev", got)
	}
	if e.file() != cfgSample {
		t.Errorf("assigning must not touch the SSH config")
	}
}

func TestGOnPatternAssignsNothing(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "G")
	sel := m.list.Selected()
	if sel == nil || !sel.IsPattern {
		t.Fatalf("expected the pattern row, got %#v", sel)
	}
	m = key(m, "g")
	if m.mode != modeList {
		t.Errorf("a pattern must not open the assign chooser")
	}
	if !strings.Contains(strings.ToLower(m.View()), "pattern") {
		t.Errorf("should explain why:\n%s", m.View())
	}
	if f := e.meta(); len(f.Assignments) != 0 {
		t.Errorf("patterns are never assigned: %+v", f)
	}
}

func TestPatternStaysVisibleButUnassignable(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "w")
	m = key(m, "j") // Dev
	m = key(m, "enter")
	if v := m.View(); strings.Contains(v, "*") {
		t.Errorf("the pattern is unassigned, so Dev should hide it:\n%s", v)
	}
	m = key(m, "w")
	m = key(m, "g")
	m = key(m, "enter")
	if v := m.View(); !strings.Contains(v, "pattern") {
		t.Errorf("the pattern stays visible in All:\n%s", v)
	}
}

func TestGWithoutSelectionDoesNothing(t *testing.T) {
	_, m := newWSEnv(t, "")
	m = key(m, "g")
	if m.mode != modeList {
		t.Errorf("nothing to assign; mode = %v", m.mode)
	}
}

func TestAssignToUnassignedClearsAssignment(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	if e.meta().Assignments["web"] != "Dev" {
		t.Fatal("setup failed")
	}
	m = key(m, "g") // chooser starts on the current workspace
	m = key(m, "k") // Unassigned comes first
	m = key(m, "enter")
	if _, ok := e.meta().Assignments["web"]; ok {
		t.Errorf("Unassigned should clear the assignment: %+v", e.meta().Assignments)
	}
}

func TestAssignCancelChangesNothing(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "g")
	m = key(m, "esc")
	if m.mode != modeList {
		t.Errorf("esc should close the chooser, mode = %v", m.mode)
	}
	if len(e.meta().Assignments) != 0 {
		t.Errorf("cancel must not assign")
	}
}

func TestAssignedHostIsGroupedInAllAndNamedView(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "w")
	m = key(m, "g") // All
	m = key(m, "enter")
	v := m.View()
	if !strings.Contains(v, "Dev (1)") || !strings.Contains(v, "Unassigned (2)") {
		t.Errorf("All should group by workspace:\n%s", v)
	}
	if strings.Index(v, "Dev (1)") > strings.Index(v, "Unassigned (2)") {
		t.Errorf("named sections come before Unassigned:\n%s", v)
	}
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "enter")
	v = m.View()
	if strings.Contains(v, "db") {
		t.Errorf("named view should only show its hosts:\n%s", v)
	}
}

func TestRenameWorkspaceKeepsAssignments(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "w")
	m = key(m, "j") // Dev
	m = key(m, "r")
	for i := 0; i < len("Dev"); i++ {
		m = key(m, "backspace")
	}
	m = typ(m, "Work")
	m = key(m, "enter")
	f := e.meta()
	if len(f.Workspaces) != 1 || f.Workspaces[0] != "Work" {
		t.Fatalf("metadata = %+v", f)
	}
	if f.Assignments["web"] != "Work" {
		t.Errorf("assignment should follow the rename: %+v", f.Assignments)
	}
	if e.file() != cfgSample {
		t.Errorf("rename must not touch the SSH config")
	}
}

// TestWorkspaceDisappearingFallsBackToAll guards the list when the metadata
// loses the active workspace while the list is rendering it.
func TestWorkspaceDisappearingFallsBackToAll(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m.list.SetWorkspace("Dev")
	m.list.SetWorkspaceData(nil, nil)
	if got := m.list.Workspace(); got != "" {
		t.Errorf("a vanished workspace should fall back to All, got %q", got)
	}
}

func TestRenameToExistingNameIsRefused(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createWS(m, "Dev")
	m = key(m, "esc")
	m = createWS(m, "Prod")
	m = key(m, "esc")
	m = key(m, "w")
	m = key(m, "j") // Dev
	m = key(m, "r")
	for i := 0; i < len("Dev"); i++ {
		m = key(m, "backspace")
	}
	m = typ(m, "prod")
	m = key(m, "enter")
	if v := m.View(); !strings.Contains(strings.ToLower(v), "already exists") {
		t.Errorf("duplicate rename should be explained:\n%s", v)
	}
	if f := e.meta(); len(f.Workspaces) != 2 {
		t.Errorf("metadata changed: %+v", f)
	}
}

func TestDeleteWorkspaceUnassignsAndLeavesHosts(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "d")
	if v := m.View(); !strings.Contains(v, "Delete") || !strings.Contains(v, "Unassigned") {
		t.Errorf("delete should explain hosts become Unassigned:\n%s", v)
	}
	m = key(m, "n")
	if f := e.meta(); len(f.Workspaces) != 1 {
		t.Errorf("n must keep the workspace: %+v", f)
	}
	m = key(m, "d")
	m = key(m, "y")
	f := e.meta()
	if len(f.Workspaces) != 0 || len(f.Assignments) != 0 {
		t.Errorf("delete should clear the workspace and its assignments: %+v", f)
	}
	if e.file() != cfgSample {
		t.Errorf("deleting a workspace must never touch the SSH config:\n%s", e.file())
	}
	if m.list.Workspace() != "" {
		t.Errorf("deleting the active workspace should fall back to All")
	}
	m = key(m, "esc") // close the manager to see the list
	if !strings.Contains(m.View(), "Unassigned (3)") {
		t.Errorf("every host should be back under Unassigned:\n%s", m.View())
	}
}

// TestWorkspaceFailureKeepsState uses an unreadable path so every save fails.
func TestWorkspaceFailureKeepsState(t *testing.T) {
	e, _ := newEnv(t, cfgSample)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A regular file where the metadata directory should be.
	m := openWS(t, e, filepath.Join(blocker, "workspaces.json"))
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, "Dev")
	m = key(m, "enter")
	if m.mode != modeWorkspace {
		t.Fatalf("a failed save should keep the manager open")
	}
	if !m.statusErr || !strings.Contains(strings.ToLower(m.status), "save") {
		t.Errorf("failure should be a visible error, status = %q", m.status)
	}
	if names := m.list.Workspaces(); len(names) != 0 {
		t.Errorf("failed create must not change the visible state: %v", names)
	}
}

func TestMalformedMetadataIsReportedAndSSHConfigUntouched(t *testing.T) {
	e, _ := newEnv(t, cfgSample)
	dir := filepath.Join(t.TempDir(), "elyms")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "workspaces.json")
	if err := os.WriteFile(bad, []byte(`{"version":99,"workspaces":["X"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := openWS(t, e, bad)
	if v := m.View(); !strings.Contains(strings.ToLower(v), "metadata") {
		t.Errorf("bad metadata should be reported:\n%s", v)
	}
	if !strings.Contains(m.View(), "web") {
		t.Errorf("hosts must still be listed:\n%s", m.View())
	}
	m = key(m, "a")
	m = typ(m, "new-host")
	m = key(m, "enter")
	if !strings.Contains(e.file(), "Host new-host") {
		t.Errorf("host editing must keep working with bad metadata:\n%s", e.file())
	}
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, "Dev")
	m = key(m, "enter")
	if m.mode != modeWorkspace || !strings.Contains(strings.ToLower(m.status), "metadata") {
		t.Errorf("saving over unreadable metadata should be refused, status = %q", m.status)
	}
	if b, _ := os.ReadFile(bad); !strings.Contains(string(b), `"version":99`) {
		t.Errorf("the newer metadata file must not be overwritten:\n%s", b)
	}
}

func TestStaleAssignmentIsDroppedOnReload(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	// web disappears from the SSH config while elyms is running.
	if err := os.WriteFile(e.cfgPath, []byte("Host db\n  HostName 10.0.0.2\n\nHost *\n  Compression yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = key(m, "d") // deleting a vanished host reloads the config
	m = key(m, "y")
	v := m.View()
	if strings.Contains(v, "ssh web") {
		t.Errorf("web should be gone:\n%s", v)
	}
	if strings.Contains(v, "Dev (1)") {
		t.Errorf("the stale Dev section should not claim a host:\n%s", v)
	}
	if got := e.meta().Assignments["web"]; got != "Dev" {
		t.Errorf("reloading must not rewrite metadata, got %q", got)
	}
	if len(e.connected) != 0 {
		t.Errorf("a stale assignment must never connect: %v", e.connected)
	}
}

// TestWorkspaceLifecycleNeverTouchesSSHConfig walks every workspace operation
// and asserts the SSH config bytes are identical at each step.
func TestWorkspaceLifecycleNeverTouchesSSHConfig(t *testing.T) {
	e, m := newWSEnv(t, cfgSample)
	check := func(step string) {
		t.Helper()
		if got := e.file(); got != cfgSample {
			t.Fatalf("%s changed the SSH config:\n%s", step, got)
		}
	}
	m = createAndLeave(m, "Dev")
	check("create")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	check("assign")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "r")
	for i := 0; i < len("Dev"); i++ {
		m = key(m, "backspace")
	}
	m = typ(m, "Work")
	m = key(m, "enter")
	check("rename")
	m = key(m, "esc")
	m = key(m, "g")
	m = key(m, "k") // Unassigned
	m = key(m, "enter")
	check("unassign")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "d")
	m = key(m, "y")
	check("delete")
	if f := e.meta(); len(f.Workspaces) != 0 || len(f.Assignments) != 0 {
		t.Errorf("metadata should end empty: %+v", f)
	}
}

func TestWorkspaceModalFitsMinimumTerminal(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = send(m, tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	m = key(m, "w")
	for _, open := range []func(Model) Model{
		func(m Model) Model { return m },
		func(m Model) Model { return key(m, "n") },
	} {
		v := open(m).View()
		rows := strings.Split(v, "\n")
		if len(rows) > minHeight {
			t.Errorf("%d rows at %d:\n%s", len(rows), minHeight, v)
		}
		for _, r := range rows {
			if w := lipglossWidth(r); w > minWidth {
				t.Errorf("row width %d: %q", w, r)
			}
		}
	}
}
