package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
	"elyms/internal/ssh"
	"elyms/internal/ui/views"
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

// writeMeta writes the workspace metadata file the way a previous run would
// have left it.
func (e *wsEnv) writeMeta(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(e.wsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.wsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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

// reloadConfig re-reads the SSH config the way a host edit does.
func (m Model) reloadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := m.reload()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestEditRenameKeepsHostWorkspace covers renaming a host inside elyms while
// it is assigned: the assignment follows the new alias and the list is not
// left stranded in an empty workspace.
func TestEditRenameKeepsHostWorkspace(t *testing.T) {
	e, m := newWSEnv(t, "Host web\n  HostName 10.0.0.1\n\nHost *\n  Compression yes\n")
	m = createAndLeave(m, "Dev")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter") // web -> Dev, back in the list
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "enter") // enter Dev
	if got := m.list.Workspace(); got != "Dev" {
		t.Fatalf("context = %q", got)
	}
	m = key(m, "e")
	m = key(m, "backspace")
	m = key(m, "backspace")
	m = key(m, "backspace") // erase "web"
	m = typ(m, "web2")
	m = key(m, "enter")
	if m.mode != modeList {
		t.Fatalf("saving the edit should return to the list, mode = %v\n%s", m.mode, m.View())
	}
	if strings.Contains(m.View(), "No hosts in") {
		t.Errorf("the renamed host should still be visible in Dev:\n%s", m.View())
	}
	if len(m.list.Visible()) != 1 || m.list.Visible()[0].Alias != "web2" {
		var got []string
		for _, h := range m.list.Visible() {
			got = append(got, h.Alias)
		}
		t.Errorf("visible in Dev = %v, want [web2]", got)
	}
	if got := e.meta().Assignments["web2"]; got != "Dev" {
		t.Errorf("assignment should follow the rename, metadata = %+v", e.meta())
	}
}

// TestDeleteLastHostInWorkspaceLeavesEmptyState covers deleting the only host
// of the active workspace: the list still shows the empty workspace and w
// still gets the user back to All.
func TestDeleteLastHostInWorkspaceLeavesEmptyState(t *testing.T) {
	_, m := newWSEnv(t, "Host web\n  HostName 10.0.0.1\n\nHost *\n  Compression yes\n")
	m = createAndLeave(m, "Dev")
	m = key(m, "g")
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "enter") // in Dev
	m = key(m, "d")
	m = key(m, "y")
	if !strings.Contains(m.View(), "No hosts in Dev") {
		t.Errorf("the empty workspace should say so:\n%s", m.View())
	}
	m = key(m, "w")
	if got := m.list.Workspace(); got != "" {
		t.Fatalf("w should leave the empty workspace, context = %q", got)
	}
	// Leaving an empty workspace lands somewhere visible: either a
	// confirmation on the list or the manager itself.
	if m.mode != modeWorkspace && !strings.Contains(m.View(), "Showing all hosts") {
		t.Errorf("w should confirm leaving or open the manager, mode = %v status = %q", m.mode, m.status)
	}
}

func TestRefreshNeverMovesPatternAssignments(t *testing.T) {
	e, m := newWSEnv(t, "Host web\n  HostName 10.0.0.1\n\nHost *\n  Compression yes\n")
	e.writeMeta(t, `{"version":1,"workspaces":["Dev"],"assignments":{"*":"Dev"}}`)
	m = openWS(t, e.env, e.wsPath)
	if got := m.list.WorkspaceOf("*"); got != "Dev" {
		t.Fatalf("setup: * should start in Dev, got %q", got)
	}
	if err := os.WriteFile(e.cfgPath, []byte("Host web\n  HostName 10.0.0.1\n\nHost *\n  Compression yes\n\nHost *.internal\n  User ops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.refreshWorkspaces(m.reloadConfig(t))
	if got := m.ws.WorkspaceOf("*"); got != "Dev" {
		t.Errorf("* should keep its own assignment, got %q", got)
	}
	if got := m.ws.WorkspaceOf("*.internal"); got != "" {
		t.Errorf("a new pattern must not inherit another pattern's workspace, got %q", got)
	}
}

func TestRefreshDoesNotGuessForIdentityLessHosts(t *testing.T) {
	e, m := newWSEnv(t, "Host alpha\n\nHost beta\n  HostName 10.0.0.9\n")
	e.writeMeta(t, `{"version":1,"workspaces":["Dev","Prod"],"assignments":{"alpha":"Dev","beta":"Prod"}}`)
	m = openWS(t, e.env, e.wsPath)
	// A rename that changes both the alias and the identity: no host in the
	// new config can be identified as the old one.
	if err := os.WriteFile(e.cfgPath, []byte("Host gamma\n  HostName 10.0.0.7\n\nHost beta\n  HostName 10.0.0.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.refreshWorkspaces(m.reloadConfig(t))
	if got := m.ws.WorkspaceOf("gamma"); got != "" {
		t.Errorf("an unrelated host must not inherit alpha's workspace, got %q", got)
	}
	if got := m.ws.WorkspaceOf("beta"); got != "Prod" {
		t.Errorf("an unchanged assignment must stay put, got %q", got)
	}
}

func TestMalformedMetadataIsRecheckedOnHostEdit(t *testing.T) {
	e, _ := newEnv(t, cfgSample)
	dir := filepath.Join(t.TempDir(), "elyms")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "workspaces.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := openWS(t, e, bad)
	if !m.statusErr {
		t.Fatalf("setup: the broken file should be reported, status = %q", m.status)
	}
	// The user fixes the file while elyms is running.
	if err := os.WriteFile(bad, []byte(`{"version":1,"workspaces":["Dev"],"assignments":{"web":"Dev"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m = key(m, "d")
	m = key(m, "y") // delete web: reloads the config and the metadata
	if m.statusErr {
		t.Errorf("a repaired file should clear the error, status = %q", m.status)
	}
	if m.ws == nil {
		t.Fatalf("the repaired metadata should be in use")
	}
	// The next save works on the repaired file instead of refusing.
	m = key(m, "w")
	m = key(m, "n")
	m = typ(m, "Ops")
	m = key(m, "enter")
	var f workspaceFile
	if b, err := os.ReadFile(bad); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Workspaces) != 2 || f.Workspaces[1] != "Ops" {
		t.Errorf("metadata = %+v, want the repaired file plus Ops", f)
	}
}

func TestWOpensManagerWhileSearchIsApplied(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "enter") // switch to Dev
	m = key(m, "g")     // assign the selected host to Dev
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "/")
	m = typ(m, "db")
	m = key(m, "enter")
	if !m.list.HasFilter() || m.list.Workspace() != "Dev" {
		t.Fatalf("setup: workspace=%q filter=%v", m.list.Workspace(), m.list.HasFilter())
	}
	m = key(m, "w")
	if m.mode != modeWorkspace {
		t.Fatalf("w with an applied search should open the manager, mode = %v\n%s", m.mode, m.View())
	}
	if got := m.list.Workspace(); got != "" {
		t.Errorf("w should have left the workspace, context = %q", got)
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

// TestWInWorkspaceContextLeavesItThenOpensManager documents the interaction:
// inside a workspace context w goes back to All (opening the manager too when
// the workspace has nothing to show, as Dev does here), and a second w opens
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
	if m.mode != modeWorkspace {
		t.Errorf("an empty workspace should open the manager on leave, mode = %v", m.mode)
	}
	m = key(m, "esc")
	m = key(m, "w")
	if m.mode != modeWorkspace {
		t.Fatalf("w from All should open the manager, mode = %v", m.mode)
	}
}

func TestWInPopulatedWorkspaceOnlyLeaves(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "g") // web -> Dev
	m = key(m, "j")
	m = key(m, "enter")
	m = key(m, "w")
	m = key(m, "j")
	m = key(m, "enter")
	if m.list.Workspace() != "Dev" || len(m.list.Visible()) != 1 {
		t.Fatalf("setup: ws = %q visible = %d", m.list.Workspace(), len(m.list.Visible()))
	}
	m = key(m, "w")
	if m.list.Workspace() != "" {
		t.Errorf("w should leave the workspace context, ws = %q", m.list.Workspace())
	}
	if m.mode != modeList {
		t.Errorf("leaving a populated workspace should just show All, mode = %v", m.mode)
	}
	if m.status != "Showing all hosts" {
		t.Errorf("leaving should be confirmed, status = %q", m.status)
	}
}

func TestManagerSettlesWhenMetadataVanishesUnderIt(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = createAndLeave(m, "Dev")
	m = key(m, "w")
	m = key(m, "j") // Dev
	// The metadata loses Dev while the manager is open (another elyms, an
	// editor): the cursor row no longer exists, so the next key settles it
	// on All rather than acting on a workspace that is gone.
	m.wsModal = m.wsModal.SetData(views.WorkspaceData{})
	m = key(m, "enter")
	if m.list.Workspace() != "" {
		t.Errorf("no vanished workspace should be entered, ws = %q", m.list.Workspace())
	}
	m = key(m, "w")
	m = key(m, "enter")
	if m.mode != modeList || m.list.Workspace() != "" {
		t.Errorf("after settling, enter on All should leave the manager, mode = %v ws = %q",
			m.mode, m.list.Workspace())
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

// TestRightPaneShowsWelcomeWhenNothingIsSelected checks the empty-selection
// state, and that selecting a host goes back to the inspector.
func TestRightPaneShowsWelcomeWhenNothingIsSelected(t *testing.T) {
	_, m := newWSEnv(t, "")
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.View()
	for _, want := range []string{"TUI SSH manager", "byntangxyz", "Getting started"} {
		if !strings.Contains(v, want) {
			t.Errorf("empty list should show the welcome panel, missing %q:\n%s", want, v)
		}
	}
}

func TestRightPaneShowsInspectorForSelectedHost(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	v := m.View()
	if strings.Contains(v, "byntangxyz") {
		t.Errorf("a selected host should replace the welcome panel:\n%s", v)
	}
	if !strings.Contains(v, "Connects with") || !strings.Contains(v, "ssh web") {
		t.Errorf("inspector missing for the selected host:\n%s", v)
	}
}

// TestHintLineAdvertisesWorkspaceControls checks the hint bar names the// workspace keys in the list, and narrows to search hints while filtering.
func TestHintLineAdvertisesWorkspaceControls(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	hints := m.hintLine()
	for _, want := range []string{"workspaces", "assign"} {
		if !strings.Contains(hints, want) {
			t.Errorf("hint line missing %q: %q", want, hints)
		}
	}
	if !strings.Contains(hints, "quit") {
		t.Errorf("brand-new keys must not push out the existing ones: %q", hints)
	}

	// Inside a workspace context, w leaves it, so the hint says so.
	m = createWS(m, "Dev")
	m = key(m, "j")
	m = key(m, "enter")
	if hints := m.hintLine(); !strings.Contains(hints, "all hosts") {
		t.Errorf("a workspace context should advertise leaving it: %q", hints)
	}
}

func TestHintLineFilteringShowsOnlySearchHints(t *testing.T) {
	_, m := newWSEnv(t, cfgSample)
	m = key(m, "/")
	hints := m.hintLine()
	if !strings.Contains(hints, "apply") || !strings.Contains(hints, "clear") {
		t.Errorf("filtering hints = %q", hints)
	}
	if strings.Contains(hints, "workspaces") {
		t.Errorf("workspace keys cannot run while typing a query: %q", hints)
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
