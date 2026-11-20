package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"elyms/internal/ui/styles"
)

// Workspace messages travel from the modal to the root model, which owns all
// persistence. A view never saves.
type (
	// WorkspaceCreateMsg asks for a new workspace with Name.
	WorkspaceCreateMsg struct{ Name string }
	// WorkspaceRenameMsg asks to rename OldName to NewName.
	WorkspaceRenameMsg struct{ OldName, NewName string }
	// WorkspaceDeleteMsg asks to remove a workspace; its hosts stay in the
	// SSH config and become unassigned.
	WorkspaceDeleteMsg struct{ Name string }
	// WorkspaceSelectMsg switches context; an empty Name means All.
	WorkspaceSelectMsg struct{ Name string }
	// WorkspaceAssignMsg puts Alias in workspace Name; an empty Name
	// unassigns it.
	WorkspaceAssignMsg struct{ Alias, Name string }
	// WorkspaceAssignCancelMsg leaves the assignment modal without a change.
	WorkspaceAssignCancelMsg struct{ Alias string }
	// WorkspaceModalCloseMsg leaves the workspace manager.
	WorkspaceModalCloseMsg struct{}
)

// WorkspaceData is everything the modal needs to render itself.
type WorkspaceData struct {
	Names       []string          // workspaces in display order
	Assignments map[string]string // alias -> workspace
	Target      string            // when set, the modal opens in assign mode for it
}

// WorkspaceMode is which panel of the modal is showing.
type WorkspaceMode int

const (
	WorkspaceModeManage WorkspaceMode = iota
	WorkspaceModeCreate
	WorkspaceModeRename
	WorkspaceModeConfirmDelete
	WorkspaceModeAssign
)

// AssignTarget is what an assign modal is assigning.
type AssignTarget int

const (
	AssignNone AssignTarget = iota
	AssignHost
	AssignPattern
)

// minModalWidth is the narrowest width styles.Modal renders without wrapping.
const minModalWidth = 50

const (
	wsManage = iota
	wsCreate
	wsRename
)

// WorkspaceModal manages workspaces and host assignments.
type WorkspaceModal struct {
	data    WorkspaceData
	mode    WorkspaceMode
	cursor  int
	offset  int
	input   textinput.Model
	err     string
	target  string
	pending tea.Msg // a typed action decided by the last keystroke
}

// NewWorkspaceModal opens on the workspace manager, or on the assign chooser
// when data.Target names a host.
func NewWorkspaceModal(data WorkspaceData) WorkspaceModal {
	m := WorkspaceModal{data: data, input: wsInput()}
	if data.Target != "" {
		m.mode = WorkspaceModeAssign
		m.target = data.Target
		m.cursor = m.currentAssignChoice()
	}
	return m
}

// TakeAction returns the action the last keystroke decided on, if any. The
// modal owns key handling, so the action is available synchronously and the
// root model never has to run the returned command (a text-input cursor-blink
// timer would stall the update loop).
func (m *WorkspaceModal) TakeAction() (tea.Msg, bool) {
	msg := m.pending
	m.pending = nil
	if msg == nil {
		return nil, false
	}
	return msg, true
}

// decide records the action msg as the modal's pending action and returns the
// matching command, so callers can use the command or TakeAction alike.
func (m WorkspaceModal) decide(msg tea.Msg) (WorkspaceModal, tea.Cmd) {
	m.pending = msg
	return m, func() tea.Msg { return msg }
}

func wsInput() textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 64
	in.Width = 40
	return in
}

func (m WorkspaceModal) Mode() WorkspaceMode { return m.mode }
func (m WorkspaceModal) Err() string         { return m.err }

// SetErr shows a persistence or validation error without closing the modal,
// keeping whatever the user typed.
func (m WorkspaceModal) SetErr(err error) WorkspaceModal {
	if err != nil {
		m.err = err.Error()
	}
	return m
}

// Acknowledge closes a create or rename form after the root model saved it, so
// the manager is back on screen for the next action. Other panels are left
// alone.
func (m WorkspaceModal) Acknowledge() WorkspaceModal {
	if m.mode == WorkspaceModeCreate || m.mode == WorkspaceModeRename {
		m, _ = m.backToManage()
	}
	return m
}

// SetData replaces the workspace list and assignments the modal renders. The
// cursor stays where it is unless the list shrank.
func (m WorkspaceModal) SetData(data WorkspaceData) WorkspaceModal {
	data.Target = m.target
	m.data = data
	if m.cursor >= m.managerChoices() {
		m.cursor = m.managerChoices() - 1
	}
	return m
}

// CursorIsAll reports whether All is highlighted in the manager.
func (m WorkspaceModal) CursorIsAll() bool { return m.cursor == 0 }

// CursorName returns the highlighted workspace; ok is false for All.
func (m WorkspaceModal) CursorName() (string, bool) {
	i := m.cursor - 1
	if i < 0 || i >= len(m.data.Names) {
		return "", false
	}
	return m.data.Names[i], true
}

// AssignMode says what the assign panel is assigning. Only hosts can be
// assigned, so it is AssignHost whenever a target is set.
func (m WorkspaceModal) AssignMode() AssignTarget {
	if m.mode == WorkspaceModeAssign || m.target != "" {
		return AssignHost
	}
	return AssignNone
}

// choices returns the assign chooser's entries: Unassigned first, then every
// workspace, then the host's current workspace when the metadata no longer
// lists it.
func (m WorkspaceModal) choices() []string {
	out := []string{""}
	out = append(out, m.data.Names...)
	cur := m.data.Assignments[m.target]
	if cur == "" {
		return out
	}
	for _, n := range m.data.Names {
		if n == cur {
			return out
		}
	}
	return append(out, cur)
}

func (m WorkspaceModal) currentAssignChoice() int {
	cur := m.data.Assignments[m.target]
	for i, c := range m.choices() {
		if c == cur {
			return i
		}
	}
	return 0
}

func (m WorkspaceModal) managerChoices() int { return len(m.data.Names) + 1 }

func (m WorkspaceModal) Update(msg tea.Msg) (WorkspaceModal, tea.Cmd) {
	m.pending = nil
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	return m.handle(k)
}

func (m WorkspaceModal) handle(k tea.KeyMsg) (WorkspaceModal, tea.Cmd) {
	switch m.mode {
	case WorkspaceModeAssign:
		return m.updateAssign(k)
	case WorkspaceModeCreate, WorkspaceModeRename:
		return m.updateForm(k)
	case WorkspaceModeConfirmDelete:
		return m.updateConfirm(k)
	}
	return m.updateManage(k)
}

func (m WorkspaceModal) setMode(mode WorkspaceMode) WorkspaceModal {
	m.mode = mode
	return m
}

func (m WorkspaceModal) clampCursor() WorkspaceModal {
	last := m.managerChoices() - 1
	if m.cursor > last {
		m.cursor = last
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m
}

func (m WorkspaceModal) updateManage(k tea.KeyMsg) (WorkspaceModal, tea.Cmd) {
	m = m.clampCursor()
	switch k.String() {
	case "esc", "q":
		return m.decide(WorkspaceModalCloseMsg{})
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor < m.managerChoices()-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = m.managerChoices() - 1
	case "enter":
		// All is always a choice; only a cursor left past the end by a
		// shrunk list has nothing to open.
		if m.CursorIsAll() {
			return m.setMode(WorkspaceModeManage).decide(WorkspaceSelectMsg{Name: ""})
		}
		name, ok := m.CursorName()
		if !ok {
			return m, nil
		}
		return m.setMode(WorkspaceModeManage).decide(WorkspaceSelectMsg{Name: name})
	case "n":
		return m.openForm(wsCreate, ""), nil
	case "r":
		if m.CursorIsAll() || len(m.data.Names) == 0 {
			return m, nil
		}
		name, _ := m.CursorName()
		return m.openForm(wsRename, name), nil
	case "d":
		if m.CursorIsAll() || len(m.data.Names) == 0 {
			return m, nil
		}
		m.mode, m.err = WorkspaceModeConfirmDelete, ""
	}
	return m, nil
}

func (m WorkspaceModal) openForm(kind int, prefill string) WorkspaceModal {
	m.mode = WorkspaceModeCreate
	if kind == wsRename {
		m.mode = WorkspaceModeRename
	}
	m.input = wsInput()
	m.input.SetValue(prefill)
	m.input.CursorEnd()
	m.input.Focus()
	m.err = ""
	return m
}

func (m WorkspaceModal) updateForm(k tea.KeyMsg) (WorkspaceModal, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		return m.backToManage()
	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.err = "workspace name is required"
			return m, nil
		}
		if m.mode == WorkspaceModeCreate {
			v := value
			return m.openForm(wsCreate, v).decide(WorkspaceCreateMsg{Name: v})
		}
		old, _ := m.CursorName()
		v := value
		return m.openForm(wsRename, v).decide(WorkspaceRenameMsg{OldName: old, NewName: v})
	case tea.KeyRunes:
		k.Runes = stripNewlines(k.Runes)
		if len(k.Runes) == 0 {
			return m, nil
		}
	}
	m.err = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m WorkspaceModal) backToManage() (WorkspaceModal, tea.Cmd) {
	m.mode, m.err = WorkspaceModeManage, ""
	return m, nil
}

func (m WorkspaceModal) updateConfirm(k tea.KeyMsg) (WorkspaceModal, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		m.mode = WorkspaceModeManage
		name, _ := m.CursorName()
		return m.decide(WorkspaceDeleteMsg{Name: name})
	case "n", "N", "esc", "enter":
		m.mode = WorkspaceModeManage
	}
	return m, nil
}

func (m WorkspaceModal) updateAssign(k tea.KeyMsg) (WorkspaceModal, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		alias := m.target
		return m.decide(WorkspaceAssignCancelMsg{Alias: alias})
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.choices())-1 {
			m.cursor++
		}
	case "enter":
		ch := m.choices()
		if m.cursor < 0 || m.cursor >= len(ch) {
			return m, nil
		}
		alias, name := m.target, ch[m.cursor]
		return m.decide(WorkspaceAssignMsg{Alias: alias, Name: name})
	}
	return m, nil
}

// View renders the active panel inside the modal frame, keeping the frame at
// most width columns wide. Height is a soft bound: the manager and assign
// panels scroll, but forms and confirmations are never clipped.
func (m WorkspaceModal) View(width, height int) string {
	// styles.Modal frames max(width, 14) columns (border + padding), so the
	// body is padded to that inner width.
	frame := width
	if frame < 14 {
		frame = 14
	}
	inner := frame - 6
	if inner < 8 {
		inner = 8
	}
	budget := height - modalChrome - 2 // title + blank
	if budget < 3 {
		budget = 3
	}
	var body string
	switch m.mode {
	case WorkspaceModeAssign:
		body = m.viewAssign(inner, height)
	case WorkspaceModeCreate, WorkspaceModeRename:
		body = m.viewForm(inner)
	case WorkspaceModeConfirmDelete:
		body = m.viewConfirm(inner)
	default:
		body = m.viewManage(inner, budget)
	}
	return styles.Modal(frame).Render(body)
}

func (m WorkspaceModal) viewManage(inner, budget int) string {
	title := styles.Title.Render("Workspaces") + styles.Subtle.Render("  hosts grouped by context")
	hints := ansi.Truncate(
		styles.Hint("enter", "switch")+"  "+styles.Hint("n", "new")+"  "+styles.Hint("r", "rename")+"  "+
			styles.Hint("d", "delete")+"  "+styles.Hint("esc", "back"), inner, "…")

	// title + blank + rows + blank + hints
	rows := budget - 2
	if rows < 1 {
		rows = 1
	}
	choices := m.managerChoices()
	if m.cursor >= choices {
		m.cursor = choices - 1
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := start + rows
	if end > choices {
		end = choices
	}

	var list []string
	for i := start; i < end; i++ {
		list = append(list, m.managerRow(i, inner))
	}
	if m.err != "" {
		list = append(list, styles.ErrorText.Render(ansi.Truncate("✗ "+m.err, inner, "…")))
	}
	out := append([]string{title, ""}, list...)
	out = append(out, "", hints)
	return strings.Join(out, "\n")
}

func (m WorkspaceModal) managerRow(i, inner int) string {
	active := i == m.cursor
	marker := "  "
	if active {
		marker = "› "
	}
	var label string
	if i == 0 {
		label = fmt.Sprintf("All (%d)", len(m.data.Assignments))
	} else {
		name := m.data.Names[i-1]
		label = fmt.Sprintf("%s (%s)", name, countLabel(m.countAssigned(name)))
	}
	if active {
		return styles.RowActive.Width(inner).Render(marker + ansi.Truncate(label, inner-2, "…"))
	}
	return marker + styles.RowNormal.Render(ansi.Truncate(label, inner-2, "…"))
}

func (m WorkspaceModal) countAssigned(name string) int {
	n := 0
	for _, w := range m.data.Assignments {
		if w == name {
			n++
		}
	}
	return n
}

func countLabel(n int) string {
	if n == 1 {
		return "1 host"
	}
	return fmt.Sprintf("%d hosts", n)
}

func (m WorkspaceModal) viewForm(inner int) string {
	title, verb := "New workspace", "create"
	if m.mode == WorkspaceModeRename {
		title, verb = "Rename workspace", "rename"
	}
	var b strings.Builder
	b.WriteString(styles.Title.Render(title) + "\n\n")
	b.WriteString(styles.FieldFocus.Render("Name") + m.input.View() + "\n\n")
	if m.err != "" {
		b.WriteString(styles.ErrorText.Render(ansi.Truncate("✗ "+m.err, inner, "…")) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(ansi.Truncate(
		styles.Hint("enter", verb)+"  "+styles.Hint("esc", "back"), inner, "…"))
	return b.String()
}

func (m WorkspaceModal) viewConfirm(inner int) string {
	name, _ := m.CursorName()
	body := styles.Title.Render(fmt.Sprintf("Delete %s", ansi.Truncate(name, inner, "…"))) + "\n\n" +
		styles.Body.Render(ansi.Hardwrap(fmt.Sprintf("Its %s stays in your SSH config and become Unassigned.",
			pluralHosts(m.countAssigned(name))), inner, true)) + "\n\n" +
		ansi.Truncate(styles.Hint("y", "delete")+"   "+styles.Hint("n", "keep"), inner, "…")
	return body
}

func pluralHosts(n int) string {
	if n == 1 {
		return "host"
	}
	return "hosts"
}

func (m WorkspaceModal) viewAssign(inner, height int) string {
	title := styles.Title.Render("Assign "+ansi.Truncate(m.target, inner-8, "…")) + styles.Subtle.Render("  to a workspace")
	hints := ansi.Truncate(styles.Hint("enter", "assign")+"  "+styles.Hint("esc", "cancel"), inner, "…")
	budget := height - modalChrome
	rows := budget - 2
	if rows < 1 {
		rows = 1
	}
	ch := m.choices()
	if m.cursor >= len(ch) {
		m.cursor = len(ch) - 1
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := start + rows
	if end > len(ch) {
		end = len(ch)
	}
	var list []string
	for i := start; i < end; i++ {
		label := ch[i]
		mark := " "
		if ch[i] == m.data.Assignments[m.target] {
			mark = "•"
		}
		if label == "" {
			label = "Unassigned"
		} else {
			label = fmt.Sprintf("%s (%s)", label, countLabel(m.countAssigned(label)))
		}
		marker := "  "
		if i == m.cursor {
			marker = "› "
		}
		line := marker + label
		if i == m.cursor {
			list = append(list, styles.RowActive.Width(inner).Render(ansi.Truncate(line, inner, "…")))
		} else {
			list = append(list, renderAssignLine(mark, line, inner))
		}
	}
	out := append([]string{title, ""}, list...)
	out = append(out, "", hints)
	return strings.Join(out, "\n")
}

func renderAssignLine(mark, line string, inner int) string {
	prefix := "  " + styles.Subtle.Render(mark+" ")
	avail := inner - 4
	if avail < 4 {
		avail = 4
	}
	return prefix + styles.RowNormal.Render(ansi.Truncate(line, avail, "…"))
}
