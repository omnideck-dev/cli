package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Palette ───────────────────────────────────────────────────────────────────
//
// Fixed hex values from the design spec.  These are intentionally not adaptive
// so that the rendered colours are consistent regardless of terminal theme.

var (
	ColorPrimary = lipgloss.Color("#7C3AED") // violet  — buttons, borders, headings
	ColorSuccess = lipgloss.Color("#10B981") // emerald — pass / running
	ColorError   = lipgloss.Color("#EF4444") // red     — fail / stopped
	ColorMuted   = lipgloss.Color("#6B7280") // grey    — secondary text, separators
	ColorWarning = lipgloss.Color("#F59E0B") // amber   — warnings (not in spec, kept)
	ColorWhite   = lipgloss.Color("#FFFFFF")
)

// ── Text styles ───────────────────────────────────────────────────────────────

var (
	// Title — bold primary-colour heading.
	Title = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)

	// Subtitle — secondary heading in muted colour.
	Subtitle = lipgloss.NewStyle().Foreground(ColorMuted)

	// Dim — muted secondary / completed text.
	Dim = lipgloss.NewStyle().Foreground(ColorMuted)

	// Success — green pass / running text.
	Success = lipgloss.NewStyle().Foreground(ColorSuccess)

	// Error — red fail / stopped text.
	Error = lipgloss.NewStyle().Foreground(ColorError)

	// Warning — amber advisory text.
	Warning = lipgloss.NewStyle().Foreground(ColorWarning)

	// Accent — primary-colour text (non-bold).
	Accent = lipgloss.NewStyle().Foreground(ColorPrimary)

	// Active — bold bright text for the focused element.
	Active = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite)

	// Selected — primary background with white text (picker rows, menu items).
	Selected = lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorWhite).
			Bold(true)
)

// ── Symbols ───────────────────────────────────────────────────────────────────

var (
	CheckMark = Success.Render("✓")
	CrossMark = Error.Render("✗")
	WarnMark  = Warning.Render("!")
	Bullet    = lipgloss.NewStyle().Render("●")
)

// GreenBullet returns a green ● for running state.
func GreenBullet() string { return Success.Render("●") }

// RedBullet returns a red ● for stopped state.
func RedBullet() string { return Error.Render("●") }

// ── Container / panel styles ──────────────────────────────────────────────────

var (
	// Panel — rounded-border box for summary and status panels.
	// Spec: RoundedBorder, primary border colour, 1v / 2h padding.
	Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2)

	// Border is an alias for Panel kept for compatibility with existing call sites.
	Border = Panel

	// Container — outer padding applied to full-screen views.
	// Spec: 1 vertical, 2 horizontal on all containers.
	Container = lipgloss.NewStyle().Padding(1, 2)

	// HeaderBar — full-width header with primary background.
	// Spec: bold + primary colour, full-width.
	// Callers should chain .Width(windowWidth) for true full-width rendering.
	HeaderBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(ColorPrimary).
			Padding(0, 2)
)

// ── Table / status styles ─────────────────────────────────────────────────────

var (
	// StatusLabel — fixed-width muted label column in status / info tables.
	StatusLabel = lipgloss.NewStyle().Width(16).Foreground(ColorMuted)

	// StatusValue — plain value column beside a StatusLabel.
	StatusValue = lipgloss.NewStyle()
)

// ── Separator ─────────────────────────────────────────────────────────────────

// Separator renders a horizontal rule n characters wide in the muted colour.
// Pass 0 to use the default 42-character width.
func Separator(n int) string {
	if n <= 0 {
		n = 42
	}
	return Dim.Render(strings.Repeat("─", n))
}

// ── Header ────────────────────────────────────────────────────────────────────

// Header renders the standard two-part page header used across all views:
//
//	  ⚡ TITLE  ·  phase
//	  ──────────────────
//
// Pass windowWidth > 0 to render a full-width HeaderBar background.
// When windowWidth == 0 the header renders without a background fill.
func Header(title, phase string, windowWidth int) string {
	left := "  ⚡ " + title
	right := phase

	if windowWidth > 0 {
		// Full-width background bar.
		gap := strings.Repeat(" ", max(1, windowWidth-lipgloss.Width(left)-lipgloss.Width(right)-4))
		bar := HeaderBar.Width(windowWidth).Render(left + gap + right)
		return bar + "\n"
	}

	// Fallback: coloured text + separator line.
	line := Title.Render(left) + Dim.Render("  ·  "+right)
	return line + "\n  " + Separator(50) + "\n"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Misc ──────────────────────────────────────────────────────────────────────

// NoColor disables lipgloss colour rendering (e.g. for CI / pipe output).
func NoColor(disabled bool) {
	if disabled {
		lipgloss.SetColorProfile(0)
	}
}
