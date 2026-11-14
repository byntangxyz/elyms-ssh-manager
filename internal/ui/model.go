package ui

import (
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
)

const (
	minWidth  = 50
	minHeight = 16
)

type Options struct {
	ConfigPath   string
	SSHDir       string
	Connect      func(alias string) tea.Cmd
	Copy         func(text string) error
	SSHAvailable bool
}

type mode int

const (
	modeList mode = iota
	modeForm
	modeKeys
	modeConfirm
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
	return m, nil
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
			return m.setStatus("Host \""+res.Payload+"\" no longer exists", true), nil
		}
		if err := cfg.Save(); err != nil {
			return m.setStatus("could not save: "+err.Error(), true), nil
		}
		m.list.SetHosts(cfg.Hosts())
		return m.setStatus("Deleted "+res.Payload, false), nil
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	return m, cmd
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
