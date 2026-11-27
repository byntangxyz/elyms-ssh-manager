package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"elyms/internal/config"
	"elyms/internal/keygen"
	"elyms/internal/ssh"
	"elyms/internal/ui/styles"
	"elyms/internal/ui/views"
	"elyms/internal/workspace"
)

const (
	minWidth  = 50
	minHeight = 16

	// chromeHeight is the vertical space the frame spends outside the
	// bordered panes: the title, the status line, the hint line, and the two
	// border rows of the panes themselves.
	chromeHeight = 5

	// The last row and column are deliberately left unused. Writing a cell in
	// the bottom-right corner makes conhost (and most other terminals) scroll
	// or wrap, and a repaint then draws the footer twice on consecutive
	// lines. Reserving the edge keeps every frame strictly inside the window.
	reservedEdge = 1
)

type Options struct {
	ConfigPath    string
	SSHDir        string
	WorkspacePath string
	Connect       func(alias string) tea.Cmd
	Copy          func(text string) error
	SSHAvailable  bool
}

type mode int

const (
	modeList mode = iota
	modeForm
	modeKeys
	modeConfirm
	modeWorkspace
)

type Model struct {
	opts   Options
	mode   mode
	width  int
	height int

	list    views.HostList
	form    views.HostForm
	keys    views.KeyManager
	confirm views.Confirm
	wsModal views.WorkspaceModal

	// Workspace state: the saved metadata, and wsErr when the file on disk
	// could not be read (in which case saving is refused, never guessed at).
	ws      *workspace.Store
	wsErr   error
	wsHosts map[string]*config.HostEntry // the hosts ws was loaded for

	status    string
	statusErr bool
}

func New(opts Options) (Model, error) {
	m := Model{opts: opts, list: views.NewHostList()}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return m, err
	}
	m.list.SetHosts(cfg.Hosts())
	if opts.WorkspacePath != "" {
		if err := m.loadWorkspace(cfg); err != nil {
			m = m.setStatus(err.Error(), true)
		}
	}
	return m, nil
}

// loadWorkspace reads metadata for cfg's aliases and hands it to the list. A
// broken metadata file leaves the store empty and wsErr set, so hosts keep
// working (and connecting) and saving is refused until the file is fixed.
func (m *Model) loadWorkspace(cfg *config.Config) error {
	hosts := cfg.Hosts()
	aliases := make([]string, 0, len(hosts))
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	st, err := workspace.Load(m.opts.WorkspacePath, aliases)
	m.wsHosts = toHostMap(hosts)
	if err != nil {
		m.ws, m.wsErr = nil, err
		m.list.SetWorkspaceData(nil, nil)
		return fmt.Errorf("workspace metadata: %v", err)
	}
	m.ws, m.wsErr = st, nil
	m.list.SetWorkspaceData(st.Workspaces(), st.Assignments())
	return nil
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) setStatus(msg string, isErr bool) Model {
	m.status, m.statusErr = msg, isErr
	return m
}

// reload re-reads the config from disk so that edits made outside elyms
// while it is open are never overwritten.
func (m Model) reload() (*config.Config, error) {
	return config.Load(m.opts.ConfigPath)
}

func (m Model) keyPaths() []string {
	keys, _ := keygen.ListKeys(m.opts.SSHDir)
	var out []string
	for _, k := range keys {
		if k.HasPrivate {
			out = append(out, k.Path)
		}
	}
	return out
}

func (m Model) keyDeps() views.KeyDeps {
	return views.KeyDeps{
		List: func() ([]*keygen.KeyInfo, error) { return keygen.ListKeys(m.opts.SSHDir) },
		Generate: func(name string, t keygen.KeyType, comment string) (*keygen.KeyInfo, error) {
			return keygen.GenerateKey(m.opts.SSHDir, name, t, comment)
		},
		ReadPublic: func(k *keygen.KeyInfo) (string, error) { return keygen.ReadPublicKey(k.PubPath) },
		Copy:       m.opts.Copy,
	}
}

func (m Model) openForm(original *config.HostEntry) Model {
	exists := func(alias string) bool {
		cfg, err := m.reload()
		return err == nil && cfg.Has(alias)
	}
	m.form = views.NewHostForm(original, m.keyPaths(), exists)
	m.mode = modeForm
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetHeight(m.listHeight())
		return m, nil

	case ssh.FinishedMsg:
		text, isErr := ssh.Describe(msg)
		return m.setStatus(text, isErr), nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}

	switch m.mode {
	case modeForm:
		return m.updateForm(msg)
	case modeKeys:
		return m.updateKeys(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeWorkspace:
		return m.updateWorkspace(msg)
	}
	return m.updateList(msg)
}

func (m Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.list.Filtering() {
		m.list, _ = m.list.Update(msg)
		return m, nil
	}
	// w with no workspace context opens the manager (below). Inside a
	// workspace it returns to All, and with a search applied or a workspace
	// that lost its hosts it also opens the manager, so the key never does
	// nothing visible.
	if k.String() == "w" && m.list.Workspace() != "" {
		leave := m.list.HasFilter() || len(m.list.Visible()) == 0
		m.list.SetWorkspace("")
		m.list.SetHeight(m.listHeight())
		if leave {
			m.status = ""
			return m.openWorkspace(""), nil
		}
		return m.setStatus("Showing all hosts", false), nil
	}
	sel := m.list.Selected()

	switch k.String() {
	case "q":
		return m, tea.Quit
	case "enter":
		if sel == nil {
			return m, nil
		}
		if sel.IsPattern {
			return m.setStatus("\""+sel.Alias+"\" is a pattern, not a host you can connect to", true), nil
		}
		if !m.opts.SSHAvailable {
			return m.setStatus("OpenSSH client not found. Enable it in Windows Optional Features.", true), nil
		}
		m.status = ""
		return m, m.opts.Connect(sel.Alias)
	case "a":
		m.status = ""
		return m.openForm(nil), nil
	case "e":
		if sel != nil {
			if sel.Duplicate {
				return m.setStatus("\""+sel.Alias+"\" is defined more than once. Fix the duplicate Host lines by hand first.", true), nil
			}
			m.status = ""
			return m.openForm(sel), nil
		}
	case "d":
		if sel != nil {
			if sel.Duplicate {
				return m.setStatus("\""+sel.Alias+"\" is defined more than once. Fix the duplicate Host lines by hand first.", true), nil
			}
			m.confirm = views.NewConfirm("Delete host \""+sel.Alias+"\" from your SSH config?", sel.Alias)
			m.mode = modeConfirm
		}
		return m, nil
	case "k":
		m.keys = views.NewKeyManager(m.keyDeps())
		m.mode = modeKeys
		m.status = ""
		return m, nil
	case "w":
		m.status = ""
		return m.openWorkspace(""), nil
	case "g":
		if sel == nil {
			return m, nil
		}
		if sel.IsPattern {
			return m.setStatus("\""+sel.Alias+"\" is a pattern; patterns are never assigned", true), nil
		}
		return m.openWorkspace(sel.Alias), nil
	}
	m.list, _ = m.list.Update(msg)
	return m, nil
}
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case views.HostFormCancelMsg:
		m.mode = modeList
		return m, nil
	case views.HostFormSubmitMsg:
		cfg, err := m.reload()
		if err != nil {
			m.form = m.form.SetErr(err.Error())
			return m, nil
		}
		e := msg.Entry
		switch {
		case msg.OriginalAlias == "":
			if cfg.Has(e.Alias) {
				m.form = m.form.SetErr("a host with this alias already exists")
				return m, nil
			}
		case msg.OriginalAlias != e.Alias:
			if err := cfg.Rename(msg.OriginalAlias, e.Alias); err != nil {
				m.form = m.form.SetErr(err.Error())
				return m, nil
			}
		}
		cfg.AddOrUpdate(e)
		if err := cfg.Save(); err != nil {
			m.form = m.form.SetErr("could not save: " + err.Error())
			return m, nil
		}
		m.list.SetHosts(cfg.Hosts())
		m.list.SelectAlias(e.Alias)
		status, err := m.resyncAfterHostChange(cfg, "Saved "+e.Alias, e.Alias)
		m.mode = modeList
		return m.setStatus(status, err != nil), nil
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m Model) updateKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(views.KeyManagerCloseMsg); ok {
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.keys, cmd = m.keys.Update(msg)
	return m, cmd
}

func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if res, ok := msg.(views.ConfirmResultMsg); ok {
		m.mode = modeList
		if !res.Confirmed {
			return m, nil
		}
		cfg, err := m.reload()
		if err != nil {
			return m.setStatus(err.Error(), true), nil
		}
		if !cfg.Delete(res.Payload) {
			// The host was edited away outside elyms: resync so the list
			// stops offering it instead of leaving a stale row.
			m.list.SetHosts(cfg.Hosts())
			status, rerr := m.resyncAfterHostChange(cfg, "Host \""+res.Payload+"\" no longer exists", res.Payload)
			return m.setStatus(status, rerr != nil), nil
		}
		if err := cfg.Save(); err != nil {
			return m.setStatus("could not save: "+err.Error(), true), nil
		}
		m.list.SetHosts(cfg.Hosts())
		status, rerr := m.resyncAfterHostChange(cfg, "Deleted "+res.Payload, res.Payload)
		return m.setStatus(status, rerr != nil), nil
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	return m, cmd
}

// refreshOutcome says what a config resync found.
type refreshOutcome int

const (
	refreshNone        refreshOutcome = iota // nothing to reload
	refreshKept                              // metadata unchanged
	refreshAdopted                           // a previously broken file was read
	refreshStillBroken                       // the metadata file is still unreadable
)

// refreshWorkspaces resyncs the visible grouping with a freshly loaded config
// after a host edit or delete, and returns any assignment that follows a host
// renamed outside elyms (old alias -> workspace, new alias -> workspace) for
// the caller to save. A broken metadata file is retried, so one repaired while
// elyms was open is adopted. Patterns never move, and a move is only proposed
// when exactly one host in the new config carries the old host's identity.
func (m *Model) refreshWorkspaces(cfg *config.Config) (refreshOutcome, map[string]aliasMove) {
	if m.opts.WorkspacePath == "" {
		return refreshNone, nil
	}
	hosts := cfg.Hosts()
	if m.ws == nil {
		if m.wsErr == nil {
			return refreshNone, nil
		}
		if err := m.loadWorkspace(cfg); err != nil {
			m.wsHosts = toHostMap(hosts)
			return refreshStillBroken, nil
		}
		m.wsHosts = toHostMap(hosts)
		return refreshAdopted, nil
	}

	moves := map[string]aliasMove{}
	for oldAlias, old := range m.wsHosts {
		if cfg.Has(oldAlias) || old.IsPattern {
			continue
		}
		w := m.ws.WorkspaceOf(oldAlias)
		if w == "" {
			continue
		}
		if next := renamedInto(old, hosts); next != "" && m.ws.WorkspaceOf(next) == "" {
			move := aliasMove{OldAlias: oldAlias, To: next, Workspace: w}
			moves[next] = move
		}
	}
	m.wsHosts = toHostMap(hosts)
	m.list.SetWorkspaceData(m.ws.Workspaces(), m.ws.Assignments())
	return refreshKept, moves
}

// aliasMove carries a host's workspace from OldAlias to To.
type aliasMove struct {
	OldAlias  string
	To        string
	Workspace string
}

func toHostMap(hosts []*config.HostEntry) map[string]*config.HostEntry {
	out := make(map[string]*config.HostEntry, len(hosts))
	for _, h := range hosts {
		out[h.Alias] = h
	}
	return out
}

// renamedInto returns the single unassigned host in hosts that still looks
// like old (same HostName and User, stronger identity first), or "".
func renamedInto(old *config.HostEntry, hosts []*config.HostEntry) string {
	if old.HostName == "" {
		return ""
	}
	byIdentity, byHost := "", ""
	for _, h := range hosts {
		if h.IsPattern || h.Alias == old.Alias || h.HostName == "" {
			continue
		}
		if h.HostName != old.HostName {
			continue
		}
		if h.User != old.User {
			continue
		}
		if h.Port == old.Port && h.IdentityFile != "" && h.IdentityFile == old.IdentityFile {
			if byIdentity != "" {
				return "" // ambiguous
			}
			byIdentity = h.Alias
			continue
		}
		if byHost != "" {
			return "" // ambiguous
		}
		byHost = h.Alias
	}
	if byIdentity != "" {
		return byIdentity
	}
	return byHost
}

// resyncAfterHostChange refreshes the workspace view after a host edit or
// delete. An assignment whose host was just renamed is moved onto the new
// alias and saved; any other pending move is left for the next save, which
// re-reads the metadata from disk the way saveWorkspace does.
func (m *Model) resyncAfterHostChange(cfg *config.Config, base, edited string) (string, error) {
	outcome, moves := m.refreshWorkspaces(cfg)
	switch outcome {
	case refreshAdopted:
		return "Workspace metadata reloaded; " + strings.ToLower(base), nil
	case refreshStillBroken:
		return base + "; workspace metadata still unreadable", errors.New("workspace metadata")
	}
	move, ok := moves[edited]
	if !ok {
		return base, nil
	}
	if _, err := m.saveWorkspace(func(s *workspace.Store) (string, error) {
		s.Unassign(move.OldAlias)
		err := s.Assign(move.To, move.Workspace)
		return move.Workspace, err
	}); err != nil {
		return base + "; could not move its workspace: " + err.Error(), err
	}
	return base + " in workspace " + move.Workspace, nil
}

// updateWorkspace handles the workspace manager and the assign chooser. Every
// mutation builds a candidate store, saves it, and only then touches the
// visible list, so a failed save changes nothing.
func (m Model) updateWorkspace(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The modal owns its keys: it hands back the action a keystroke decided
	// synchronously (TakeAction), so no command runs and the textinput's
	// cursor-blink timer can never stall the next keypress.
	next, _ := m.wsModal.Update(msg)
	pending, hasAction := next.TakeAction()
	m.wsModal = next
	if _, ok := msg.(tea.KeyMsg); ok && hasAction {
		msg = pending
	}
	switch msg := msg.(type) {
	case views.WorkspaceModalCloseMsg:
		m.mode = modeList
		return m, nil
	case views.WorkspaceAssignCancelMsg:
		m.mode = modeList
		return m, nil
	case views.WorkspaceSelectMsg:
		m.mode = modeList
		m.list.SetWorkspace(msg.Name)
		m.list.SetHeight(m.listHeight())
		if msg.Name == "" {
			return m.setStatus("Showing all hosts", false), nil
		}
		return m.setStatus("Workspace "+msg.Name, false), nil
	case views.WorkspaceCreateMsg:
		name, err := m.saveWorkspace(func(s *workspace.Store) (string, error) { return s.Create(msg.Name) })
		if err != nil {
			return m.workspaceErr(err)
		}
		m.wsModal = m.wsModal.Acknowledge()
		// Creating organizes the manager; it never switches context, so the
		// user can assign hosts from where they were.
		return m.setStatus("Created workspace "+name, false), nil
	case views.WorkspaceRenameMsg:
		_, err := m.saveWorkspace(func(s *workspace.Store) (string, error) { return msg.NewName, s.Rename(msg.OldName, msg.NewName) })
		if err != nil {
			return m.workspaceErr(err)
		}
		m.wsModal = m.wsModal.Acknowledge()
		if m.list.Workspace() == msg.OldName {
			m.list.SetWorkspace(msg.NewName)
		}
		return m.setStatus("Renamed "+msg.OldName+" to "+msg.NewName, false), nil
	case views.WorkspaceDeleteMsg:
		_, err := m.saveWorkspace(func(s *workspace.Store) (string, error) { return msg.Name, s.Delete(msg.Name) })
		if err != nil {
			return m.workspaceErr(err)
		}
		m.wsModal = m.wsModal.Acknowledge()
		if m.list.Workspace() == msg.Name {
			m.list.SetWorkspace("")
		}
		return m.setStatus("Deleted workspace "+msg.Name+" — its hosts are Unassigned", false), nil
	case views.WorkspaceAssignMsg:
		_, err := m.saveWorkspace(func(s *workspace.Store) (string, error) {
			return msg.Name, s.Assign(msg.Alias, msg.Name)
		})
		if err != nil {
			return m.workspaceErr(err)
		}
		m.mode = modeList
		m.wsModal = views.NewWorkspaceModal(views.WorkspaceData{})
		if msg.Name == "" {
			return m.setStatus("Unassigned "+msg.Alias, false), nil
		}
		return m.setStatus("Assigned "+msg.Alias+" to "+msg.Name, false), nil
	}
	// A create or rename that succeeds leaves the manager showing the new
	// list underneath the result status; a failed one keeps the form and its
	// typed value.
	return m, nil
}

// saveWorkspace applies op to a copy of the metadata, saves it, and only on
// success replaces the live store and refreshes the list. The returned value
// is whatever op chose to report (a canonical name, for instance).
func (m *Model) saveWorkspace(op func(s *workspace.Store) (string, error)) (string, error) {
	if m.opts.WorkspacePath == "" {
		return "", fmt.Errorf("workspace metadata is disabled")
	}
	if m.wsErr != nil {
		return "", fmt.Errorf("fix the workspace metadata file first: %v", m.wsErr)
	}
	if m.ws == nil {
		return "", fmt.Errorf("workspace metadata is unavailable")
	}
	cfg, err := m.reload()
	if err != nil {
		return "", fmt.Errorf("re-read SSH config: %v", err)
	}
	hosts := cfg.Hosts()
	aliases := make([]string, 0, len(hosts))
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	cand, err := workspace.Load(m.opts.WorkspacePath, aliases)
	if err != nil {
		return "", fmt.Errorf("workspace metadata: %v", err)
	}
	value, err := op(cand)
	if err != nil {
		return "", err
	}
	if err := cand.Save(); err != nil {
		return "", fmt.Errorf("could not save workspace metadata: %v", err)
	}
	m.ws = cand
	m.wsHosts = toHostMap(hosts)
	m.list.SetWorkspaceData(cand.Workspaces(), cand.Assignments())
	// The manager stays open after a mutation; keep its rows current.
	m.wsModal = m.wsModal.SetData(views.WorkspaceData{
		Names:       cand.Workspaces(),
		Assignments: cand.Assignments(),
	})
	return value, nil
}

// workspaceErr shows err in the modal (keeping the typed value) and in the
// status bar (visible after the modal closes). No command is returned: a
// textinput cursor-blink timer would swallow the user's next keypress.
func (m Model) workspaceErr(err error) (tea.Model, tea.Cmd) {
	m.wsModal = m.wsModal.SetErr(err)
	return m.setStatus(err.Error(), true), nil
}

// openWorkspace opens the manager, or the assign chooser for target.
func (m Model) openWorkspace(target string) Model {
	var names []string
	assignments := map[string]string{}
	if m.ws != nil {
		names = m.ws.Workspaces()
		assignments = m.ws.Assignments()
	}
	data := views.WorkspaceData{Names: names, Assignments: assignments, Target: target}
	m.wsModal = views.NewWorkspaceModal(data)
	m.mode = modeWorkspace
	return m
}

// contentWidth and contentHeight are the columns and rows the frame may use.
// The terminal's last column and last row are never written: filling them
// makes conhost scroll the alt screen, and the next repaint then shows the
// footer on two consecutive lines.
func (m Model) contentWidth() int {
	w := m.width - reservedEdge
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) contentHeight() int {
	h := m.height - reservedEdge
	if h < 1 {
		h = 1
	}
	return h
}

// listHeight is the number of text rows inside the bordered panes. The frame
// spends chromeHeight rows outside them (title, status, hint, two borders),
// and must stay within contentHeight.
func (m Model) listHeight() int {
	h := m.contentHeight() - chromeHeight
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	w, h := m.contentWidth(), m.contentHeight()
	if m.width < minWidth || m.height < minHeight {
		msg := styles.Subtle.Render(fmt.Sprintf("Terminal too small\nneeds %dx%d, have %dx%d", minWidth, minHeight, m.width, m.height))
		return clampFrame(w, h, lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg))
	}

	base := m.viewMain()
	modalW := w - 4
	if modalW > 76 {
		modalW = 76
	}
	var modal string
	switch m.mode {
	case modeForm:
		modal = m.form.View(modalW)
	case modeKeys:
		modal = m.keys.View(modalW, h-4)
	case modeConfirm:
		modal = m.confirm.View(minInt(modalW, 56))
	case modeWorkspace:
		modal = m.wsModal.View(minInt(modalW, 64), h-4)
	default:
		return clampFrame(w, h, base)
	}
	return clampFrame(w, h, lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, modal))
}

// clampFrame enforces the hard bound on what the renderer may draw: at most
// height rows of at most width cells each, with no trailing newline. Every
// path out of View goes through it, so a view that overgrows its budget can
// never push the alt screen into scrolling (which duplicates the last line).
func clampFrame(width, height int, s string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, ln := range lines {
		lines[i] = ansi.Truncate(ln, width, "")
	}
	return strings.Join(lines, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m Model) viewMain() string {
	cw := m.contentWidth()
	leftW := cw * 55 / 100
	rightW := cw - leftW
	listH := m.listHeight()

	list := m.list
	left := styles.Panel(true).Width(leftW - 2).Height(listH).Render(list.View(leftW-4, listH))
	right := views.Welcome(rightW-4, listH)
	if h := m.list.Selected(); h != nil {
		right = views.Inspector(h, rightW-4, listH)
	}
	rightPanel := styles.Panel(false).Width(rightW - 2).Height(listH).Render(right)

	title := styles.Title.Render("elyms") + styles.Subtle.Render("  ssh manager")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, rightPanel)
	return strings.Join([]string{title, body, m.statusLine(), m.hintLine()}, "\n")
}

func (m Model) statusLine() string {
	if m.status == "" {
		return ""
	}
	st := styles.OKText
	if m.statusErr {
		st = styles.ErrorText
	}
	return st.Render(ansi.Truncate(m.status, m.contentWidth(), "…"))
}

// hintLine shows the keys that work right now. It is truncated to the
// terminal width, so the least essential hint goes last and drops first;
// truncation is ANSI-aware because the hints are styled.
func (m Model) hintLine() string {
	var parts []string
	if m.list.Filtering() {
		parts = []string{styles.Hint("enter", "apply"), styles.Hint("esc", "clear")}
	} else if m.list.Workspace() != "" {
		parts = []string{
			styles.Hint("enter", "connect"), styles.Hint("g", "assign"), styles.Hint("w", "all hosts"),
			styles.Hint("a", "add"), styles.Hint("e", "edit"), styles.Hint("d", "delete"),
			styles.Hint("k", "keys"), styles.Hint("/", "search"), styles.Hint("q", "quit"),
		}
	} else {
		parts = []string{
			styles.Hint("enter", "connect"), styles.Hint("g", "assign"), styles.Hint("w", "workspaces"),
			styles.Hint("a", "add"), styles.Hint("e", "edit"), styles.Hint("d", "delete"),
			styles.Hint("k", "keys"), styles.Hint("/", "search"), styles.Hint("q", "quit"),
		}
	}
	return ansi.Truncate(strings.Join(parts, "  "), m.contentWidth(), "…")
}
