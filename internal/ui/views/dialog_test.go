package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDefaultsToNoOnEnter(t *testing.T) {
	d := NewConfirm("Delete host web?", "web")
	_, cmd := d.Update(special(tea.KeyEnter))
	got, ok := msgOf(cmd).(ConfirmResultMsg)
	if !ok || got.Confirmed {
		t.Errorf("enter must not confirm a destructive action: %+v", got)
	}
}

func TestConfirmYConfirmsAndCarriesPayload(t *testing.T) {
	d := NewConfirm("Delete host web?", "web")
	_, cmd := d.Update(runes("y"))
	got, ok := msgOf(cmd).(ConfirmResultMsg)
	if !ok || !got.Confirmed || got.Payload != "web" {
		t.Errorf("y should confirm with payload: %+v", got)
	}
}

func TestConfirmNAndEscCancel(t *testing.T) {
	for _, k := range []tea.KeyMsg{runes("n"), special(tea.KeyEsc), runes("N")} {
		d := NewConfirm("q", "p")
		_, cmd := d.Update(k)
		got, ok := msgOf(cmd).(ConfirmResultMsg)
		if !ok || got.Confirmed {
			t.Errorf("%v should cancel: %+v", k, got)
		}
	}
}

func TestConfirmOtherKeysDoNothing(t *testing.T) {
	d := NewConfirm("q", "p")
	_, cmd := d.Update(runes("x"))
	if cmd != nil {
		t.Errorf("unrelated key must not resolve the dialog")
	}
}

func TestConfirmViewShowsPromptAndChoices(t *testing.T) {
	v := NewConfirm("Delete host web?", "web").View(50)
	for _, want := range []string{"Delete host web?", "y", "n"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
}
