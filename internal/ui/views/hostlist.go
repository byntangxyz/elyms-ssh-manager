package views

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"elyms/internal/config"
	"elyms/internal/ui/styles"
)

// HostList is the filterable, scrollable list of Host entries.
type HostList struct {
	all       []*config.HostEntry
	visible   []*config.HostEntry
	query     string
	filtering bool
	cursor    int
	offset    int
	height    int
}

func NewHostList() HostList { return HostList{height: 10} }

func (l *HostList) SetHosts(hosts []*config.HostEntry) {
	keep := ""
	if s := l.Selected(); s != nil {
		keep = s.Alias
	}
	l.all = hosts
	l.refilter()
	if keep != "" {
		l.SelectAlias(keep)
	}
}

func (l *HostList) SelectAlias(alias string) {
	for i, h := range l.visible {
		if h.Alias == alias {
			l.cursor = i
			l.clamp()
			return
		}
	}
}

// SetHeight tells the list how many lines its View will occupy, so paging
// and scrolling match what is on screen.
func (l *HostList) SetHeight(h int) {
	if h < 3 {
		h = 3
	}
	l.height = h
	l.scrollIntoView()
}

func (l HostList) Selected() *config.HostEntry {
	if l.cursor < 0 || l.cursor >= len(l.visible) {
		return nil
	}
	return l.visible[l.cursor]
}

func (l HostList) Visible() []*config.HostEntry { return l.visible }
func (l HostList) Filtering() bool              { return l.filtering }
func (l HostList) HasFilter() bool              { return l.query != "" }

func (l *HostList) refilter() {
	q := strings.ToLower(l.query)
	l.visible = l.visible[:0:0]
	for _, h := range l.all {
		if q == "" || strings.Contains(strings.ToLower(h.Alias+"\x00"+h.HostName+"\x00"+h.User), q) {
			l.visible = append(l.visible, h)
		}
	}
	l.clamp()
}

func (l *HostList) clamp() {
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.scrollIntoView()
}

func (l *HostList) scrollIntoView() {
	rows := l.height - 1
	if rows < 1 {
		rows = 1
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+rows {
		l.offset = l.cursor - rows + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

func (l *HostList) move(delta int) {
	l.cursor += delta
	l.clamp()
}

func (l HostList) Update(msg tea.Msg) (HostList, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return l, nil
	}
	switch k.Type {
	case tea.KeyUp:
		l.move(-1)
		return l, nil
	case tea.KeyDown:
		l.move(1)
		return l, nil
	case tea.KeyPgUp:
		l.move(-(l.height - 1))
		return l, nil
	case tea.KeyPgDown:
		l.move(l.height - 1)
		return l, nil
	case tea.KeyHome:
		l.cursor = 0
		l.clamp()
		return l, nil
	case tea.KeyEnd:
		l.cursor = len(l.visible) - 1
		l.clamp()
		return l, nil
	}

	if l.filtering {
		switch k.Type {
		case tea.KeyEnter:
			l.filtering = false
		case tea.KeyEsc:
			l.filtering = false
			l.query = ""
			l.refilter()
		case tea.KeyBackspace:
			if r := []rune(l.query); len(r) > 0 {
				l.query = string(r[:len(r)-1])
				l.refilter()
			}
		case tea.KeySpace:
			l.query += " "
			l.refilter()
		case tea.KeyRunes:
			l.query += string(k.Runes)
			l.refilter()
		}
		return l, nil
	}

	switch k.String() {
	case "up", "k":
		l.move(-1)
	case "down", "j":
		l.move(1)
	case "g":
		l.cursor = 0
		l.clamp()
	case "G":
		l.cursor = len(l.visible) - 1
		l.clamp()
	case "/":
		l.filtering = true
	case "esc":
		if l.query != "" {
			l.query = ""
			l.refilter()
		}
	}
	return l, nil
}

// Target renders the host the way ssh would address it: user@host:port.
func Target(h *config.HostEntry) string {
	s := h.HostName
	if s == "" {
		s = h.Alias
	}
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != "" && h.Port != "22" {
		s += ":" + h.Port
	}
	return s
}

func (l *HostList) View(width, height int) string {
	if width < 10 {
		width = 10
	}
	if height < 3 {
		height = 3
	}
	if height != l.height {
		l.height = height
		l.scrollIntoView()
	}

	var header string
	switch {
	case l.filtering:
		header = styles.KeyHint.Render("/") + " " + styles.Body.Render(l.query) + styles.Subtle.Render("▏")
	case l.query != "":
		header = styles.KeyHint.Render("/") + " " + styles.Body.Render(l.query) + styles.Subtle.Render("  esc to clear")
	default:
		header = styles.Subtle.Render("/ to search")
	}
	out := []string{ansi.Truncate(header, width, "…")}

	rows := height - 1
	switch {
	case len(l.all) == 0:
		out = append(out, styles.Subtle.Render(ansi.Truncate("No hosts yet. Press a to add one.", width, "…")))
	case len(l.visible) == 0:
		out = append(out, styles.Subtle.Render(ansi.Truncate("No matches.", width, "…")))
	default:
		end := l.offset + rows
		if end > len(l.visible) {
			end = len(l.visible)
		}
		for i := l.offset; i < end; i++ {
			out = append(out, renderRow(l.visible[i], i == l.cursor, width))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

func renderRow(h *config.HostEntry, active bool, width int) string {
	marker := "  "
	if active {
		marker = "› "
	}
	avail := width - 2
	alias := ansi.Truncate(h.Alias, avail, "…")
	sub := Target(h)
	if h.IsPattern {
		sub = "pattern"
	}
	if h.Duplicate {
		sub = "dup · " + sub
	}
	rest := avail - lipgloss.Width(alias) - 2
	if rest > 3 {
		sub = ansi.Truncate(sub, rest, "…")
	} else {
		sub = ""
	}

	if active {
		plain := marker + alias
		if sub != "" {
			plain += "  " + sub
		}
		return styles.RowActive.Width(width).Render(plain)
	}
	line := marker + styles.RowNormal.Render(alias)
	if sub != "" {
		line += "  " + styles.RowMuted.Render(sub)
	}
	return line
}

// Inspector renders the detail pane for a host. h may be nil.
func Inspector(h *config.HostEntry, width, height int) string {
	if h == nil {
		return styles.Subtle.Render("Select a host to see its details.")
	}
	clip := func(s string) string { return ansi.Truncate(s, width, "…") }
	field := func(label, value string) string {
		if value == "" {
			value = "—"
		}
		label = ansi.Truncate(label, 12, "…")
		return styles.Label.Width(13).Render(label) + clip2(value, width-13)
	}

	var out []string
	out = append(out, clip(styles.Title.Render(h.Alias)))
	if h.IsPattern {
		out = append(out, clip(styles.Badge.Render("pattern")+styles.Subtle.Render(" defaults for matching hosts")))
		out = append(out, "")
		out = append(out, styles.Subtle.Render(clip("Not a connect target.")))
	} else {
		host := h.HostName
		if host == "" {
			host = h.Alias
		}
		port := h.Port
		if port == "" {
			port = "22"
		}
		out = append(out, "")
		out = append(out, field("Hostname", host), field("User", h.User), field("Port", port), field("Identity", h.IdentityFile))
	}
	if h.Duplicate {
		out = append(out, "", clip(styles.ErrorText.Render("Duplicate alias: edit and delete are disabled")))
	}

	if len(h.ExtraDirectives) > 0 {
		keys := make([]string, 0, len(h.ExtraDirectives))
		for k := range h.ExtraDirectives {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, "", styles.Subtle.Render("Other directives"))
		for _, k := range keys {
			out = append(out, field(k, h.ExtraDirectives[k]))
		}
	}
	if h.IsPattern {
		for _, kv := range []struct{ k, v string }{{"HostName", h.HostName}, {"User", h.User}, {"Port", h.Port}, {"IdentityFile", h.IdentityFile}} {
			if kv.v != "" {
				out = append(out, field(kv.k, kv.v))
			}
		}
	}

	if !h.IsPattern {
		out = append(out, "", styles.Subtle.Render("Connects with"), clip(styles.KeyHint.Render(fmt.Sprintf("ssh %s", h.Alias))))
	}
	all := strings.Split(strings.Join(out, "\n"), "\n")
	if height > 0 && len(all) > height {
		all = append(all[:height-1], styles.Subtle.Render("…"))
	}
	return strings.Join(all, "\n")
}

func clip2(s string, w int) string {
	if w < 4 {
		w = 4
	}
	return styles.Value.Render(ansi.Truncate(s, w, "…"))
}
