package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/ssh"
)

const cfgSample = `# my servers
Host web
  HostName 10.0.0.1
  User root
  ForwardAgent yes

Host db
  HostName 10.0.0.2

Host *
  Compression yes
`

type env struct {
	t         *testing.T
	cfgPath   string
	sshDir    string
	connected []string
	copied    string
}

func newEnv(t *testing.T, content string) (*env, Model) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, sshDir: dir, cfgPath: filepath.Join(dir, "config")}
	if content != "" {
		if err := os.WriteFile(e.cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(Options{
		ConfigPath: e.cfgPath,
		SSHDir:     dir,
		Connect: func(alias string) tea.Cmd {
			e.connected = append(e.connected, alias)
			return func() tea.Msg { return ssh.FinishedMsg{Alias: alias} }
		},
		Copy:         func(s string) error { e.copied = s; return nil },
		SSHAvailable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	return e, m
}

// runCmd runs a command but gives up on slow ones (cursor blink timers), so
// tests do not sleep through them.
func runCmd(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case out := <-done:
		return out
	case <-time.After(20 * time.Millisecond):
		return nil
	}
}

func send(m Model, msg tea.Msg) Model {
	next, cmd := m.Update(msg)
	m = next.(Model)
	for cmd != nil {
		out := runCmd(cmd)
		if out == nil {
			break
		}
		if _, isBatch := out.(tea.BatchMsg); isBatch {
			break
		}
		next, cmd = m.Update(out)
		m = next.(Model)
	}
	return m
}

func key(m Model, s string) Model {
	switch s {
	case "enter":
		return send(m, tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		return send(m, tea.KeyMsg{Type: tea.KeyEsc})
	case "tab":
		return send(m, tea.KeyMsg{Type: tea.KeyTab})
	case "down":
		return send(m, tea.KeyMsg{Type: tea.KeyDown})
	case "backspace":
		return send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	return send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func typ(m Model, s string) Model {
	for _, r := range s {
		m = key(m, string(r))
	}
	return m
}

func (e *env) file() string {
	b, err := os.ReadFile(e.cfgPath)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func TestStartupShowsHostsInspectorAndHints(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	v := m.View()
	for _, want := range []string{"elyms", "web", "db", "10.0.0.1", "ssh web", "connect", "add", "quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("startup view missing %q:\n%s", want, v)
		}
	}
}

func TestStartupWithNoConfigCreatesItAndShowsEmptyState(t *testing.T) {
	e, m := newEnv(t, "")
	if !strings.Contains(m.View(), "No hosts") {
		t.Errorf("empty state missing:\n%s", m.View())
	}
	if _, err := os.Stat(e.cfgPath); err != nil {
		t.Errorf("config should exist: %v", err)
	}
}

func TestEnterConnectsToSelectedHostAndReturnsToList(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "down")
	m = key(m, "enter")
	if len(e.connected) != 1 || e.connected[0] != "db" {
		t.Fatalf("connected = %v", e.connected)
	}
	if !strings.Contains(m.View(), "db") {
		t.Errorf("list should be back after the session")
	}
}

func TestSSHErrorAfterSessionIsShownInStatusBar(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = send(m, ssh.FinishedMsg{Alias: "web", Err: os.ErrPermission})
	if !strings.Contains(m.View(), "web") || !strings.Contains(strings.ToLower(m.View()), "permission") {
		t.Errorf("ssh error should be visible:\n%s", m.View())
	}
}

func TestEnterOnPatternDoesNotConnect(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "G")
	m = key(m, "enter")
	if len(e.connected) != 0 {
		t.Errorf("pattern host must not connect: %v", e.connected)
	}
	if !strings.Contains(strings.ToLower(m.View()), "pattern") {
		t.Errorf("should explain why:\n%s", m.View())
	}
}

func TestEnterWithNoHostsDoesNothing(t *testing.T) {
	e, m := newEnv(t, "")
	m = key(m, "enter")
	if len(e.connected) != 0 {
		t.Errorf("nothing to connect to")
	}
}

func TestConnectUnavailableSSHShowsHelpfulMessage(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m.opts.SSHAvailable = false
	m = key(m, "enter")
	if len(e.connected) != 0 || !strings.Contains(strings.ToLower(m.View()), "openssh") {
		t.Errorf("should explain OpenSSH is missing:\n%s", m.View())
	}
}

func TestAddHostWritesConfigAndSelectsIt(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "a")
	m = typ(m, "test-box")
	m = key(m, "tab")
	m = typ(m, "127.0.0.1")
	m = key(m, "tab")
	m = typ(m, "admin")
	m = key(m, "enter")
	got := e.file()
	block := "Host test-box\n  HostName 127.0.0.1\n  User admin\n  Port 22\n\n"
	if strings.Replace(got, block, "", 1) != cfgSample {
		t.Errorf("existing content must be untouched apart from the new block:\n%s", got)
	}
	if !strings.Contains(got, block) {
		t.Errorf("new block missing:\n%s", got)
	}
	if !strings.Contains(m.View(), "ssh test-box") {
		t.Errorf("new host should be selected and shown:\n%s", m.View())
	}
	if _, err := os.Stat(e.cfgPath + ".bak"); err != nil {
		t.Errorf("backup should exist: %v", err)
	}
}

func TestAddDuplicateAliasShowsErrorAndKeepsFormOpen(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "a")
	m = typ(m, "web")
	m = key(m, "enter")
	if !strings.Contains(strings.ToLower(m.View()), "exists") {
		t.Errorf("duplicate error missing:\n%s", m.View())
	}
	if e.file() != cfgSample {
		t.Errorf("config must be untouched")
	}
}

func TestEditHostUpdatesInPlaceKeepingCommentsAndExtras(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "e")
	for i := 0; i < 3; i++ {
		m = key(m, "tab")
	}
	m = key(m, "backspace")
	m = key(m, "backspace")
	m = typ(m, "2200")
	m = key(m, "enter")
	got := e.file()
	for _, must := range []string{"# my servers", "ForwardAgent yes", "Port 2200", "Host db", "Compression yes"} {
		if !strings.Contains(got, must) {
			t.Errorf("missing %q:\n%s", must, got)
		}
	}
}

func TestEditRenamingAliasRenamesBlockInPlace(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "e")
	m = typ(m, "2")
	m = key(m, "enter")
	got := e.file()
	if !strings.Contains(got, "Host web2\n  HostName 10.0.0.1\n  User root\n  ForwardAgent yes") {
		t.Errorf("block should be renamed with body intact:\n%s", got)
	}
	if strings.Contains(got, "Host web\n") {
		t.Errorf("old alias should be gone:\n%s", got)
	}
}

func TestEditPatternHostIsAllowedButDeleteConfirms(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = key(m, "G")
	m = key(m, "d")
	if !strings.Contains(m.View(), "Delete") {
		t.Errorf("delete should ask for confirmation:\n%s", m.View())
	}
}

func TestDeleteRequiresYAndRemovesBlock(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "d")
	m = key(m, "enter")
	if !strings.Contains(e.file(), "Host web") {
		t.Fatalf("enter must not delete")
	}
	m = key(m, "d")
	m = key(m, "y")
	got := e.file()
	if strings.Contains(got, "Host web") || strings.Contains(got, "ForwardAgent") {
		t.Errorf("web should be gone:\n%s", got)
	}
	if !strings.Contains(got, "Host db") || !strings.Contains(got, "# my servers") {
		t.Errorf("others must remain:\n%s", got)
	}
	if strings.Contains(m.View(), "ssh web") {
		t.Errorf("list should refresh")
	}
}

func TestExternalEditsAreNotClobberedByNextWrite(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	if err := os.WriteFile(e.cfgPath, []byte(cfgSample+"\nHost external\n  HostName 9.9.9.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = key(m, "a")
	m = typ(m, "mine")
	m = key(m, "enter")
	got := e.file()
	if !strings.Contains(got, "Host external") || !strings.Contains(got, "Host mine") {
		t.Errorf("both external and new host must be present:\n%s", got)
	}
	if !strings.Contains(m.View(), "external") {
		t.Errorf("list should show the externally added host after reload")
	}
}

func TestKeyManagerOpensFromListAndEscReturns(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = key(m, "k")
	if !strings.Contains(m.View(), "Keys") {
		t.Errorf("key manager should open:\n%s", m.View())
	}
	m = key(m, "esc")
	if strings.Contains(m.View(), "Keys") || !strings.Contains(m.View(), "web") {
		t.Errorf("esc should return to the host list")
	}
}

func TestKeyManagerGeneratesIntoSSHDirAndFormOffersIt(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "k")
	m = key(m, "n")
	m = typ(m, "id_elyms_test")
	m = key(m, "enter")
	if _, err := os.Stat(filepath.Join(e.sshDir, "id_elyms_test")); err != nil {
		t.Fatalf("private key not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.sshDir, "id_elyms_test.pub")); err != nil {
		t.Fatalf("public key not written: %v", err)
	}
	m = key(m, "esc")
	m = key(m, "esc")
	m = key(m, "a")
	m = typ(m, "box")
	for i := 0; i < 4; i++ {
		m = key(m, "tab")
	}
	m = key(m, "down")
	m = key(m, "enter")
	if !strings.Contains(e.file(), "IdentityFile "+filepath.Join(e.sshDir, "id_elyms_test")) {
		t.Errorf("identity file from key list should be written:\n%s", e.file())
	}
}

func TestKeyManagerCopyUsesInjectedClipboard(t *testing.T) {
	e, m := newEnv(t, cfgSample)
	m = key(m, "k")
	m = key(m, "n")
	m = typ(m, "id_c")
	m = key(m, "enter")
	m = key(m, "c")
	if !strings.HasPrefix(e.copied, "ssh-ed25519 ") {
		t.Errorf("clipboard = %q", e.copied)
	}
}

func TestQuitKeyQuits(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q should quit")
	}
}

func TestQDoesNotQuitWhileTypingInFilterOrForm(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = key(m, "/")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Errorf("q while filtering must be text")
		}
	}
	m = key(m, "esc")
	m = key(m, "a")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Errorf("q in a form must be text")
		}
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = key(m, "a")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should quit even inside a form")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c should quit")
	}
}

func TestSmallTerminalShowsHintInsteadOfBrokenLayout(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	m = send(m, tea.WindowSizeMsg{Width: 30, Height: 8})
	if !strings.Contains(strings.ToLower(m.View()), "too small") {
		t.Errorf("tiny terminals should get a resize hint:\n%s", m.View())
	}
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if strings.Contains(strings.ToLower(m.View()), "too small") {
		t.Errorf("hint should clear after resize")
	}
}

func TestViewFitsTerminalSize(t *testing.T) {
	for _, sz := range [][2]int{{120, 30}, {80, 24}, {60, 15}} {
		_, m := newEnv(t, cfgSample)
		m = send(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		v := m.View()
		rows := strings.Split(v, "\n")
		if len(rows) > sz[1] {
			t.Errorf("%dx%d: view has %d rows", sz[0], sz[1], len(rows))
		}
		for _, r := range rows {
			if w := lipglossWidth(r); w > sz[0] {
				t.Errorf("%dx%d: row width %d: %q", sz[0], sz[1], w, r)
			}
		}
	}
}

func TestModalsFitTerminalSize(t *testing.T) {
	for _, open := range []string{"a", "k", "d"} {
		_, m := newEnv(t, cfgSample)
		m = send(m, tea.WindowSizeMsg{Width: 70, Height: 24})
		m = key(m, open)
		rows := strings.Split(m.View(), "\n")
		if len(rows) > 24 {
			t.Errorf("modal %q: %d rows", open, len(rows))
		}
		for _, r := range rows {
			if w := lipglossWidth(r); w > 70 {
				t.Errorf("modal %q: row width %d", open, w)
			}
		}
	}
}

func TestMalformedConfigStillLoads(t *testing.T) {
	_, m := newEnv(t, "Host\n  HostName\nHost ok\n  HostName 1.1.1.1\n=\n\x00garbage\n")
	if !strings.Contains(m.View(), "ok") {
		t.Errorf("good hosts should still show:\n%s", m.View())
	}
}

func TestEditAndDeleteAreRefusedOnDuplicateAliases(t *testing.T) {
	e, m := newEnv(t, "Host a\n  HostName 1\n\nHost a\n  HostName 2\n")
	for _, k := range []string{"e", "d"} {
		m = key(m, k)
		if m.mode != modeList {
			t.Fatalf("%q must not open a dialog on a duplicate alias", k)
		}
		if !strings.Contains(strings.ToLower(m.View()), "duplicate") {
			t.Errorf("%q should explain why:\n%s", k, m.View())
		}
	}
	if e.file() != "Host a\n  HostName 1\n\nHost a\n  HostName 2\n" {
		t.Errorf("config must be untouched")
	}
}

func TestSSHExit255ShowsConnectionHintAndRemoteExitIsNotAnError(t *testing.T) {
	_, m := newEnv(t, cfgSample)
	cmd := exec.Command("cmd", "/c", "exit 255")
	m = send(m, ssh.FinishedMsg{Alias: "web", Err: cmd.Run()})
	if !strings.Contains(m.View(), "Could not connect to web") || !m.statusErr {
		t.Errorf("255 should show the connection hint:\n%s", m.View())
	}
	cmd = exec.Command("cmd", "/c", "exit 3")
	m = send(m, ssh.FinishedMsg{Alias: "web", Err: cmd.Run()})
	if m.statusErr {
		t.Errorf("remote exit code is not an ssh failure")
	}
}

func TestModalsFitMinimumTerminalSize(t *testing.T) {
	for _, open := range []string{"a", "k", "d"} {
		_, m := newEnv(t, cfgSample)
		m = send(m, tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
		m = key(m, open)
		rows := strings.Split(m.View(), "\n")
		if len(rows) > minHeight {
			t.Errorf("modal %q: %d rows at minimum height %d:\n%s", open, len(rows), minHeight, m.View())
		}
		for _, r := range rows {
			if w := lipglossWidth(r); w > minWidth {
				t.Errorf("modal %q: row width %d", open, w)
			}
		}
	}
	_, m := newEnv(t, cfgSample)
	m = send(m, tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	m = key(m, "a")
	m = key(m, "enter")
	if n := len(strings.Split(m.View(), "\n")); n > minHeight {
		t.Errorf("form with error: %d rows", n)
	}
}
