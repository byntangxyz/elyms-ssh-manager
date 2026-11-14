package views

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func special(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func ctrl(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func lines(s string) int { return len(strings.Split(s, "\n")) }

func msgOf(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
