package views

import (
	"strings"
	"testing"

	"elyms/internal/config"
)

// wsHosts is an SSH-config-ordered host list that deliberately interleaves
// workspaces so grouping cannot accidentally preserve file order.
func wsHosts() []*config.HostEntry {
	return []*config.HostEntry{
		{Alias: "web", HostName: "10.0.0.1"},
		{Alias: "db", HostName: "10.0.0.2"},
		{Alias: "prod-1", HostName: "p1"},
		{Alias: "*", IsPattern: true},
		{Alias: "prod-2", HostName: "p2"},
		{Alias: "dev-1", HostName: "d1"},
	}
}

func wsAssignments() map[string]string {
	return map[string]string{"prod-1": "Prod", "prod-2": "Prod", "dev-1": "Dev"}
}

func wsList(t *testing.T) HostList {
	t.Helper()
	l := NewHostList()
	l.SetHosts(wsHosts())
	l.SetWorkspaceData([]string{"Dev", "Prod"}, wsAssignments())
	return l
}

// order returns the indexes at which each needle appears, in rendering order.
func order(view string, needles ...string) ([]int, bool) {
	var out []int
	prev := -1
	for _, n := range needles {
		i := strings.Index(view, n)
		if i < 0 || i < prev {
			return out, false
		}
		prev = i
		out = append(out, i)
	}
	return out, true
}

func TestGroupAllRendersWorkspacesInMetadataOrderThenUnassigned(t *testing.T) {
	l := wsList(t)
	v := l.View(60, 14)
	if _, ok := order(v, "Dev (1)", "dev-1", "Prod (2)", "prod-1", "prod-2", "Unassigned", "web", "db", "*"); !ok {
		t.Fatalf("All must group by metadata order then Unassigned:\n%s", v)
	}
	if n := strings.Count(v, "Unassigned"); n != 1 {
		t.Errorf("Unassigned heading should appear once, got %d:\n%s", n, v)
	}
	if got := lines(v); got != 14 {
		t.Errorf("view height = %d, want 14", got)
	}
}

func TestGroupWithinSectionsKeepsSSHConfigOrder(t *testing.T) {
	l := wsList(t)
	v := l.View(60, 14)
	if i, j := strings.Index(v, "prod-1"), strings.Index(v, "prod-2"); i < 0 || j < 0 || i > j {
		t.Errorf("prod-1 must come before prod-2 as in the config:\n%s", v)
	}
	if i, j := strings.Index(v, "web"), strings.Index(v, "db"); i < 0 || j < 0 || i > j {
		t.Errorf("unassigned hosts keep config order (web before db):\n%s", v)
	}
}

func TestGroupOnlyExistingSectionsRender(t *testing.T) {
	l := NewHostList()
	l.SetHosts([]*config.HostEntry{{Alias: "web"}})
	l.SetWorkspaceData([]string{"Empty", "Prod"}, nil)
	v := l.View(60, 10)
	if strings.Contains(v, "Empty") {
		t.Errorf("empty workspaces need no heading:\n%s", v)
	}
	if !strings.Contains(v, "Unassigned") || !strings.Contains(v, "web") {
		t.Errorf("unassigned section missing:\n%s", v)
	}
}

func TestGroupNamedContextHidesOtherWorkspaces(t *testing.T) {
	l := wsList(t)
	l.SetWorkspace("Prod")
	v := l.View(60, 12)
	for _, want := range []string{"Prod", "prod-1", "prod-2"} {
		if !strings.Contains(v, want) {
			t.Errorf("Prod view missing %q:\n%s", want, v)
		}
	}
	for _, gone := range []string{"dev-1", "web", "db", "Unassigned"} {
		if strings.Contains(v, gone) {
			t.Errorf("Prod view must hide %q:\n%s", gone, v)
		}
	}
	if l.Workspace() != "Prod" {
		t.Errorf("active workspace = %q", l.Workspace())
	}
	if len(l.Visible()) != 2 {
		t.Errorf("visible = %d", len(l.Visible()))
	}
}

func TestGroupNamedContextUnknownFallsBackToAll(t *testing.T) {
	l := wsList(t)
	l.SetWorkspace("Nope")
	if l.Workspace() != "" || len(l.Visible()) != len(wsHosts()) {
		t.Errorf("unknown workspace should mean All, got %q with %d hosts", l.Workspace(), len(l.Visible()))
	}
	l.SetWorkspace("Prod")
	l.SetWorkspaceData([]string{"Dev"}, nil)
	if l.Workspace() != "" {
		t.Errorf("removing the active workspace should fall back to All")
	}
}

func TestGroupSearchOnlyFiltersTheCurrentContext(t *testing.T) {
	l := wsList(t)
	l, _ = l.Update(runes("/"))
	l, _ = l.Update(runes("prod"))
	if len(l.Visible()) != 2 {
		t.Fatalf("All search for prod should match both prod hosts, got %d", len(l.Visible()))
	}
	l, _ = l.Update(special(27)) // esc clears
	l.SetWorkspace("Prod")
	l, _ = l.Update(runes("/"))
	l, _ = l.Update(runes("web"))
	if l.Selected() != nil || len(l.Visible()) != 0 {
		t.Errorf("search inside Prod must not reach web: %d visible", len(l.Visible()))
	}
	v := l.View(60, 10)
	if !strings.Contains(v, "No matches in Prod") {
		t.Errorf("empty search should name the context:\n%s", v)
	}
	l, _ = l.Update(special(27))
	l, _ = l.Update(runes("/"))
	l, _ = l.Update(runes("prod"))
	if len(l.Visible()) != 2 {
		t.Errorf("search inside Prod should match both prod hosts, got %d", len(l.Visible()))
	}
}

func TestGroupHeadingsAreNotSelectable(t *testing.T) {
	l := NewHostList()
	l.SetHosts(wsHosts())
	l.SetWorkspaceData([]string{"Dev", "Prod"}, wsAssignments())
	if l.Selected() == nil || l.Selected().Alias != "dev-1" {
		t.Fatalf("first row should be the first host, got %v", l.Selected())
	}
	if !strings.Contains(l.View(60, 12), "Dev (1)") {
		t.Errorf("heading missing")
	}
	for i := 0; i < 10; i++ {
		l, _ = l.Update(runes("j"))
	}
	if s := l.Selected(); s == nil || s.Alias != "*" {
		t.Fatalf("moving down must stay on hosts, ended at %v", l.Selected())
	}
	for i := 0; i < 10; i++ {
		l, _ = l.Update(runes("k"))
	}
	if s := l.Selected(); s == nil || s.Alias != "dev-1" {
		t.Fatalf("moving back up must stop at the first host, got %v", l.Selected())
	}
}

func TestGroupSelectionFollowsAliasAcrossContexts(t *testing.T) {
	l := wsList(t)
	l.SelectAlias("prod-2")
	l.SetWorkspace("Prod")
	if s := l.Selected(); s == nil || s.Alias != "prod-2" {
		t.Errorf("selection should follow the alias into Prod, got %v", l.Selected())
	}
	l.SetWorkspace("")
	if s := l.Selected(); s == nil || s.Alias != "prod-2" {
		t.Errorf("selection should follow the alias back to All, got %v", l.Selected())
	}
	l.SetWorkspaceData([]string{"Dev", "Prod"}, map[string]string{"dev-1": "Dev"})
	l.SelectAlias("prod-2")
	if s := l.Selected(); s == nil || s.Alias != "prod-2" {
		t.Errorf("existing alias should stay selected, got %v", l.Selected())
	}
}

func TestGroupPatternsStayVisibleAndAreNeverAssignedByDefault(t *testing.T) {
	l := wsList(t)
	for _, ws := range []string{"", "Dev", "Prod"} {
		l.SetWorkspace(ws)
		if l.WorkspaceOf("*") != "" && l.Workspace() == "" {
			t.Errorf("pattern must not be assigned")
		}
	}
	l.SetWorkspace("")
	if l.WorkspaceOf("*") != "" {
		t.Errorf("pattern assignment came from nowhere")
	}
	v := l.View(60, 14)
	if !strings.Contains(v, "pattern") {
		t.Errorf("pattern row should stay visible and flagged:\n%s", v)
	}
}

func TestGroupEmptyWorkspaceExplainsHowToAssign(t *testing.T) {
	l := NewHostList()
	l.SetHosts(wsHosts())
	l.SetWorkspaceData([]string{"Empty"}, nil)
	l.SetWorkspace("Empty")
	v := l.View(60, 10)
	if !strings.Contains(v, "No hosts in Empty") {
		t.Errorf("empty workspace should say so:\n%s", v)
	}
	if l.Selected() != nil {
		t.Errorf("empty workspace has no selection")
	}
}

func TestGroupScrollsWithHeadingsTakenIntoAccount(t *testing.T) {
	var hosts []*config.HostEntry
	for i := 0; i < 20; i++ {
		hosts = append(hosts, &config.HostEntry{Alias: "h" + string(rune('a'+i))})
	}
	l := NewHostList()
	l.SetHosts(hosts)
	l.SetWorkspaceData([]string{"A", "B"}, map[string]string{"ha": "A", "hb": "A", "hc": "B", "hd": "B"})
	for i := 0; i < 15; i++ {
		l, _ = l.Update(runes("j"))
	}
	v := l.View(40, 8)
	if got := lines(v); got != 8 {
		t.Fatalf("height = %d", got)
	}
	if s := l.Selected(); s == nil || !strings.Contains(v, s.Alias) {
		t.Errorf("cursor row %v not visible:\n%s", l.Selected(), v)
	}
	if strings.Contains(v, "ha") {
		t.Errorf("top rows should have scrolled away:\n%s", v)
	}
}

func TestGroupViewsFitNarrowWidth(t *testing.T) {
	l := wsList(t)
	l.SetWorkspaceData([]string{strings.Repeat("wide", 20), "Prod"}, wsAssignments())
	for _, ws := range []string{"", "Prod"} {
		l.SetWorkspace(ws)
		for _, ln := range strings.Split(l.View(20, 10), "\n") {
			if w := lipglossWidth(ln); w > 20 {
				t.Errorf("%q: line wider than 20: %d %q", ws, w, ln)
			}
		}
	}
}
