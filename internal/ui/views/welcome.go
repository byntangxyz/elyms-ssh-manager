package views

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"elyms/internal/ui/styles"
)

// Welcome renders the right pane when no host is selected: what elyms is, who
// wrote it, and the handful of keys worth memorising. It is an empty-selection
// state, not a modal, so it disappears as soon as a host is selected.
//
// width and height are hard bounds. Lines are clipped and the block is padded
// to height, so the pane never overflows at the minimum terminal size.
func Welcome(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	clip := func(s string) string { return ansi.Truncate(s, width, "…") }

	lines := []string{
		styles.Title.Render(clip("elyms  TUI SSH manager")),
		"",
		styles.Body.Render(ansi.Hardwrap(
			"Manage OpenSSH hosts and keys locally, from one screen.", width, true)),
		"",
		styles.Subtle.Render(clip("by byntangxyz")),
		"",
		styles.Badge.Render(clip("Getting started")),
		clip(styles.Hint("a", "add a host")),
		clip(styles.Hint("j/k", "move the selection")),
		clip(styles.Hint("enter", "connect to the host")),
		clip(styles.Hint("k", "manage keys")),
		"",
		styles.Subtle.Render(ansi.Hardwrap(
			"Connection settings stay in your standard .ssh/config; elyms only edits that file.",
			width, true)),
	}

	out := make([]string, 0, height)
	for _, ln := range lines {
		for _, part := range strings.Split(ln, "\n") {
			if len(out) == height {
				return strings.Join(out, "\n")
			}
			out = append(out, part)
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
