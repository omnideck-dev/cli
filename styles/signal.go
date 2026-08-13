package styles

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SIGNAL is Omnideck's product design language. These dark-theme values mirror
// server/ui/src/global.css in the main Omnideck repository. Keep this list in
// token order so changes can be compared directly with the canonical CSS.
var (
	SignalCanvas        = lipgloss.Color("#0c0e14")
	SignalSurface       = lipgloss.Color("#151821")
	SignalElevated      = lipgloss.Color("#1e2130")
	SignalTextPrimary   = lipgloss.Color("#e8ecf4")
	SignalTextSecondary = lipgloss.Color("#8892a8")
	SignalTextTertiary  = lipgloss.Color("#4a5168")
	SignalBorder        = lipgloss.Color("#252a3a")
	SignalBorderSubtle  = lipgloss.Color("#1a1e2a")
	SignalBorderStrong  = lipgloss.Color("#363d52")
	SignalAccent        = lipgloss.Color("#3b82f6")
	SignalAccentHover   = lipgloss.Color("#60a5fa")
	SignalSuccess       = lipgloss.Color("#4ade80")
	SignalWarning       = lipgloss.Color("#fbbf24")
	SignalDanger        = lipgloss.Color("#f87171")

	SignalTerminalBackground = lipgloss.Color("#111318")
	SignalTerminalSurface    = lipgloss.Color("#1a1d26")
	SignalTerminalText       = lipgloss.Color("#c8cdd8")
	SignalTerminalPrompt     = lipgloss.Color("#4ade80")
	SignalTerminalError      = lipgloss.Color("#ff7043")
	SignalTerminalComment    = lipgloss.Color("#4a5168")

	// Solid terminal approximations of the app's 12%-opacity status surfaces,
	// composited over --elevated.
	SignalAccentMuted  = lipgloss.Color("#212d48")
	SignalSuccessMuted = lipgloss.Color("#23383a")
	SignalWarningMuted = lipgloss.Color("#39342f")
	SignalDangerMuted  = lipgloss.Color("#382b38")

	// The three blues in the desktop icon, left to right.
	SignalLogoDeep = lipgloss.Color("#2563eb")
)

// Pre-built styles for the dark interactive terminal application. Supporting
// copy deliberately uses the accessible secondary token. The lower-contrast
// tertiary token is reserved for borders and non-text decoration.
var (
	TUIPrimaryText     = lipgloss.NewStyle().Foreground(SignalTextPrimary)
	TUIPrimaryBold     = lipgloss.NewStyle().Foreground(SignalTextPrimary).Bold(true)
	TUISecondaryText   = lipgloss.NewStyle().Foreground(SignalTextSecondary)
	TUISubtleText      = lipgloss.NewStyle().Foreground(SignalTextSecondary)
	TUIDecorative      = lipgloss.NewStyle().Foreground(SignalTextTertiary)
	TUIAccentText      = lipgloss.NewStyle().Foreground(SignalAccent)
	TUIAccentHoverText = lipgloss.NewStyle().Foreground(SignalAccentHover)
	TUISuccessText     = lipgloss.NewStyle().Foreground(SignalSuccess)
	TUIWarningText     = lipgloss.NewStyle().Foreground(SignalWarning)
	TUIDangerText      = lipgloss.NewStyle().Foreground(SignalDanger)

	TUIBoldAccent  = lipgloss.NewStyle().Foreground(SignalAccent).Bold(true)
	TUIBoldSuccess = lipgloss.NewStyle().Foreground(SignalSuccess).Bold(true)
	TUIBoldWarning = lipgloss.NewStyle().Foreground(SignalWarning).Bold(true)
	TUIBoldDanger  = lipgloss.NewStyle().Foreground(SignalDanger).Bold(true)

	TUIKeyChip = lipgloss.NewStyle().
			Background(SignalElevated).
			Foreground(SignalTextPrimary).
			Padding(0, 1)

	TUISelectedRow = lipgloss.NewStyle().Background(SignalElevated)

	// Backgrounds are applied with RenderOnBackground at composition time.
	// Rendering an ANSI-styled child inside a Lip Gloss background style allows
	// the child's SGR reset to expose the terminal's default background.
	TUIPanel = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(SignalBorderStrong)

	TUIPanelAccent = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(SignalAccent)

	TUIHeaderBar = lipgloss.NewStyle().
			Foreground(SignalTextPrimary).
			Padding(0, 1)

	TUIFooterBar = lipgloss.NewStyle().
			Foreground(SignalTextSecondary).
			Padding(0, 1)

	TUITableHeader = lipgloss.NewStyle().Foreground(SignalTextSecondary)
)

// BrandMark renders a terminal-safe version of the desktop icon's three
// descending blue bars. Each segment owns its background, so it is safe on
// terminals whose default background is light.
func BrandMark(background lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(SignalLogoDeep).Background(background).Render("█") +
		lipgloss.NewStyle().Foreground(SignalAccent).Background(background).Render("▆") +
		lipgloss.NewStyle().Foreground(SignalAccentHover).Background(background).Render("▄")
}

// RenderOnBackground composes already-styled ANSI content onto a fixed
// background. Lip Gloss/termenv child styles end with SGR 0, which also clears
// a parent background. Reapplying the background after every reset prevents
// light terminal defaults from leaking through headers, footers, cards, and
// full-screen padding.
func RenderOnBackground(content string, background lipgloss.Color) string {
	prefix := backgroundPrefix(background)
	if prefix == "" {
		return content
	}
	const reset = "\x1b[0m"
	content = strings.ReplaceAll(content, reset, reset+prefix)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = prefix + line + reset
	}
	return strings.Join(lines, "\n")
}

func backgroundPrefix(background lipgloss.Color) string {
	const marker = "__omnideck_background_marker__"
	rendered := lipgloss.NewStyle().Background(background).Render(marker)
	index := strings.Index(rendered, marker)
	if index <= 0 {
		return ""
	}
	return rendered[:index]
}

// TUIStatusColor returns the SIGNAL color for a container status string.
func TUIStatusColor(status string) lipgloss.Color {
	switch status {
	case "running":
		return SignalSuccess
	case "paused":
		return SignalWarning
	default:
		return SignalDanger
	}
}

func TUIStatusDot(status string) string {
	return lipgloss.NewStyle().Foreground(TUIStatusColor(status)).Render("●")
}

func TUILogLevel(level string) string {
	switch level {
	case "ERROR":
		return TUIBoldDanger.Render(padWidth(level, 5))
	case "WARN":
		return TUIBoldWarning.Render(padWidth(level, 5))
	case "INFO":
		return TUIBoldAccent.Render(padWidth(level, 5))
	case "READY":
		return TUIBoldSuccess.Render(padWidth(level, 5))
	case "DEBUG":
		return TUISecondaryText.Bold(true).Render(padWidth(level, 5))
	default:
		return TUISubtleText.Render(padWidth(level, 5))
	}
}

func TUIBar(pct float64, width int, color lipgloss.Color) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	if width <= 0 {
		return ""
	}
	filled := int(pct * float64(width))
	if filled == 0 && pct > 0 {
		filled = 1
	}
	empty := width - filled
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(SignalBorder).Render(strings.Repeat("░", empty))
}

func TUIGradientBar(pct float64, width int, startColor, endColor lipgloss.Color) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	if width <= 0 {
		return ""
	}
	filled := int(pct * float64(width))
	if filled == 0 && pct > 0 {
		filled = 1
	}
	empty := width - filled

	sr, sg, sb := hexToRGB(string(startColor))
	er, eg, eb := hexToRGB(string(endColor))
	var out strings.Builder
	for i := range filled {
		t := 0.0
		if filled > 1 {
			t = float64(i) / float64(filled-1)
		}
		r := sr + int(float64(er-sr)*t)
		g := sg + int(float64(eg-sg)*t)
		b := sb + int(float64(eb-sb)*t)
		color := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
		out.WriteString(lipgloss.NewStyle().Foreground(color).Render("█"))
	}
	out.WriteString(lipgloss.NewStyle().Foreground(SignalBorder).Render(strings.Repeat("░", empty)))
	return out.String()
}

func hexToRGB(value string) (int, int, int) {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return 128, 128, 128
	}
	var r, g, b int
	if _, err := fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b); err != nil {
		return 128, 128, 128
	}
	return r, g, b
}

func padWidth(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}
