package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestNoColorUsesPlainTextProfile(t *testing.T) {
	previous := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	NoColor(true)
	if got := Title.Render("Omnideck"); strings.Contains(got, "\x1b[") {
		t.Fatalf("Title.Render() contains an ANSI escape with color disabled: %q", got)
	}
}
