package views

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/keygen"
)

type fakeKeys struct {
	list      []*keygen.KeyInfo
	generated []string
	genErr    error
	pub       string
	copied    string
	copyErr   error
}

func (f *fakeKeys) deps() KeyDeps {
	return KeyDeps{
		List: func() ([]*keygen.KeyInfo, error) { return f.list, nil },
		Generate: func(name string, t keygen.KeyType, comment string) (*keygen.KeyInfo, error) {
			if f.genErr != nil {
				return nil, f.genErr
			}
			f.generated = append(f.generated, name+"|"+string(t)+"|"+comment)
			k := &keygen.KeyInfo{Name: name, Type: t, Comment: comment, HasPrivate: true, Fingerprint: "SHA256:new"}
			f.list = append(f.list, k)
			return k, nil
		},
		ReadPublic: func(k *keygen.KeyInfo) (string, error) { return f.pub, nil },
		Copy:       func(s string) error { f.copied = s; return f.copyErr },
	}
}

func twoKeys() *fakeKeys {
	return &fakeKeys{
		list: []*keygen.KeyInfo{
			{Name: "id_ed25519", Path: `C:\k\id_ed25519`, Type: keygen.TypeEd25519, Fingerprint: "SHA256:aaa", Comment: "me@x", HasPrivate: true},
			{Name: "old", Path: `C:\k\old`, Type: keygen.TypeRSA, Fingerprint: "SHA256:bbb", HasPrivate: false},
		},
		pub: "ssh-ed25519 AAAA me@x",
	}
}

func TestKeyManagerListsKeysWithTypeAndFingerprint(t *testing.T) {
	m := NewKeyManager(twoKeys().deps())
	v := m.View(70, 20)
	for _, want := range []string{"Keys", "id_ed25519", "ed25519", "SHA256:aaa", "old", "rsa"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "no private") {
		t.Errorf("a key without a private half should be flagged:\n%s", v)
	}
}

func TestKeyManagerEmptyStateExplainsGenerate(t *testing.T) {
	m := NewKeyManager((&fakeKeys{}).deps())
	if !strings.Contains(m.View(70, 20), "n") || !strings.Contains(m.View(70, 20), "No keys") {
		t.Errorf("empty state should say No keys and mention n:\n%s", m.View(70, 20))
	}
}

func TestKeyManagerNavigationClampsAndClose(t *testing.T) {
	m := NewKeyManager(twoKeys().deps())
	m, _ = m.Update(runes("j"))
	m, _ = m.Update(runes("j"))
	m, _ = m.Update(runes("j"))
	if m.cursor != 1 {
		t.Errorf("cursor must clamp at 1, got %d", m.cursor)
	}
	_, cmd := m.Update(special(tea.KeyEsc))
	if _, ok := msgOf(cmd).(KeyManagerCloseMsg); !ok {
		t.Errorf("esc should close the key manager")
	}
}

func TestKeyManagerCopyPutsPublicKeyOnClipboard(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("c"))
	if f.copied != "ssh-ed25519 AAAA me@x" {
		t.Errorf("clipboard = %q", f.copied)
	}
	if !strings.Contains(m.View(70, 20), "Copied") {
		t.Errorf("should confirm the copy:\n%s", m.View(70, 20))
	}
}

func TestKeyManagerCopyFailureIsShownNotSwallowed(t *testing.T) {
	f := twoKeys()
	f.copyErr = errors.New("clipboard unavailable")
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("c"))
	if !strings.Contains(m.View(70, 20), "clipboard unavailable") {
		t.Errorf("copy error must be visible:\n%s", m.View(70, 20))
	}
}

func TestKeyManagerPublicKeyToggleShowsFullLine(t *testing.T) {
	m := NewKeyManager(twoKeys().deps())
	m, _ = m.Update(special(tea.KeyEnter))
	if !strings.Contains(m.View(70, 20), "ssh-ed25519 AAAA me@x") {
		t.Errorf("enter should reveal the public key:\n%s", m.View(70, 20))
	}
	m, _ = m.Update(special(tea.KeyEnter))
	if strings.Contains(m.View(70, 20), "ssh-ed25519 AAAA me@x") {
		t.Errorf("enter again should hide it")
	}
}

func TestKeyManagerGenerateFlowEd25519Default(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	if !m.Generating() {
		t.Fatal("n should open the generate form")
	}
	for _, r := range "id_elyms" {
		m, _ = m.Update(runes(string(r)))
	}
	m = runGenerate(m)
	if len(f.generated) != 1 || f.generated[0] != "id_elyms|ed25519|" {
		t.Fatalf("generated = %v", f.generated)
	}
	if m.Generating() {
		t.Errorf("form should close after success")
	}
	if !strings.Contains(m.View(70, 20), "id_elyms") {
		t.Errorf("new key should appear in the list")
	}
}

func TestKeyManagerGenerateRSAViaTypeToggleAndComment(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	for _, r := range "k2" {
		m, _ = m.Update(runes(string(r)))
	}
	m, _ = m.Update(special(tea.KeyTab))
	m, _ = m.Update(special(tea.KeyRight))
	m, _ = m.Update(special(tea.KeyTab))
	for _, r := range "bob@laptop" {
		m, _ = m.Update(runes(string(r)))
	}
	m = runGenerate(m)
	if len(f.generated) != 1 || f.generated[0] != "k2|rsa|bob@laptop" {
		t.Fatalf("generated = %v", f.generated)
	}
}

func TestKeyManagerGenerateErrorKeepsFormOpenAndShowsError(t *testing.T) {
	f := twoKeys()
	f.genErr = keygen.ErrKeyExists
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	for _, r := range "id_ed25519" {
		m, _ = m.Update(runes(string(r)))
	}
	m = runGenerate(m)
	if !m.Generating() {
		t.Errorf("form must stay open on error")
	}
	if !strings.Contains(m.View(70, 20), "already exists") {
		t.Errorf("error must be shown:\n%s", m.View(70, 20))
	}
}

func TestKeyManagerGenerateEmptyNameIsRejectedBeforeCalling(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	m, _ = m.Update(special(tea.KeyEnter))
	if len(f.generated) != 0 || !m.Generating() {
		t.Errorf("empty name must not call Generate")
	}
}

func TestKeyManagerEscInGenerateFormReturnsToListNotClose(t *testing.T) {
	m := NewKeyManager(twoKeys().deps())
	m, _ = m.Update(runes("n"))
	m, cmd := m.Update(special(tea.KeyEsc))
	if m.Generating() || cmd != nil {
		t.Errorf("esc in form should only close the form")
	}
}

func TestKeyManagerNewKeySelectedAfterGenerate(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	for _, r := range "zzz" {
		m, _ = m.Update(runes(string(r)))
	}
	m = runGenerate(m)
	if k := m.Selected(); k == nil || k.Name != "zzz" {
		t.Errorf("new key should be selected, got %+v", k)
	}
}

func TestKeyManagerSelectedPathForHostForm(t *testing.T) {
	m := NewKeyManager(twoKeys().deps())
	if m.Selected().Path != `C:\k\id_ed25519` {
		t.Errorf("Selected wrong")
	}
	empty := NewKeyManager((&fakeKeys{}).deps())
	if empty.Selected() != nil {
		t.Errorf("empty manager has no selection")
	}
}

// runGenerate presses Enter in the generate form and delivers the result of
// the background command, as Bubble Tea would.
func runGenerate(m KeyManager) KeyManager {
	m, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		return m
	}
	m, _ = m.Update(cmd())
	return m
}

func TestKeyManagerGenerateRunsInBackgroundAndShowsProgress(t *testing.T) {
	f := twoKeys()
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	for _, r := range "slow" {
		m, _ = m.Update(runes(string(r)))
	}
	m, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("generation must be returned as a command, not run inside Update")
	}
	if len(f.generated) != 0 {
		t.Fatalf("Update must not generate synchronously")
	}
	if !strings.Contains(m.View(70, 20), "Generating") {
		t.Errorf("progress should be visible:\n%s", m.View(70, 20))
	}
	m, c2 := m.Update(special(tea.KeyEsc))
	if !m.Generating() || c2 != nil {
		t.Errorf("keys must be ignored while a key is being generated")
	}
	m, _ = m.Update(cmd())
	if m.Generating() || len(f.generated) != 1 {
		t.Errorf("result should close the form and have generated once")
	}
}

func TestKeyManagerViewHonorsHeightWithManyKeysAndShowsSelection(t *testing.T) {
	f := &fakeKeys{pub: strings.Repeat("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI", 12)}
	for i := 0; i < 30; i++ {
		f.list = append(f.list, &keygen.KeyInfo{Name: fmt.Sprintf("key%02d", i), Type: keygen.TypeEd25519, Fingerprint: "SHA256:x", Comment: "c", HasPrivate: true})
	}
	m := NewKeyManager(f.deps())
	for i := 0; i < 25; i++ {
		m, _ = m.Update(runes("j"))
	}
	for _, withPub := range []bool{false, true} {
		if withPub {
			m, _ = m.Update(special(tea.KeyEnter))
			m, _ = m.Update(runes("c"))
		}
		v := m.View(46, 16)
		if got := lines(v); got > 16 {
			t.Errorf("withPub=%v: modal must fit 16 rows, got %d:\n%s", withPub, got, v)
		}
		if !strings.Contains(v, "Keys") || !strings.Contains(v, "key25") {
			t.Errorf("withPub=%v: title and selected key must be visible:\n%s", withPub, v)
		}
		for _, ln := range strings.Split(v, "\n") {
			if w := lipglossWidth(ln); w > 48 {
				t.Errorf("withPub=%v: row too wide: %d", withPub, w)
			}
		}
	}
}

func TestKeyManagerGenerateFormFitsMinimumSizeEvenWithError(t *testing.T) {
	f := twoKeys()
	f.genErr = errors.New("something went wrong while generating the key, please try again later")
	m := NewKeyManager(f.deps())
	m, _ = m.Update(runes("n"))
	m, _ = m.Update(runes("x"))
	m = runGenerate(m)
	if got := lines(m.View(46, 16)); got > 16 {
		t.Errorf("generate form must fit 16 rows, got %d:\n%s", got, m.View(46, 16))
	}
}