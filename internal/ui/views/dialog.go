package views

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"elyms/internal/ui/styles"
)

type ConfirmResultMsg struct {
	Confirmed bool
	Payload   string
}

// Confirm is a yes/no dialog. Only an explicit "y" confirms; Enter does not.
type Confirm struct {
	prompt  string
	payload string
}

func NewConfirm(prompt, payload string) Confirm {
	return Confirm{prompt: prompt, payload: payload}
}

func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	result := func(yes bool) tea.Cmd {
		return func() tea.Msg { return ConfirmResultMsg{Confirmed: yes, Payload: c.payload} }
	}
	switch k.String() {
	case "y", "Y":
		return c, result(true)
	case "n", "N", "esc", "enter":
		return c, result(false)
	}
	return c, nil
}

func (c Confirm) View(width int) string {
	dialog := width
	if dialog < 14 {
		dialog = 14
	}
	inner := dialog - 6
	body := ansi.Truncate(styles.Title.Render("Confirm"), inner, "…") + "\n\n" +
		styles.Body.Render(ansi.Hardwrap(c.prompt, inner, true)) + "\n\n" +
		ansi.Truncate(styles.Hint("y", "yes")+"   "+styles.Hint("n", "no (default)"), inner, "…")
	return styles.Modal(dialog).Render(body)
}
