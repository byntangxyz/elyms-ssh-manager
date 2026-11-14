package views

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
)

func sampleHosts() []*config.HostEntry {
	return []*config.HostEntry{
		{Alias: "web", HostName: "10.0.0.1", User: "root", Port: "2222", IdentityFile: `C:\k\id`, ExtraDirectives: map[string]string{"ForwardAgent": "yes"}},
		{Alias: "db", HostName: "db.example.com", User: "admin"},
		{Alias: "*", IsPattern: true, ExtraDirectives: map[string]string{"Compression": "yes"}},
	}
}

func TestHostListSelectedFollowsCursor(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	if l.Selected().Alias != "web" {
		t.Fatalf("first host should be selected, got %v", l.Selected())
	}
	l, _ = l.Update(special(tea.KeyDown))
	if l.Selected().Alias != "db" {
		t.Errorf("down should select db, got %v", l.Selected().Alias)
	}
	l, _ = l.Update(runes("j"))
	l, _ = l.Update(runes("j"))
	if l.Selected().Alias != "*" {
		t.Errorf("cursor must clamp at last row, got %v", l.Selected().Alias)
	}
	l, _ = l.Update(runes("k"))
	if l.Selected().Alias != "db" {
		t.Errorf("k should move up, got %v", l.Selected().Alias)
	}
}

func TestHostListEmptyHasNoSelection(t *testing.T) {
	l := NewHostList()
	l.SetHosts(nil)
	if l.Selected() != nil {
		t.Errorf("empty list must have nil selection")
	}
	l, _ = l.Update(special(tea.KeyDown))
	if !strings.Contains(l.View(40, 10), "No hosts") {
		t.Errorf("empty view should explain how to add a host:\n%s", l.View(40, 10))
	}
}

func TestHostListFilterNarrowsByAliasHostnameAndUser(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	l, _ = l.Update(runes("/"))
	if !l.Filtering() {
		t.Fatal("/ should start filtering")
	}
	for _, r := range "EXAMPLE" {
		l, _ = l.Update(runes(string(r)))
	}
	if n := len(l.Visible()); n != 1 || l.Selected().Alias != "db" {
		t.Fatalf("hostname filter should leave only db, got %d", n)
	}
	l, _ = l.Update(special(tea.KeyEnter))
	if l.Filtering() {
		t.Errorf("enter should leave typing mode")
	}
	if len(l.Visible()) != 1 {
		t.Errorf("enter must keep the filter applied")
	}
	l, _ = l.Update(special(tea.KeyEsc))
	if len(l.Visible()) != 3 {
		t.Errorf("esc should clear the filter, got %d visible", len(l.Visible()))
	}
}

func TestHostListFilterEscWhileTypingClears(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	l, _ = l.Update(runes("/"))
	l, _ = l.Update(runes("r"))
	l, _ = l.Update(runes("o"))
	l, _ = l.Update(special(tea.KeyEsc))
	if l.Filtering() || len(l.Visible()) != 3 {
		t.Errorf("esc while typing should clear and stop filtering")
	}
}

func TestHostListFilterWithNoMatchHasNilSelection(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	l, _ = l.Update(runes("/"))
	for _, r := range "zzzz" {
		l, _ = l.Update(runes(string(r)))
	}
	if l.Selected() != nil {
		t.Errorf("no match must give nil selection")
	}
	if !strings.Contains(l.View(40, 10), "No matches") {
		t.Errorf("view should say no matches")
	}
}

func TestHostListTypingJKInFilterDoesNotMoveCursor(t *testing.T) {
	l := NewHostList()
	l.SetHosts([]*config.HostEntry{{Alias: "jk-one"}, {Alias: "jk-two"}})
	l, _ = l.Update(runes("/"))
	l, _ = l.Update(runes("j"))
	l, _ = l.Update(runes("k"))
	if len(l.Visible()) != 2 || l.Selected().Alias != "jk-one" {
		t.Errorf("j/k typed in the filter must be text, not navigation")
	}
}

func TestHostListSetHostsKeepsSelectionByAlias(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	l, _ = l.Update(special(tea.KeyDown))
	hosts := sampleHosts()
	hosts = append([]*config.HostEntry{{Alias: "aaa", HostName: "x"}}, hosts...)
	l.SetHosts(hosts)
	if l.Selected().Alias != "db" {
		t.Errorf("selection should follow the alias after a reload, got %s", l.Selected().Alias)
	}
}

func TestHostListSelectAliasMovesCursor(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	l.SelectAlias("db")
	if l.Selected().Alias != "db" {
		t.Errorf("SelectAlias failed")
	}
}

func TestHostListViewShowsRowsAndFlagsPatterns(t *testing.T) {
	l := NewHostList()
	l.SetHosts(sampleHosts())
	v := l.View(50, 12)
	for _, want := range []string{"web", "db", "*", "pattern"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestHostListViewScrollsToKeepCursorVisibleAndHonorsHeight(t *testing.T) {
	var hosts []*config.HostEntry
	for i := 0; i < 30; i++ {
		hosts = append(hosts, &config.HostEntry{Alias: fmt.Sprintf("host%02d", i), HostName: "h"})
	}
	l := NewHostList()
	l.SetHosts(hosts)
	for i := 0; i < 25; i++ {
		l, _ = l.Update(special(tea.KeyDown))
	}
	v := l.View(40, 10)
	if got := lines(v); got != 10 {
		t.Errorf("view must be exactly 10 lines tall, got %d", got)
	}
	if !strings.Contains(v, "host25") || strings.Contains(v, "host00") {
		t.Errorf("view should be scrolled to host25:\n%s", v)
	}
}

func TestHostListViewNeverExceedsWidth(t *testing.T) {
	l := NewHostList()
	l.SetHosts([]*config.HostEntry{{Alias: strings.Repeat("a", 80), HostName: strings.Repeat("b", 80), User: "u"}})
	for _, ln := range strings.Split(l.View(30, 6), "\n") {
		if w := lipglossWidth(ln); w > 30 {
			t.Errorf("line wider than 30: %d %q", w, ln)
		}
	}
}

func TestInspectorShowsAllFieldsAndConnectCommand(t *testing.T) {
	v := Inspector(sampleHosts()[0], 50, 20)
	for _, want := range []string{"web", "10.0.0.1", "root", "2222", `C:\k\id`, "ForwardAgent", "yes", "ssh web"} {
		if !strings.Contains(v, want) {
			t.Errorf("inspector missing %q:\n%s", want, v)
		}
	}
}

func TestInspectorDefaultsPortAndHostName(t *testing.T) {
	v := Inspector(&config.HostEntry{Alias: "bare"}, 50, 20)
	if !strings.Contains(v, "22") || !strings.Contains(v, "bare") {
		t.Errorf("port should default to 22 and hostname to the alias:\n%s", v)
	}
}

func TestInspectorExplainsPatternsAreNotConnectable(t *testing.T) {
	v := Inspector(sampleHosts()[2], 50, 20)
	if !strings.Contains(v, "pattern") || strings.Contains(v, "ssh *") {
		t.Errorf("pattern entries must be described as defaults, not connect targets:\n%s", v)
	}
}

func TestInspectorNilIsEmptyStateNotPanic(t *testing.T) {
	if Inspector(nil, 40, 10) == "" {
		t.Errorf("nil entry should render an empty state")
	}
}

func TestTargetFormatsUserHostPort(t *testing.T) {
	if got := Target(sampleHosts()[0]); got != "root@10.0.0.1:2222" {
		t.Errorf("got %q", got)
	}
	if got := Target(&config.HostEntry{Alias: "x"}); got != "x" {
		t.Errorf("got %q", got)
	}
}

func TestHostListSetHeightControlsPageSize(t *testing.T) {
	var hosts []*config.HostEntry
	for i := 0; i < 40; i++ {
		hosts = append(hosts, &config.HostEntry{Alias: fmt.Sprintf("h%02d", i)})
	}
	l := NewHostList()
	l.SetHosts(hosts)
	l.SetHeight(21)
	l, _ = l.Update(special(tea.KeyPgDown))
	if l.Selected().Alias != "h20" {
		t.Errorf("page down should move by height-1 rows, got %s", l.Selected().Alias)
	}
}

func TestInspectorLongDirectiveNamesDoNotWrapAndRespectHeight(t *testing.T) {
	extras := map[string]string{}
	for _, k := range []string{"StrictHostKeyChecking", "ServerAliveInterval", "IdentitiesOnly", "UserKnownHostsFile", "PreferredAuthentications", "ServerAliveCountMax", "ControlPersist", "HostKeyAlgorithms"} {
		extras[k] = "some-fairly-long-value-" + k
	}
	h := &config.HostEntry{Alias: "web", HostName: "10.0.0.1", User: "root", ExtraDirectives: extras}
	v := Inspector(h, 40, 12)
	if got := lines(v); got > 12 {
		t.Errorf("inspector must fit 12 lines, got %d:\n%s", got, v)
	}
	for _, ln := range strings.Split(v, "\n") {
		if w := lipglossWidth(ln); w > 40 {
			t.Errorf("line wider than 40: %d %q", w, ln)
		}
	}
	if !strings.Contains(v, "web") {
		t.Errorf("title must stay visible")
	}
}

func TestHostListMarksDuplicateAliases(t *testing.T) {
	l := NewHostList()
	l.SetHosts([]*config.HostEntry{{Alias: "a", HostName: "1", Duplicate: true}, {Alias: "b", HostName: "2"}})
	v := l.View(50, 6)
	if !strings.Contains(v, "dup") {
		t.Errorf("duplicate alias should be flagged in the list:\n%s", v)
	}
	if strings.Count(v, "dup") != 1 {
		t.Errorf("only the duplicate row is flagged:\n%s", v)
	}
}
