package ui

import (
	"fmt"
	"strings"
	"time"

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
	wsHosts []*config.HostEntry // the hosts ws was loaded for

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
	m.wsHosts = hosts
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
	// w leaves a workspace context and returns to All; with no context it
	// opens the manager below.
	if k.String() == "w" && m.list.Workspace() != "" {
		m.list, _ = m.list.Update(k)
		m.list.SetHeight(m.listHeight())
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
		m.mode = modeList
		return m.setStatus("Saved "+e.Alias, false), nil
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
			m.refreshWorkspaces(cfg)
			return m.setStatus("Host \""+res.Payload+"\" no longer exists", true), nil
		}
		if err := cfg.Save(); err != nil {
			return m.setStatus("could not save: "+err.Error(), true), nil
		}
		m.list.SetHosts(cfg.Hosts())
		m.refreshWorkspaces(cfg)
		return m.setStatus("Deleted "+res.Payload, false), nil
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	return m, cmd
}

// refreshWorkspaces reconciles metadata with a freshly loaded config. Hosts
// added, renamed, or removed outside elyms update the visible grouping, while
// stale assignments stay on disk (never rewritten here) and are ignored until
// their host comes back.
func (m *Model) refreshWorkspaces(cfg *config.Config) {
	if m.ws == nil {
		if m.wsErr != nil && m.opts.WorkspacePath != "" {
			_ = m.loadWorkspace(cfg)
		}
		return
	}
	gone := map[string]*config.HostEntry{}
	for _, h := range m.wsHosts {
		gone[h.Alias] = h
	}
	for _, h := range cfg.Hosts() {
		delete(gone, h.Alias)
	}
	// A rename leaves one alias gone and one new alias in the config; moving
	// the assignment over keeps a host's workspace across edits.
	for _, h := range cfg.Hosts() {
		if m.ws.WorkspaceOf(h.Alias) != "" {
			continue
		}
		for oldAlias := range gone {
			w := m.ws.WorkspaceOf(oldAlias)
			if w == "" {
				continue
			}
			if old := gone[oldAlias]; old.IsPattern == h.IsPattern && old.HostName == h.HostName {
				if err := m.ws.Assign(h.Alias, w); err == nil {
					m.ws.Unassign(oldAlias)
					delete(gone, oldAlias)
					break
				}
			}
		}
	}
	m.wsHosts = cfg.Hosts()
	m.list.SetWorkspaceData(m.ws.Workspaces(), m.ws.Assignments())
}

// updateWorkspace handles the workspace manager and the assign chooser. Every
// mutation builds a candidate store, saves it, and only then touches the
// visible list, so a failed save changes nothing.
func (m Model) updateWorkspace(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Keystrokes are answered with the modal's typed message as a command, so
	// that command has to reach the runtime. The textinput cursor blink a key
	// can also return is dropped: a blink timer would swallow the next
	// keypress in the update loop below.
	next, cmd := m.wsModal.Update(msg)
	m.wsModal = next
	if _, ok := msg.(tea.KeyMsg); ok {
		cmd = workspaceCmd(cmd)
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
	return m, cmd
}

// workspaceCmd lets one of the modal's own typed messages through and drops
// everything else (the textinput cursor blink, above all: a blink timer would
// swallow the user's next keypress). The probe runs in the background so the
// update loop never waits on a timer.
func workspaceCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case out := <-ch:
		switch out.(type) {
		case views.WorkspaceCreateMsg, views.WorkspaceRenameMsg, views.WorkspaceDeleteMsg,
			views.WorkspaceSelectMsg, views.WorkspaceAssignMsg, views.WorkspaceAssignCancelMsg,
			views.WorkspaceModalCloseMsg:
			return func() tea.Msg { return out }
		}
	case <-time.After(2 * time.Millisecond):
	}
	return nil
}

// backToList closes any workspace dialog and returns to the host list.
func (m Model) backToList() Model {
	m.mode = modeList
	m.wsModal = views.NewWorkspaceModal(views.WorkspaceData{})
	return m
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
	m.wsHosts = hosts
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
	if target != "" {
		data.Selected = target
		for _, h := range m.wsHosts {
			if h.Alias == target {
				data.SelectedIsPattern = h.IsPattern
			}
		}
	}
	m.wsModal = views.NewWorkspaceModal(data)
	m.mode = modeWorkspace
	return m
}

func (m Model) listHeight() int {
	h := m.height - 6
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styles.Subtle.Render(fmt.Sprintf("Terminal too small\nneeds %dx%d, have %dx%d", minWidth, minHeight, m.width, m.height)))
	}

	base := m.viewMain()
	modalW := m.width - 4
	if modalW > 76 {
		modalW = 76
	}
	var modal string
	switch m.mode {
	case modeForm:
		modal = m.form.View(modalW)
	case modeKeys:
		modal = m.keys.View(modalW, m.height-4)
	case modeConfirm:
		modal = m.confirm.View(minInt(modalW, 56))
	case modeWorkspace:
		modal = m.wsModal.View(minInt(modalW, 64), m.height-4)
	default:
		return base
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m Model) viewMain() string {
	leftW := m.width * 55 / 100
	rightW := m.width - leftW
	listH := m.listHeight()

	list := m.list
	left := styles.Panel(true).Width(leftW - 2).Height(listH).Render(list.View(leftW-4, listH))
	right := styles.Panel(false).Width(rightW - 2).Height(listH).Render(
		views.Inspector(m.list.Selected(), rightW-4, listH))

	title := styles.Title.Render("elyms") + styles.Subtle.Render("  ssh manager")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
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
	return st.Render(ansi.Truncate(m.status, m.width, "…"))
}

func (m Model) hintLine() string {
	var parts []string
	if m.list.Filtering() {
		parts = []string{styles.Hint("enter", "apply"), styles.Hint("esc", "clear")}
	} else {
		parts = []string{
			styles.Hint("enter", "connect"), styles.Hint("a", "add"), styles.Hint("e", "edit"),
			styles.Hint("d", "delete"), styles.Hint("k", "keys"), styles.Hint("/", "search"), styles.Hint("q", "quit"),
		}
	}
	return ansi.Truncate(strings.Join(parts, "  "), m.width, "…")
}
