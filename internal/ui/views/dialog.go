package views

import (
	tea "github.com/charmbracelet/bubbletea"

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
	body := styles.Title.Render("Confirm") + "\n\n" +
		styles.Body.Render(c.prompt) + "\n\n" +
		styles.Hint("y", "yes") + "   " + styles.Hint("n", "no (default)")
	return styles.Modal(width).Render(body)
}
