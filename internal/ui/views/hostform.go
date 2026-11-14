package views

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
	"elyms/internal/ui/styles"
)

type HostFormSubmitMsg struct {
	Entry         *config.HostEntry
	OriginalAlias string
}

type HostFormCancelMsg struct{}

const (
	fAlias = iota
	fHost
	fUser
	fPort
	fIdentity
	fCount
)

var formLabels = [fCount]string{"Alias", "Hostname", "User", "Port", "Identity file"}

// HostForm adds a new host or edits an existing one.
type HostForm struct {
	inputs   [fCount]textinput.Model
	focus    int
	err      string
	original *config.HostEntry
	exists   func(alias string) bool
	keys     []string
	keyIdx   int
}

// NewHostForm edits original, or adds a host when original is nil. keys are
// private key paths offered with up/down on the identity field. exists
// reports whether an alias is already in the config.
func NewHostForm(original *config.HostEntry, keys []string, exists func(string) bool) HostForm {
	f := HostForm{original: original, exists: exists, keys: keys, keyIdx: -1}
	placeholders := [fCount]string{"my-server", "192.168.1.10 or example.com", "root", "22", "optional, ↑/↓ to pick a key"}
	for i := range f.inputs {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = placeholders[i]
		in.CharLimit = 256
		in.Width = 40
		f.inputs[i] = in
	}
	if original != nil {
		f.inputs[fAlias].SetValue(original.Alias)
		f.inputs[fHost].SetValue(original.HostName)
		f.inputs[fUser].SetValue(original.User)
		f.inputs[fPort].SetValue(original.Port)
		f.inputs[fIdentity].SetValue(original.IdentityFile)
	} else {
		f.inputs[fPort].SetValue("22")
	}
	f.inputs[0].Focus()
	return f
}

func (f HostForm) values() [fCount]string {
	var v [fCount]string
	for i := range f.inputs {
		v[i] = strings.TrimSpace(f.inputs[i].Value())
	}
	return v
}

func (f HostForm) Err() string { return f.err }

func (f HostForm) SetErr(msg string) HostForm { f.err = msg; return f }

func (f HostForm) setFocus(i int) HostForm {
	f.inputs[f.focus].Blur()
	f.focus = (i + fCount) % fCount
	f.inputs[f.focus].Focus()
	return f
}

func (f HostForm) validate() (*config.HostEntry, error) {
	v := f.values()
	editingSame := f.original != nil && f.original.Alias == v[fAlias]
	if !editingSame {
		if err := config.ValidateAlias(v[fAlias]); err != nil {
			return nil, err
		}
		if f.exists != nil && f.exists(v[fAlias]) {
			return nil, errors.New("a host with this alias already exists")
		}
	}
	if v[fPort] != "" {
		p, err := strconv.Atoi(v[fPort])
		if err != nil || p < 1 || p > 65535 {
			return nil, errors.New("port must be a number from 1 to 65535")
		}
	}
	e := &config.HostEntry{
		Alias:        v[fAlias],
		HostName:     v[fHost],
		User:         v[fUser],
		Port:         v[fPort],
		IdentityFile: v[fIdentity],
	}
	if f.original != nil {
		e.ExtraDirectives = f.original.ExtraDirectives
	}
	return e, nil
}

func stripNewlines(r []rune) []rune {
	out := r[:0:0]
	for _, c := range r {
		if c != '\n' && c != '\r' {
			out = append(out, c)
		}
	}
	return out
}

func (f HostForm) Update(msg tea.Msg) (HostForm, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		return f, func() tea.Msg { return HostFormCancelMsg{} }
	case tea.KeyTab:
		return f.setFocus(f.focus + 1), nil
	case tea.KeyShiftTab:
		return f.setFocus(f.focus - 1), nil
	case tea.KeyEnter:
		e, err := f.validate()
		if err != nil {
			f.err = err.Error()
			return f, nil
		}
		f.err = ""
		orig := ""
		if f.original != nil {
			orig = f.original.Alias
		}
		return f, func() tea.Msg { return HostFormSubmitMsg{Entry: e, OriginalAlias: orig} }
	case tea.KeyUp, tea.KeyDown:
		if f.focus == fIdentity && len(f.keys) > 0 {
			if k.Type == tea.KeyDown {
				f.keyIdx = (f.keyIdx + 1) % len(f.keys)
			} else {
				f.keyIdx = (f.keyIdx - 1 + len(f.keys)) % len(f.keys)
				if f.keyIdx < 0 {
					f.keyIdx = 0
				}
			}
			f.inputs[fIdentity].SetValue(f.keys[f.keyIdx])
			f.inputs[fIdentity].CursorEnd()
			return f, nil
		}
		if k.Type == tea.KeyDown {
			return f.setFocus(f.focus + 1), nil
		}
		return f.setFocus(f.focus - 1), nil
	case tea.KeyRunes:
		k.Runes = stripNewlines(k.Runes)
		if len(k.Runes) == 0 {
			return f, nil
		}
		msg = k
	}
	f.err = ""
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return f, cmd
}

func (f HostForm) View(width int) string {
	title := "Add host"
	if f.original != nil {
		title = "Edit host"
	}
	inner := width - 6
	if inner < 20 {
		inner = 20
	}
	var b strings.Builder
	b.WriteString(styles.Title.Render(title) + "\n\n")
	for i := range f.inputs {
		label := styles.FieldLabel
		if i == f.focus {
			label = styles.FieldFocus
		}
		in := f.inputs[i]
		if w := inner - 14; w > 8 {
			in.Width = w - 1
		}
		b.WriteString(label.Render(formLabels[i]) + in.View() + "\n")
	}
	b.WriteString("\n")
	if f.err != "" {
		b.WriteString(styles.ErrorText.Render("✗ "+f.err) + "\n\n")
	}
	b.WriteString(styles.Hint("tab", "next") + "  " + styles.Hint("enter", "save") + "  " + styles.Hint("esc", "cancel"))
	return styles.Modal(width).Render(b.String())
}
