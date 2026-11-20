package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"elyms/internal/keygen"
	"elyms/internal/ui/styles"
)

type KeyManagerCloseMsg struct{}

// keyGeneratedMsg carries the result of a background key generation.
type keyGeneratedMsg struct {
	info *keygen.KeyInfo
	err  error
}

// KeyDeps are the side effects the key manager needs, injected so the view
// can be tested without touching ~/.ssh or the clipboard.
type KeyDeps struct {
	List       func() ([]*keygen.KeyInfo, error)
	Generate   func(name string, t keygen.KeyType, comment string) (*keygen.KeyInfo, error)
	ReadPublic func(k *keygen.KeyInfo) (string, error)
	Copy       func(text string) error
}

const (
	gName = iota
	gType
	gComment
	gCount
)

type KeyManager struct {
	deps      KeyDeps
	keys      []*keygen.KeyInfo
	cursor    int
	showPub   bool
	pubText   string
	status    string
	statusErr bool
	loadErr   string

	generating bool
	busy       bool
	gFocus     int
	gInputs    [2]textinput.Model // name, comment
	gRSA       bool
	gErr       string
}

func NewKeyManager(deps KeyDeps) KeyManager {
	m := KeyManager{deps: deps}
	m.reload("")
	return m
}

func (m *KeyManager) reload(selectName string) {
	keys, err := m.deps.List()
	if err != nil {
		m.loadErr = err.Error()
	} else {
		m.loadErr = ""
	}
	m.keys = keys
	m.cursor = 0
	for i, k := range keys {
		if k.Name == selectName {
			m.cursor = i
		}
	}
}

func (m KeyManager) Generating() bool { return m.generating }

func (m KeyManager) Selected() *keygen.KeyInfo {
	if m.cursor < 0 || m.cursor >= len(m.keys) {
		return nil
	}
	return m.keys[m.cursor]
}

func (m *KeyManager) openGenerate() {
	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "id_ed25519_work"
	name.CharLimit = 64
	name.Width = 30
	name.Focus()
	comment := textinput.New()
	comment.Prompt = ""
	comment.Placeholder = "optional, e.g. you@laptop"
	comment.CharLimit = 128
	comment.Width = 30
	m.gInputs = [2]textinput.Model{name, comment}
	m.generating, m.gFocus, m.gRSA, m.gErr = true, gName, false, ""
	m.status = ""
}

func (m KeyManager) finishGenerate(r keyGeneratedMsg) KeyManager {
	m.busy = false
	if r.err != nil {
		m.gErr = r.err.Error()
		return m
	}
	m.generating = false
	m.reload(r.info.Name)
	m.showPub = false
	m.status, m.statusErr = "Generated "+r.info.Name+" ("+string(r.info.Type)+")", false
	return m
}

func (m KeyManager) Update(msg tea.Msg) (KeyManager, tea.Cmd) {
	if r, ok := msg.(keyGeneratedMsg); ok {
		return m.finishGenerate(r), nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok || m.busy {
		return m, nil
	}
	if m.generating {
		return m.updateGenerate(k)
	}

	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return KeyManagerCloseMsg{} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.showPub, m.status = false, ""
	case "down", "j":
		if m.cursor < len(m.keys)-1 {
			m.cursor++
		}
		m.showPub, m.status = false, ""
	case "n":
		m.openGenerate()
	case "enter":
		if m.showPub {
			m.showPub = false
			break
		}
		if sel := m.Selected(); sel != nil {
			txt, err := m.deps.ReadPublic(sel)
			if err != nil {
				m.status, m.statusErr = err.Error(), true
				break
			}
			m.pubText, m.showPub, m.status = txt, true, ""
		}
	case "c":
		sel := m.Selected()
		if sel == nil {
			break
		}
		txt, err := m.deps.ReadPublic(sel)
		if err == nil {
			err = m.deps.Copy(txt)
		}
		if err != nil {
			m.status, m.statusErr = "Copy failed: "+err.Error(), true
		} else {
			m.status, m.statusErr = "Copied public key of "+sel.Name+" to clipboard", false
		}
	}
	return m, nil
}

func (m KeyManager) updateGenerate(k tea.KeyMsg) (KeyManager, tea.Cmd) {
	focusInput := func(i int) {
		for j := range m.gInputs {
			m.gInputs[j].Blur()
		}
		in := map[int]int{gName: 0, gComment: 1}
		if idx, ok := in[i]; ok {
			m.gInputs[idx].Focus()
		}
	}
	switch k.Type {
	case tea.KeyEsc:
		m.generating = false
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		m.gFocus = (m.gFocus + 1) % gCount
		focusInput(m.gFocus)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.gFocus = (m.gFocus - 1 + gCount) % gCount
		focusInput(m.gFocus)
		return m, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(m.gInputs[0].Value())
		if name == "" {
			m.gErr = "key name is required"
			return m, nil
		}
		t := keygen.TypeEd25519
		if m.gRSA {
			t = keygen.TypeRSA
		}
		comment := strings.TrimSpace(m.gInputs[1].Value())
		generate := m.deps.Generate
		m.busy, m.gErr = true, ""
		return m, func() tea.Msg {
			info, err := generate(name, t, comment)
			return keyGeneratedMsg{info: info, err: err}
		}
	}

	if m.gFocus == gType {
		switch k.String() {
		case "left", "h":
			m.gRSA = false
		case "right", "l":
			m.gRSA = true
		case " ", "space":
			m.gRSA = !m.gRSA
		}
		return m, nil
	}

	if k.Type == tea.KeyRunes {
		k.Runes = stripNewlines(k.Runes)
		if len(k.Runes) == 0 {
			return m, nil
		}
	}
	m.gErr = ""
	idx := 0
	if m.gFocus == gComment {
		idx = 1
	}
	var cmd tea.Cmd
	m.gInputs[idx], cmd = m.gInputs[idx].Update(k)
	return m, cmd
}

// modalChrome is the vertical space a Modal uses for border and padding.
const modalChrome = 4

func (m KeyManager) View(width, height int) string {
	if m.generating {
		return m.viewGenerate(width, height)
	}
	inner := width - 8
	budget := height - modalChrome - 2
	if budget < 6 {
		budget = 6
	}

	title := styles.Title.Render("Keys") + styles.Subtle.Render("  ~/.ssh")
	hints := ansi.Truncate(styles.Hint("n", "new")+"  "+styles.Hint("c", "copy")+"  "+styles.Hint("enter", "public key")+"  "+styles.Hint("esc", "back"), inner, "…")

	var tail []string
	if m.status != "" {
		st := styles.OKText
		if m.statusErr {
			st = styles.ErrorText
		}
		tail = append(tail, st.Render(ansi.Truncate(m.status, inner, "…")))
	}
	if sel := m.Selected(); sel != nil && sel.Comment != "" && m.loadErr == "" {
		tail = append(tail, styles.Label.Render("Comment ")+styles.Value.Render(ansi.Truncate(sel.Comment, inner-8, "…")))
	}
	if m.showPub && m.Selected() != nil {
		wrapped := strings.Split(ansi.Hardwrap(m.pubText, inner, true), "\n")
		room := budget - 9 - len(tail)
		if room < 1 {
			room = 1
		}
		if len(wrapped) > room {
			wrapped = append(wrapped[:room-1], "…")
		}
		tail = append(tail, styles.Subtle.Render("Public key"))
		for _, w := range wrapped {
			tail = append(tail, styles.Body.Render(w))
		}
	}

	// title + blank + list rows + blank + tail + blank + hints
	fixed := 2 + 2
	if len(tail) > 0 {
		fixed += 1 + len(tail)
	}
	rows := budget - fixed
	if rows < 1 {
		rows = 1
	}

	var list []string
	switch {
	case m.loadErr != "":
		list = []string{styles.ErrorText.Render(ansi.Truncate("✗ "+m.loadErr, inner, "…"))}
	case len(m.keys) == 0:
		list = []string{styles.Subtle.Render("No keys found. Press n to generate one.")}
	default:
		start := 0
		if m.cursor >= rows {
			start = m.cursor - rows + 1
		}
		end := start + rows
		if end > len(m.keys) {
			end = len(m.keys)
		}
		for i := start; i < end; i++ {
			k := m.keys[i]
			marker, name := "  ", styles.RowNormal.Render(k.Name)
			if i == m.cursor {
				marker, name = "› ", styles.RowActive.Render(k.Name)
			}
			meta := fmt.Sprintf("%s  %s", k.Type, k.Fingerprint)
			if !k.HasPrivate {
				meta += "  (no private key)"
			}
			list = append(list, ansi.Truncate(marker+name+"  "+styles.RowMuted.Render(meta), inner, "…"))
		}
	}

	out := append([]string{title, ""}, list...)
	if len(tail) > 0 {
		out = append(out, "")
		out = append(out, tail...)
	}
	out = append(out, "", hints)
	return styles.Modal(width).Render(strings.Join(out, "\n"))
}

func (m KeyManager) viewGenerate(width, height int) string {
	inner := width - 8
	var b strings.Builder
	b.WriteString(styles.Title.Render("Generate key") + "\n\n")
	label := func(i int, s string) string {
		if i == m.gFocus {
			return styles.FieldFocus.Render(s)
		}
		return styles.FieldLabel.Render(s)
	}
	b.WriteString(label(gName, "Name") + m.gInputs[0].View() + "\n")

	ed, rsa := "( ) ed25519", "( ) rsa 4096"
	if m.gRSA {
		rsa = "(•) rsa 4096"
	} else {
		ed = "(•) ed25519"
	}
	b.WriteString(label(gType, "Type") + ed + "   " + rsa + "\n")
	b.WriteString(label(gComment, "Comment") + m.gInputs[1].View() + "\n\n")
	switch {
	case m.busy:
		kind := "ed25519"
		if m.gRSA {
			kind = "rsa 4096 (this can take several seconds)"
		}
		b.WriteString(styles.Subtle.Render(ansi.Truncate("Generating "+kind+"…", inner, "…")) + "\n")
	case m.gErr != "":
		b.WriteString(styles.ErrorText.Render(ansi.Truncate("✗ "+m.gErr, inner, "…")) + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString("\n" + ansi.Truncate(styles.Hint("tab", "next")+"  "+styles.Hint("←/→", "type")+"  "+styles.Hint("enter", "generate")+"  "+styles.Hint("esc", "cancel"), inner, "…"))
	return styles.Modal(width).Render(b.String())
}
