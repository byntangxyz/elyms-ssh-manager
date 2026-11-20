package views

import (
	"strings"
	"testing"
)

func TestWelcomeExplainsElymsAndCreditsAuthor(t *testing.T) {
	v := Welcome(60, 20)
	for _, want := range []string{
		"elyms",
		"TUI SSH manager",
		"byntangxyz",
		".ssh/config",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("welcome missing %q:\n%s", want, v)
		}
	}
}

func TestWelcomeListsGettingStartedKeys(t *testing.T) {
	v := Welcome(60, 20)
	for _, want := range []string{
		"Getting started",
		"add",
		"connect",
		"keys",
		"j/k",
	} {
		if !strings.Contains(strings.ToLower(v), strings.ToLower(want)) {
			t.Errorf("welcome missing %q:\n%s", want, v)
		}
	}
}

// TestWelcomeFitsNarrowPane keeps every line inside the pane at 50 columns,
// which is the narrowest supported terminal.
func TestWelcomeFitsNarrowPane(t *testing.T) {
	v := Welcome(50, 12)
	for _, ln := range strings.Split(v, "\n") {
		if w := lipglossWidth(ln); w > 50 {
			t.Errorf("line wider than 50 (%d): %q", w, ln)
		}
	}
	if n := lines(v); n > 12 {
		t.Errorf("welcome used %d lines, pane has 12:\n%s", n, v)
	}
}

// TestWelcomeDegradesGracefully proves a very short pane is clipped instead of
// panicking or overflowing.
func TestWelcomeDegradesGracefully(t *testing.T) {
	for _, size := range [][2]int{{20, 3}, {10, 1}, {0, 0}} {
		v := Welcome(size[0], size[1])
		if size[1] > 0 && lines(v) > size[1] {
			t.Errorf("height %d produced %d lines", size[1], lines(v))
		}
		for _, ln := range strings.Split(v, "\n") {
			if size[0] > 0 && lipglossWidth(ln) > size[0] {
				t.Errorf("width %d produced a %d-wide line", size[0], lipglossWidth(ln))
			}
		}
	}
}
