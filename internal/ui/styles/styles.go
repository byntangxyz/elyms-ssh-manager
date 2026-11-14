package styles

import "github.com/charmbracelet/lipgloss"

var (
	Accent    = lipgloss.AdaptiveColor{Light: "#0F6E8C", Dark: "#7DCFFF"}
	Secondary = lipgloss.AdaptiveColor{Light: "#6E40C9", Dark: "#BB9AF7"}
	Text      = lipgloss.AdaptiveColor{Light: "#1F2335", Dark: "#C0CAF5"}
	Muted     = lipgloss.AdaptiveColor{Light: "#5A607D", Dark: "#8A92B5"}
	Border    = lipgloss.AdaptiveColor{Light: "#A0A6C0", Dark: "#3B4261"}
	Danger    = lipgloss.AdaptiveColor{Light: "#B3243E", Dark: "#F7768E"}
	Success   = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#9ECE6A"}
	Selection = lipgloss.AdaptiveColor{Light: "#D6E4FF", Dark: "#283457"}
)

var (
	Title      = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	Subtle     = lipgloss.NewStyle().Foreground(Muted)
	Body       = lipgloss.NewStyle().Foreground(Text)
	Label      = lipgloss.NewStyle().Foreground(Muted)
	Value      = lipgloss.NewStyle().Foreground(Text)
	ErrorText  = lipgloss.NewStyle().Foreground(Danger)
	OKText     = lipgloss.NewStyle().Foreground(Success)
	Badge      = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
	KeyHint    = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	HintText   = lipgloss.NewStyle().Foreground(Muted)
	RowActive  = lipgloss.NewStyle().Foreground(Text).Background(Selection).Bold(true)
	RowNormal  = lipgloss.NewStyle().Foreground(Text)
	RowMuted   = lipgloss.NewStyle().Foreground(Muted)
	FieldLabel = lipgloss.NewStyle().Foreground(Muted).Width(14)
	FieldFocus = lipgloss.NewStyle().Foreground(Accent).Bold(true).Width(14)
)

func Panel(focused bool) lipgloss.Style {
	c := Border
	if focused {
		c = Accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Padding(0, 1)
}

func Modal(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent).
		Padding(1, 2).
		Width(width)
}

// Hint renders "key description" pairs for the status bar.
func Hint(key, desc string) string {
	return KeyHint.Render(key) + " " + HintText.Render(desc)
}
