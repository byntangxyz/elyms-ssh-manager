package views

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

func wsData() WorkspaceData {
	return WorkspaceData{
		Names:       []string{"Dev", "Prod"},
		Assignments: map[string]string{"web": "Dev", "db": "Prod"},
		Selected:    "web",
	}
}

func sendWS(m WorkspaceModal, msg tea.Msg) WorkspaceModal {
	next, _ := m.Update(msg)
	return next
}

// typeWS feeds text one key at a time, answering the textinput's cursor-blink
// commands so the keystrokes land like they do in the running program.
func typeWS(m WorkspaceModal, s string) WorkspaceModal {
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = m.Update(runes(string(r)))
		for cmd != nil {
			out := cmd()
			if _, blink := out.(cursor.BlinkMsg); !blink {
				break
			}
			m, cmd = m.Update(out)
		}
	}
	return m
}

func submitWS(m WorkspaceModal) tea.Msg {
	_, cmd := m.Update(special(tea.KeyEnter))
	return msgOf(cmd)
}

func TestWorkspaceManagerListsAllAndWorkspaces(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	view := m.View(68, 24)
	for _, want := range []string{"Workspaces", "All (2)", "Dev (1 host)", "Prod (1 host)"} {
		if !strings.Contains(view, want) {
			t.Errorf("manager missing %q:\n%s", want, view)
		}
	}
	for _, want := range []string{"enter", "n", "r", "d", "esc"} {
		if !strings.Contains(view, want) {
			t.Errorf("manager hints missing %q:\n%s", want, view)
		}
	}
	// At the narrowest modal width the hints are clipped, never wrapped.
	narrow := m.View(44, 12)
	if n := lines(narrow); n > 12 {
		t.Errorf("narrow manager must not wrap: %d lines\n%s", n, narrow)
	}
}

func TestWorkspaceManagerModes(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	if m.Mode() != WorkspaceModeManage {
		t.Fatalf("mode = %v", m.Mode())
	}
	if got := sendWS(m, runes("n")).Mode(); got != WorkspaceModeCreate {
		t.Errorf("n should switch to create, got %v", got)
	}
	onWS := sendWS(m, runes("j"))
	if got := sendWS(onWS, runes("r")).Mode(); got != WorkspaceModeRename {
		t.Errorf("r on a workspace should switch to rename, got %v", got)
	}
	if got := sendWS(onWS, runes("d")).Mode(); got != WorkspaceModeConfirmDelete {
		t.Errorf("d on a workspace should ask for confirmation, got %v", got)
	}
	if got := sendWS(m, runes("esc")).Mode(); got != WorkspaceModeManage {
		t.Errorf("esc in manage should close, got %v", got)
	}
}

func TestWorkspaceManagerNavigateSkipsNothingButStaysInBounds(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	m = sendWS(m, runes("j"))
	if got, ok := m.CursorName(); !ok || got != "Dev" {
		t.Errorf("cursor = %q %v", got, ok)
	}
	m = sendWS(m, runes("j"))
	m = sendWS(m, runes("j"))
	m = sendWS(m, runes("j"))
	if got, _ := m.CursorName(); got != "Prod" {
		t.Errorf("cursor must clamp on the last workspace, got %q", got)
	}
	for i := 0; i < 5; i++ {
		m = sendWS(m, runes("k"))
	}
	if m.CursorIsAll() != true {
		t.Errorf("k must clamp on All")
	}
}

func TestWorkspaceManagerEnterSwitchesContext(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	got := submitWS(m)
	sel, ok := got.(WorkspaceSelectMsg)
	if !ok || sel.Name != "" {
		t.Fatalf("enter on All should send WorkspaceSelectMsg{\"\"}, got %#v", got)
	}
	m = sendWS(m, runes("j"))
	got = submitWS(m)
	sel, ok = got.(WorkspaceSelectMsg)
	if !ok || sel.Name != "Dev" {
		t.Fatalf("enter on Dev should select it, got %#v", got)
	}
}

func TestWorkspaceManagerEscCloses(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	_, cmd := m.Update(special(tea.KeyEsc))
	if _, ok := msgOf(cmd).(WorkspaceModalCloseMsg); !ok {
		t.Fatalf("esc should close the manager, got %#v", msgOf(cmd))
	}
}

func TestWorkspaceCreateTypingAndSubmit(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("n"))
	if m.Mode() != WorkspaceModeCreate {
		t.Fatal("create mode")
	}
	m = typeWS(m, "  Staging  ")
	got := submitWS(m)
	create, ok := got.(WorkspaceCreateMsg)
	if !ok || create.Name != "Staging" {
		t.Fatalf("create should send the trimmed name, got %#v", got)
	}
}

func TestWorkspaceCreateEnterOnEmptyNameIsRefusedLocally(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("n"))
	m = typeWS(m, "   ")
	next, cmd := m.Update(special(tea.KeyEnter))
	if msg := msgOf(cmd); msg != nil {
		t.Errorf("empty name must not be submitted, got %#v", msg)
	}
	if !strings.Contains(next.View(70, 24), "required") {
		t.Errorf("empty name should show a hint:\n%s", next.View(70, 24))
	}
}

func TestWorkspaceCreateEscReturnsToManage(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("n"))
	m = sendWS(m, special(tea.KeyEsc))
	if m.Mode() != WorkspaceModeManage {
		t.Errorf("esc in create should return to the manager, got %v", m.Mode())
	}
}

func TestWorkspaceFormErrorIsShownAndKept(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("n"))
	m = typeWS(m, "Dev")
	m = m.SetErr(errors.New("a workspace with this name already exists"))
	view := m.View(70, 24)
	if !strings.Contains(view, "already exists") {
		t.Errorf("error should be visible:\n%s", view)
	}
	if !strings.Contains(view, "Dev") {
		t.Errorf("typed text should survive the error:\n%s", view)
	}
}

func TestWorkspaceRenamePrefillsAndSubmits(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("j")) // Dev
	m = sendWS(m, runes("r"))
	if m.Mode() != WorkspaceModeRename {
		t.Fatal("rename mode")
	}
	if !strings.Contains(m.View(70, 24), "Dev") {
		t.Errorf("rename should prefill the current name:\n%s", m.View(70, 24))
	}
	m = typeWS(m, "elopment")
	got := submitWS(m)
	rn, ok := got.(WorkspaceRenameMsg)
	if !ok || rn.OldName != "Dev" || rn.NewName != "Development" {
		t.Fatalf("rename msg = %#v", got)
	}
}

func TestWorkspaceRenameOnAllOrMissingWorkspaceDoesNothing(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	if got := sendWS(m, runes("r")).Mode(); got != WorkspaceModeManage {
		t.Errorf("rename on All should stay in the manager, got %v", got)
	}
	empty := NewWorkspaceModal(WorkspaceData{})
	if got := sendWS(empty, runes("r")).Mode(); got != WorkspaceModeManage {
		t.Errorf("rename with no workspaces should stay in the manager, got %v", got)
	}
}

func TestWorkspaceDeleteAsksBeforeSending(t *testing.T) {
	m := sendWS(NewWorkspaceModal(wsData()), runes("j")) // Dev
	m = sendWS(m, runes("d"))
	view := m.View(70, 24)
	if !strings.Contains(view, "Dev") || !strings.Contains(view, "Unassigned") {
		t.Errorf("delete should name the workspace and explain hosts become unassigned:\n%s", view)
	}
	if cancel := sendWS(m, runes("n")); cancel.Mode() != WorkspaceModeManage {
		t.Errorf("n must cancel the delete and stay in the manager")
	}
	m = sendWS(m, runes("d"))
	_, cmd := m.Update(runes("y"))
	del, ok := msgOf(cmd).(WorkspaceDeleteMsg)
	if !ok || del.Name != "Dev" {
		t.Fatalf("y should send the delete, got %#v", msgOf(cmd))
	}
}

func TestWorkspaceDeleteOnAllIsRefused(t *testing.T) {
	m := NewWorkspaceModal(wsData())
	if got := sendWS(m, runes("d")).Mode(); got != WorkspaceModeManage {
		t.Errorf("cannot delete All, mode = %v", got)
	}
}

func TestWorkspaceAssignModalOffersUnassignedAndEveryWorkspace(t *testing.T) {
	m := NewWorkspaceModal(WorkspaceData{
		Names:       []string{"Dev"},
		Assignments: map[string]string{"web": "Dev", "db": "Prod"},
		Target:      "db",
		Selected:    "db",
	})
	if m.Mode() != WorkspaceModeAssign || m.AssignMode() != AssignHost {
		t.Fatalf("mode = %v assign = %v", m.Mode(), m.AssignMode())
	}
	view := m.View(70, 24)
	for _, want := range []string{"Assign db", "Unassigned", "Dev"} {
		if !strings.Contains(view, want) {
			t.Errorf("assign modal missing %q:\n%s", want, view)
		}
	}
	// db sits in Prod, which is not in Names: the current workspace shows as
	// an explicit choice rather than a dangling assignment.
	if !strings.Contains(view, "Prod") {
		t.Errorf("current workspace should be offered:\n%s", view)
	}
}

func TestWorkspaceAssignEnterSendsAssignment(t *testing.T) {
	m := NewWorkspaceModal(WorkspaceData{Names: []string{"Dev", "Prod"}, Target: "web"})
	got := submitWS(m)
	assign, ok := got.(WorkspaceAssignMsg)
	if !ok || assign.Alias != "web" || assign.Name != "" {
		t.Fatalf("first choice is Unassigned, got %#v", got)
	}
	m = sendWS(m, runes("j"))
	got = submitWS(m)
	assign, ok = got.(WorkspaceAssignMsg)
	if !ok || assign.Alias != "web" || assign.Name != "Dev" {
		t.Fatalf("second choice is Dev, got %#v", got)
	}
}

func TestWorkspaceAssignEscClosesAssign(t *testing.T) {
	m := NewWorkspaceModal(WorkspaceData{Names: []string{"Dev"}, Target: "web"})
	m = sendWS(m, runes("w")) // no-op in assign mode
	if m.Mode() != WorkspaceModeAssign {
		t.Errorf("w should not leave the assign modal")
	}
	_, cmd := m.Update(special(tea.KeyEsc))
	out := msgOf(cmd)
	if c, ok := out.(WorkspaceAssignCancelMsg); !ok || c.Alias != "web" {
		t.Fatalf("esc should cancel the assignment for web, got %#v", out)
	}
}

func TestWorkspaceAssignWithoutHostDoesNothing(t *testing.T) {
	m := NewWorkspaceModal(WorkspaceData{Names: []string{"Dev"}})
	if m.Mode() != WorkspaceModeManage || m.AssignMode() != AssignNone {
		t.Errorf("assign needs a host; mode = %v", m.Mode())
	}
}

func TestWorkspaceModalFitsMinimumTerminal(t *testing.T) {
	views := []WorkspaceModal{
		NewWorkspaceModal(wsData()),
		sendWS(NewWorkspaceModal(wsData()), runes("n")),
		sendWS(sendWS(NewWorkspaceModal(wsData()), runes("j")), runes("r")),
		sendWS(sendWS(NewWorkspaceModal(wsData()), runes("j")), runes("d")),
		NewWorkspaceModal(WorkspaceData{Names: []string{"Dev"}, Target: "web"}),
	}
	for i, mod := range views {
		for _, termW := range []int{120, 74, 50} {
			modalW := termW - 6
			if modalW > 76 {
				modalW = 76
			}
			v := mod.View(modalW, 12)
			if n := lines(v); n > 12 {
				t.Errorf("modal %d at terminal width %d: %d lines", i, termW, n)
			}
			for _, ln := range strings.Split(v, "\n") {
				if lw := lipglossWidth(ln); lw > modalW {
					t.Errorf("modal %d: line wider than %d: %d %q", i, modalW, lw, ln)
				}
			}
		}
	}
}

func TestWorkspaceManagerScrollsLongLists(t *testing.T) {
	var names []string
	for _, n := range strings.Split("Alpha Bravo Charlie Delta Echo Foxtrot Golf Hotel India Juliet", " ") {
		names = append(names, n)
	}
	m := NewWorkspaceModal(WorkspaceData{Names: names})
	for i := 0; i < len(names); i++ {
		m = sendWS(m, runes("j"))
	}
	v := m.View(70, 12)
	if n := lines(v); n > 12 {
		t.Errorf("modal height = %d", n)
	}
	if !strings.Contains(v, "Juliet") {
		t.Errorf("last workspace should be scrolled into view:\n%s", v)
	}
}
