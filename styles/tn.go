package styles

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tokyo Night palette — used exclusively by the v2 dashboard TUI.
// These are intentional fixed hex values matching the design spec.
var (
	TNBg     = lipgloss.Color("#16161e")
	TNBgAlt  = lipgloss.Color("#191b26")
	TNBgSel  = lipgloss.Color("#23263a")
	TNBorder = lipgloss.Color("#242838")
	TNBorder2 = lipgloss.Color("#2a2e44")
	TNFg     = lipgloss.Color("#d6dcf5")
	TNFgMid  = lipgloss.Color("#a9b1d6")
	TNFgSub  = lipgloss.Color("#c0caf5")
	TNMuted  = lipgloss.Color("#565f89")
	TNDimClr = lipgloss.Color("#4d5573")
	TNFaint  = lipgloss.Color("#414868")
	TNBlue   = lipgloss.Color("#7aa2f7")
	TNPurple = lipgloss.Color("#bb9af7")
	TNGreen  = lipgloss.Color("#9ece6a")
	TNYellow = lipgloss.Color("#e0af68")
	TNRed    = lipgloss.Color("#f7768e")
	TNCyan   = lipgloss.Color("#7dcfff")
	TNOrange = lipgloss.Color("#ff9e64")
)

// Pre-built Lip Gloss styles for the dashboard.
var (
	TNText       = lipgloss.NewStyle().Foreground(TNFg)
	TNTextBold   = lipgloss.NewStyle().Foreground(TNFg).Bold(true)
	TNTextMid    = lipgloss.NewStyle().Foreground(TNFgMid)
	TNTextSub    = lipgloss.NewStyle().Foreground(TNFgSub)
	TNDimText    = lipgloss.NewStyle().Foreground(TNMuted)
	TNFaintText  = lipgloss.NewStyle().Foreground(TNFaint)
	TNBlueTxt    = lipgloss.NewStyle().Foreground(TNBlue)
	TNGreenTxt   = lipgloss.NewStyle().Foreground(TNGreen)
	TNRedTxt     = lipgloss.NewStyle().Foreground(TNRed)
	TNYellowTxt  = lipgloss.NewStyle().Foreground(TNYellow)
	TNCyanTxt    = lipgloss.NewStyle().Foreground(TNCyan)
	TNPurpleTxt  = lipgloss.NewStyle().Foreground(TNPurple)

	TNBoldBlue   = lipgloss.NewStyle().Foreground(TNBlue).Bold(true)
	TNBoldGreen  = lipgloss.NewStyle().Foreground(TNGreen).Bold(true)
	TNBoldRed    = lipgloss.NewStyle().Foreground(TNRed).Bold(true)
	TNBoldYellow = lipgloss.NewStyle().Foreground(TNYellow).Bold(true)
	TNBoldCyan   = lipgloss.NewStyle().Foreground(TNCyan).Bold(true)

	// TNKeyChip renders a keyboard key hint (e.g. "j").
	TNKeyChip = lipgloss.NewStyle().
			Background(lipgloss.Color("#262a3c")).
			Foreground(lipgloss.Color("#c0caf5")).
			Padding(0, 1)

	// TNSelRow is the background for a selected table row.
	TNSelRow = lipgloss.NewStyle().Background(TNBgSel)

	// TNPanel is a bordered card panel.
	TNPanel = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#3b4076")).
		Background(TNBgAlt)

	// TNPanelAccent is the center/focus panel with a blue accent border (simulated glow).
	TNPanelAccent = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(TNBlue).
		Background(TNBgAlt)

	// TNModal is a rounded-border modal overlay panel.
	TNModal = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(TNBorder2).
		Background(TNBgAlt)

	// TNHeaderBar is the single-line app header bar.
	TNHeaderBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1c25")).
			Foreground(TNFg).
			Padding(0, 1)

	// TNFooterBar is the single-line footer key-hint bar.
	TNFooterBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#14151c")).
			Foreground(TNMuted).
			Padding(0, 1)

	// TNTableHeader is the uppercase column label row.
	TNTableHeader = lipgloss.NewStyle().
			Foreground(TNDimClr).
			Background(TNBgAlt)
)

// TNStatusColor returns the Tokyo Night color for a container status string.
func TNStatusColor(status string) lipgloss.Color {
	switch status {
	case "running":
		return TNGreen
	case "paused":
		return TNYellow
	default:
		return TNRed
	}
}

// TNStatusDot returns a colored "●" for the given container status.
func TNStatusDot(status string) string {
	return lipgloss.NewStyle().Foreground(TNStatusColor(status)).Render("●")
}

// TNLogLevel renders a log level token in its color.
func TNLogLevel(level string) string {
	switch level {
	case "ERROR":
		return TNBoldRed.Render(padWidth(level, 5))
	case "WARN":
		return TNBoldYellow.Render(padWidth(level, 5))
	case "INFO":
		return TNBoldBlue.Render(padWidth(level, 5))
	case "READY":
		return TNBoldGreen.Render(padWidth(level, 5))
	case "DEBUG":
		return TNFaintText.Bold(true).Render(padWidth(level, 5))
	default:
		return TNDimText.Render(padWidth(level, 5))
	}
}

// TNBar renders a solid-color percentage bar (kept for compatibility).
func TNBar(pct float64, width int, color lipgloss.Color) string {
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
	s := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled))
	s += lipgloss.NewStyle().Foreground(TNBorder).Render(strings.Repeat("░", empty))
	return s
}

// TNGradientBar renders a horizontal bar with a left-to-right color gradient
// from startColor to endColor over the filled portion, then dim empty blocks.
func TNGradientBar(pct float64, width int, startColor, endColor lipgloss.Color) string {
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
		col := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
		out.WriteString(lipgloss.NewStyle().Foreground(col).Render("█"))
	}
	out.WriteString(lipgloss.NewStyle().Foreground(TNBorder).Render(strings.Repeat("░", empty)))
	return out.String()
}

// hexToRGB parses a "#rrggbb" hex color into its components.
func hexToRGB(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 128, 128, 128
	}
	var r, g, b int
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// padWidth pads a string to exactly n chars (no ANSI awareness needed for fixed tokens).
func padWidth(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
