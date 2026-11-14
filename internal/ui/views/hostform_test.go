package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
)

func typeText(f HostForm, s string) HostForm {
	for _, r := range s {
		if r == ' ' {
			f, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		f, _ = f.Update(runes(string(r)))
	}
	return f
}

func next(f HostForm) HostForm { f, _ = f.Update(special(tea.KeyTab)); return f }

func submit(f HostForm) (HostForm, tea.Msg) {
	f, cmd := f.Update(special(tea.KeyEnter))
	return f, msgOf(cmd)
}

func fillNew(t *testing.T, existing func(string) bool) HostForm {
	t.Helper()
	f := NewHostForm(nil, nil, existing)
	f = typeText(f, "test-box")
	f = next(f)
	f = typeText(f, "127.0.0.1")
	f = next(f)
	f = typeText(f, "admin")
	return f
}

func TestHostFormNewDefaultsPortTo22AndSubmitsEntry(t *testing.T) {
	f := fillNew(t, func(string) bool { return false })
	f = next(f)
	if f.values()[3] != "22" {
		t.Fatalf("port should default to 22, got %q", f.values()[3])
	}
	f, msg := submit(f)
	got, ok := msg.(HostFormSubmitMsg)
	if !ok {
		t.Fatalf("want HostFormSubmitMsg, got %T (err=%q)", msg, f.Err())
	}
	e := got.Entry
	if e.Alias != "test-box" || e.HostName != "127.0.0.1" || e.User != "admin" || e.Port != "22" {
		t.Errorf("entry wrong: %+v", e)
	}
	if got.OriginalAlias != "" {
		t.Errorf("new host has no original alias")
	}
}

func TestHostFormPortStaysImplicitWhenDefault(t *testing.T) {
	f := fillNew(t, func(string) bool { return false })
	_, msg := submit(f)
	e := msg.(HostFormSubmitMsg).Entry
	if e.Port != "22" && e.Port != "" {
		t.Errorf("unexpected port %q", e.Port)
	}
}

func TestHostFormRequiresAlias(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	f, msg := submit(f)
	if msg != nil {
		t.Fatalf("empty alias must not submit")
	}
	if !strings.Contains(strings.ToLower(f.Err()), "alias") {
		t.Errorf("error should mention alias, got %q", f.Err())
	}
}

func TestHostFormRejectsBadAliasCharacters(t *testing.T) {
	for _, bad := range []string{"my box", "web*", "-oProxy"} {
		f := NewHostForm(nil, nil, func(string) bool { return false })
		f = typeText(f, bad)
		f, msg := submit(f)
		if msg != nil || f.Err() == "" {
			t.Errorf("alias %q must be rejected (msg=%v)", bad, msg)
		}
	}
}

func TestHostFormRejectsDuplicateAliasOnAdd(t *testing.T) {
	f := NewHostForm(nil, nil, func(a string) bool { return a == "web" })
	f = typeText(f, "web")
	f, msg := submit(f)
	if msg != nil || !strings.Contains(strings.ToLower(f.Err()), "exists") {
		t.Errorf("duplicate alias must be rejected, err=%q", f.Err())
	}
}

func TestHostFormRejectsInvalidPort(t *testing.T) {
	for _, bad := range []string{"0", "70000", "abc", "-1", "22x"} {
		f := fillNew(t, func(string) bool { return false })
		f = next(f)
		for i := 0; i < 5; i++ {
			f, _ = f.Update(special(tea.KeyBackspace))
		}
		f = typeText(f, bad)
		f, msg := submit(f)
		if msg != nil || !strings.Contains(strings.ToLower(f.Err()), "port") {
			t.Errorf("port %q must be rejected (msg=%v err=%q)", bad, msg, f.Err())
		}
	}
}

func TestHostFormRejectsNewlinesCannotBeTyped(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nHost evil")})
	f, msg := submit(f)
	if msg != nil {
		if e := msg.(HostFormSubmitMsg).Entry; strings.ContainsAny(e.Alias, "\r\n") {
			t.Errorf("newline leaked into alias: %q", e.Alias)
		}
	}
}

func TestHostFormEditPrefillsAndKeepsExtras(t *testing.T) {
	orig := &config.HostEntry{Alias: "web", HostName: "10.0.0.1", User: "root", Port: "2222", IdentityFile: "k", ExtraDirectives: map[string]string{"ForwardAgent": "yes"}}
	f := NewHostForm(orig, nil, func(a string) bool { return a == "web" })
	v := f.values()
	if v[0] != "web" || v[1] != "10.0.0.1" || v[2] != "root" || v[3] != "2222" || v[4] != "k" {
		t.Fatalf("prefill wrong: %v", v)
	}
	f = next(f)
	f = next(f)
	f = next(f)
	for i := 0; i < 4; i++ {
		f, _ = f.Update(special(tea.KeyBackspace))
	}
	f = typeText(f, "2200")
	f, msg := submit(f)
	got, ok := msg.(HostFormSubmitMsg)
	if !ok {
		t.Fatalf("edit should submit, err=%q", f.Err())
	}
	if got.Entry.Port != "2200" || got.OriginalAlias != "web" {
		t.Errorf("edit result wrong: %+v orig=%q", got.Entry, got.OriginalAlias)
	}
	if got.Entry.ExtraDirectives["ForwardAgent"] != "yes" {
		t.Errorf("extras must pass through edit: %+v", got.Entry.ExtraDirectives)
	}
}

func TestHostFormEditAliasUnchangedIsNotADuplicate(t *testing.T) {
	orig := &config.HostEntry{Alias: "web", HostName: "h"}
	f := NewHostForm(orig, nil, func(a string) bool { return a == "web" })
	_, msg := submit(f)
	if _, ok := msg.(HostFormSubmitMsg); !ok {
		t.Errorf("saving an unchanged alias must work")
	}
}

func TestHostFormEscCancels(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	_, cmd := f.Update(special(tea.KeyEsc))
	if _, ok := msgOf(cmd).(HostFormCancelMsg); !ok {
		t.Errorf("esc should emit HostFormCancelMsg")
	}
}

func TestHostFormTabCyclesAndShiftTabReverses(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	for i := 0; i < 5; i++ {
		f = next(f)
	}
	if f.focus != 0 {
		t.Errorf("tab should wrap to first field, at %d", f.focus)
	}
	f, _ = f.Update(special(tea.KeyShiftTab))
	if f.focus != 4 {
		t.Errorf("shift+tab should wrap back to last field, at %d", f.focus)
	}
}

func TestHostFormKeySuggestionsCycleIntoIdentityField(t *testing.T) {
	keys := []string{`C:\Users\me\.ssh\id_ed25519`, `C:\Users\me\.ssh\id_work`}
	f := NewHostForm(nil, keys, func(string) bool { return false })
	f = typeText(f, "box")
	for i := 0; i < 4; i++ {
		f = next(f)
	}
	f, _ = f.Update(special(tea.KeyDown))
	if f.values()[4] != keys[0] {
		t.Errorf("down should fill first key, got %q", f.values()[4])
	}
	f, _ = f.Update(special(tea.KeyDown))
	if f.values()[4] != keys[1] {
		t.Errorf("down again should fill second key, got %q", f.values()[4])
	}
	f, _ = f.Update(special(tea.KeyUp))
	if f.values()[4] != keys[0] {
		t.Errorf("up should go back, got %q", f.values()[4])
	}
}

func TestHostFormViewShowsLabelsAndError(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	f, _ = submit(f)
	v := f.View(60)
	for _, want := range []string{"Alias", "Hostname", "User", "Port", "Identity", "Add host", "alias"} {
		if !strings.Contains(v, want) {
			t.Errorf("form view missing %q:\n%s", want, v)
		}
	}
	e := NewHostForm(&config.HostEntry{Alias: "web"}, nil, func(string) bool { return true })
	if !strings.Contains(e.View(60), "Edit host") {
		t.Errorf("edit form should be titled Edit host")
	}
}

func TestHostFormEditPatternAliasUnchangedIsAllowed(t *testing.T) {
	orig := &config.HostEntry{Alias: "*", IsPattern: true, ExtraDirectives: map[string]string{"Compression": "yes"}}
	f := NewHostForm(orig, nil, func(a string) bool { return a == "*" })
	_, msg := submit(f)
	if _, ok := msg.(HostFormSubmitMsg); !ok {
		t.Errorf("editing Host * without renaming it must save")
	}
}

func TestHostFormEditPatternRenameToAnotherPatternIsRejected(t *testing.T) {
	orig := &config.HostEntry{Alias: "*", IsPattern: true}
	f := NewHostForm(orig, nil, func(a string) bool { return a == "*" })
	f = typeText(f, "x?")
	f, msg := submit(f)
	if msg != nil || f.Err() == "" {
		t.Errorf("changing the alias must still be validated")
	}
}

func TestHostFormSetErrShowsInView(t *testing.T) {
	f := NewHostForm(nil, nil, func(string) bool { return false })
	f = f.SetErr("host not found: web")
	if f.Err() != "host not found: web" || !strings.Contains(f.View(60), "host not found: web") {
		t.Errorf("SetErr should be visible")
	}
}
