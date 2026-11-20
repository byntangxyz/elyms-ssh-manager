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
	// anchored is true once the user (or an explicit SelectAlias) chose the
	// cursor, so regrouping keeps that host instead of clamping.
	anchored bool

	// Workspace context: names in display order, one workspace per alias, and
	// the active workspace ("" means All).
	wsNames  []string
	wsOf     map[string]string
	wsActive string
}

func NewHostList() HostList { return HostList{height: 10} }

// SetWorkspaceData sets the workspace sections and host assignments. The list
// only renders what it is given; persistence stays in the root model.
func (l *HostList) SetWorkspaceData(names []string, assignments map[string]string) {
	keep := ""
	oldPos := l.cursor
	if s := l.Selected(); s != nil {
		keep = s.Alias
	}
	l.wsNames = append([]string(nil), names...)
	l.wsOf = make(map[string]string, len(assignments))
	for k, v := range assignments {
		l.wsOf[k] = v
	}
	if l.wsActive != "" && !l.hasWorkspace(l.wsActive) {
		l.wsActive = ""
	}
	l.refilter()
	l.reanchor(keep, oldPos)
}

// SetWorkspace switches the list context: "" is All, otherwise a workspace
// name. Unknown names fall back to All.
func (l *HostList) SetWorkspace(name string) {
	keep := ""
	oldPos := l.cursor
	if s := l.Selected(); s != nil {
		keep = s.Alias
	}
	if name != "" && !l.hasWorkspace(name) {
		name = ""
	}
	l.wsActive = name
	l.refilter()
	l.reanchor(keep, oldPos)
}

func (l HostList) Workspace() string    { return l.wsActive }
func (l HostList) Workspaces() []string { return append([]string(nil), l.wsNames...) }

func (l HostList) WorkspaceOf(alias string) string { return l.wsOf[alias] }

func (l *HostList) hasWorkspace(name string) bool {
	for _, n := range l.wsNames {
		if n == name {
			return true
		}
	}
	return false
}

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
			l.anchored = true
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

// groupAll reorders the rows the way All renders them: named workspaces in
// metadata order, then Unassigned.
func (l *HostList) groupAll() {
	if l.wsActive != "" {
		return
	}
	var out []*config.HostEntry
	for _, s := range l.sections() {
		out = append(out, s.hosts...)
	}
	l.visible = out
}

func (l *HostList) refilter() {
	q := strings.ToLower(l.query)
	l.visible = l.visible[:0:0]
	for _, h := range l.all {
		if l.wsActive != "" && l.wsOf[h.Alias] != l.wsActive {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(h.Alias+"\x00"+h.HostName+"\x00"+h.User), q) {
			l.visible = append(l.visible, h)
		}
	}
	l.groupAll()
	l.clamp()
}

// section is one headed group of rows in All.
type section struct {
	name  string
	hosts []*config.HostEntry
}

// sections groups every host the current filter matches: named workspaces in
// metadata order, then Unassigned. In a named workspace context there is a
// single section and no heading, because the context is in the list header.
func (l HostList) sections() []section {
	if l.wsActive != "" {
		return []section{{name: l.wsActive, hosts: l.visible}}
	}
	byName := make(map[string][]*config.HostEntry, len(l.wsNames))
	var unassigned []*config.HostEntry
	for _, h := range l.visible {
		w := l.wsOf[h.Alias]
		if w == "" {
			unassigned = append(unassigned, h)
			continue
		}
		byName[w] = append(byName[w], h)
	}
	var out []section
	for _, n := range l.wsNames {
		if hs := byName[n]; len(hs) > 0 {
			out = append(out, section{name: n, hosts: hs})
		}
	}
	if len(unassigned) > 0 {
		out = append(out, section{name: "Unassigned", hosts: unassigned})
	}
	return out
}

func (l *HostList) clamp() {
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	// Do not scroll here: regrouping keeps the viewport still while
	// reanchor decides which row is highlighted.
}

// reanchor keeps the cursor on alias after the grouping changed. When the
// host moved within the list, the cursor steps once in the same direction, so
// a host that just joined a section stays the highlighted row.
func (l *HostList) reanchor(alias string, oldPos int) {
	if alias == "" || len(l.visible) == 0 || !l.anchored {
		l.anchored = false
		l.clamp()
		l.scrollIntoView()
		return
	}
	newPos := -1
	for i, h := range l.visible {
		if h.Alias == alias {
			newPos = i
			break
		}
	}
	switch {
	case newPos < 0:
		l.clamp()
	case newPos > oldPos && oldPos < len(l.visible)-1:
		l.cursor = oldPos + 1
	case newPos < oldPos && oldPos > 0:
		l.cursor = oldPos - 1
	default:
		l.cursor = newPos
		l.anchored = true
	}
	l.clamp()
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

func (l *HostList) leaveWorkspace() {
	l.wsActive = ""
	l.refilter()
}

// Update handles navigation, search, and leaving a workspace context. w is
// the model's workspace key, so it is only consumed here to leave a context.
func (l HostList) Update(msg tea.Msg) (HostList, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return l, nil
	}
	if !l.filtering && l.wsActive != "" && k.String() == "w" {
		l.anchored = false
		l.leaveWorkspace()
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
	ctx := ""
	if l.wsActive != "" {
		ctx = styles.Badge.Render(ansi.Truncate(l.wsActive, width/2, "…")) + " "
	}
	switch {
	case l.filtering:
		header = ctx + styles.KeyHint.Render("/") + " " + styles.Body.Render(l.query) + styles.Subtle.Render("▏")
	case l.query != "":
		header = ctx + styles.KeyHint.Render("/") + " " + styles.Body.Render(l.query) + styles.Subtle.Render("  esc to clear")
	default:
		header = ctx + styles.Subtle.Render("/ to search")
	}
	out := []string{ansi.Truncate(header, width, "…")}

	rows := height - 1
	switch {
	case len(l.all) == 0:
		out = append(out, styles.Subtle.Render(ansi.Truncate("No hosts yet. Press a to add one.", width, "…")))
	case len(l.visible) == 0:
		out = append(out, styles.Subtle.Render(ansi.Truncate(l.emptyLine(), width, "…")))
	default:
		out = append(out, l.renderSections(rows, width)...)
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// emptyLine explains an empty result in the current workspace context.
func (l HostList) emptyLine() string {
	if l.wsActive != "" {
		if l.query != "" {
			return "No matches in " + l.wsActive + "."
		}
		return "No hosts in " + l.wsActive + ". Press g on a host to assign it."
	}
	return "No matches."
}

// renderSections renders the grouped rows intersected with the scroll
// viewport [offset, offset+rows). Headings are non-selectable rows, so only
// hosts are indexed by the cursor.
func (l HostList) renderSections(rows, width int) []string {
	var lines []renderLine
	position := 0
	for _, s := range l.sections() {
		if l.wsActive == "" {
			lines = append(lines, renderLine{text: renderHeading(s.name, len(s.hosts), width), host: -1})
		}
		for _, h := range s.hosts {
			lines = append(lines, renderLine{text: renderRow(h, position == l.cursor, width), host: position})
			position++
		}
	}
	if l.offset >= len(lines) {
		l.offset = 0
	}
	if l.cursor < len(l.visible) {
		l.scrollToVisibleLine(l.cursor, lines)
	}
	end := l.offset + rows
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, end-l.offset)
	for i := l.offset; i < end; i++ {
		out = append(out, lines[i].text)
	}
	return out
}

// scrollToVisibleLine adjusts the line offset so host cursor sits inside a
// viewport of the current height. Headings take screen lines but not cursor
// positions, so the cursor index alone is not a line offset.
func (l *HostList) scrollToVisibleLine(cursor int, lines []renderLine) {
	rows := l.height - 1
	if rows < 1 {
		rows = 1
	}
	lineOf := 0
	for i, ln := range lines {
		if ln.host == cursor {
			lineOf = i
			break
		}
	}
	if lineOf < l.offset {
		l.offset = lineOf
	}
	if lineOf >= l.offset+rows {
		l.offset = lineOf - rows + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

type renderLine struct {
	text string
	host int // index into visible, -1 for a heading
}

func renderHeading(name string, count, width int) string {
	label := name
	if count > 0 {
		label = fmt.Sprintf("%s (%d)", name, count)
	}
	return styles.Badge.Render(ansi.Truncate(label, width-2, "…"))
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
