package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// --- Dashboard screen (card layout) ---

func (m AppModel) viewDashboard() string {
	h := m.contentHeight()
	w := m.width

	headerLines := []string{""}

	// Title row with status chips right-aligned.
	title := styles.TUIPrimaryBold.Render("Decks")
	sub := styles.TUISubtleText.Render(" managed by this host")
	titleLeft := title + sub

	counts := map[string]int{}
	for _, inst := range m.instances {
		counts[inst.Status]++
	}
	var chipParts []string
	for _, s := range []string{"running", "paused", "stopped"} {
		if n := counts[s]; n > 0 {
			chip := lipgloss.NewStyle().
				Background(styles.SignalElevated).
				Foreground(styles.TUIStatusColor(s)).
				Padding(0, 1).
				Render(fmt.Sprintf("● %d %s", n, s))
			chipParts = append(chipParts, chip)
		}
	}
	chipsStr := strings.Join(chipParts, "  ")
	titleGap := w - lipgloss.Width(titleLeft) - lipgloss.Width(chipsStr) - 4
	if titleGap < 1 {
		titleGap = 1
	}
	headerLines = append(headerLines, "  "+titleLeft+safeRepeat(" ", titleGap)+chipsStr, "")

	if len(m.instances) == 0 {
		lines := append(headerLines,
			"  "+styles.TUISecondaryText.Render("No Omnideck instances are set up yet."),
			"  "+styles.TUISecondaryText.Render("Press ")+styles.TUIKeyChip.Render("n")+styles.TUISecondaryText.Render(" to set one up."),
		)
		return m.renderDashboardLines(lines, h, w)
	}

	// cardW is the Lipgloss Width() arg. Lipgloss wraps at cardW-2 (subtracts
	// left+right padding). Outer card = cardW + 2 (border). With "  " prefix
	// total line = cardW + 4. Set cardW = w - 6 so cards span w - 2 columns.
	cardW := w - 6
	if cardW < 20 {
		cardW = 20
	}

	var cardLines []string
	selectedStart, selectedEnd := 0, 0
	for i := range m.instances {
		start := len(cardLines)
		card := m.renderInstanceCard(i, cardW)
		for _, line := range strings.Split(card, "\n") {
			cardLines = append(cardLines, "  "+line)
		}
		cardLines = append(cardLines, "")
		if i == m.selected {
			selectedStart, selectedEnd = start, len(cardLines)
		}
	}

	// Toast pinned at bottom.
	if m.toast != "" {
		cardLines = append(cardLines, "  "+styles.TUIAccentText.Render("  "+m.toast))
	}

	viewportHeight := max(0, h-len(headerLines))
	baseOffset := 0
	selectedHeight := selectedEnd - selectedStart
	if selectedHeight > viewportHeight {
		baseOffset = selectedStart
	} else if selectedEnd > viewportHeight {
		baseOffset = selectedEnd - viewportHeight
	}
	offset := baseOffset + m.dashboardScroll
	maxOffset := max(0, len(cardLines)-viewportHeight)
	offset = min(max(0, offset), maxOffset)
	end := min(len(cardLines), offset+viewportHeight)
	lines := append(headerLines, cardLines[offset:end]...)
	return m.renderDashboardLines(lines, h, w)
}

// renderDashboardLines gives the root screen an exact terminal-sized canvas.
// Dashboard cards are already laid out at full width, so this path avoids the
// padding used by workflow screens and clips any overflow before rendering.
func (m AppModel) renderDashboardLines(lines []string, height, width int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	body := strings.Join(lines, "\n")
	layout := lipgloss.NewStyle().Width(max(1, width)).Height(max(1, height)).MaxWidth(max(1, width)).MaxHeight(max(1, height))
	return styles.RenderOnBackground(layout.Render(body), styles.SignalCanvas)
}

// renderInstanceCard renders one instance card. cardW is the inner content width.
func (m AppModel) renderInstanceCard(idx, cardW int) string {
	inst := &m.instances[idx]
	selected := idx == m.selected
	expanded := m.expanded[inst.Info.Name]

	// Caret indicates expand state.
	caretCh := "▸"
	if expanded {
		caretCh = "▾"
	}
	caret := styles.TUIAccentText.Render(caretCh)
	caretW := lipgloss.Width(caret)

	// Status dot + name + port badge.
	dot := styles.TUIStatusDot(inst.Status)
	name := styles.TUIPrimaryBold.Render(inst.Info.Name)
	portStr := ":" + inst.Info.Config.WebUIPortOrDefault()
	portBadge := lipgloss.NewStyle().
		Background(styles.SignalElevated).
		Foreground(styles.SignalTextSecondary).
		Padding(0, 1).Render(portStr)
	identity := caret + "  " + dot + " " + name + "  " + portBadge
	identityW := lipgloss.Width(identity)

	// Image path (line 2, below name).
	imgIndent := caretW + 5 // caret + "  " + dot + " "
	imageStr := styles.TUISecondaryText.Render(tnTruncate(inst.Info.Config.Image, identityW+6))
	imageLine := safeRepeat(" ", imgIndent) + imageStr

	// CPU sparkline block.
	cpuVal := dashOr(inst.CPU)
	cpuLabel := styles.TUISubtleText.Render("CPU") + " " + styles.TUIAccentText.Bold(true).Render(cpuVal)
	cpuSpark := renderSparkline(inst.CPUHistory, styles.SignalAccent, 16)
	cpuBlock := cpuLabel + "  " + cpuSpark

	// MEM sparkline block.
	ramVal := dashOr(inst.RAM)
	memLabel := styles.TUISubtleText.Render("MEM") + " " + styles.TUIAccentHoverText.Bold(true).Render(ramVal)
	memSpark := renderSparkline(inst.RAMHistory, styles.SignalAccentHover, 16)
	memBlock := memLabel + "  " + memSpark

	stats := cpuBlock + "   " + memBlock
	statsW := lipgloss.Width(stats)

	// Primary action chips. Destructive removal is kept out of this compact row
	// and shown separately when the card is expanded.
	chips := m.renderActionChips(inst, idx)
	chipsW := lipgloss.Width(chips)

	// contentW is the actual wrap threshold inside Width(cardW) + Padding(0,1):
	// lipgloss wraps at width - leftPad - rightPad = cardW - 2.
	contentW := cardW - 2

	// Distribute remaining space as gaps between identity, stats, chips.
	used := identityW + statsW + chipsW
	gapTotal := contentW - used
	if gapTotal < 4 {
		gapTotal = 4
	}
	gap1 := gapTotal / 3
	gap2 := gapTotal - gap1 - gap1

	line1 := identity + safeRepeat(" ", gap1) + stats + safeRepeat(" ", gap2) + chips

	content := line1 + "\n" + imageLine

	// Accordion section when expanded.
	if expanded {
		sep := styles.TUISubtleText.Render(safeRepeat("─", contentW))
		accordion := m.renderCardAccordion(inst, contentW)
		removeAction := m.renderRemoveAction(idx, contentW)
		content = content + "\n" + sep + "\n" + accordion + "\n" + sep + "\n" + removeAction
	}

	borderColor := styles.SignalBorder
	if selected {
		borderColor = styles.SignalAccent
	}

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(cardW).
		Render(content)
	return styles.RenderOnBackground(card, styles.SignalElevated)
}

// renderActionChips renders the compact, non-destructive action row for a card.
func (m AppModel) renderActionChips(inst *InstanceState, cardIdx int) string {
	isSelected := cardIdx == m.selected
	chipBase := lipgloss.NewStyle().
		Background(styles.SignalElevated).
		Foreground(styles.SignalTextSecondary).
		Padding(0, 1)
	chipFocused := lipgloss.NewStyle().
		Background(styles.SignalAccent).
		Foreground(styles.SignalCanvas).
		Bold(true).
		Padding(0, 1)

	chip := func(label string, focusIdx int) string {
		if isSelected && m.chipFocus == focusIdx {
			return chipFocused.Render(label)
		}
		return chipBase.Render(label)
	}

	// Open UI chip.
	openChip := chip("↗ Open UI", 0)

	// Logs chip.
	logsChip := chip("≣ Logs", 1)

	// Update chip.
	updateChip := chip("⚙ Update", 2)

	// Stop/Start chip — color-coded by run state.
	var toggleChip string
	if inst.Status == "running" {
		label := "■ Stop"
		if isSelected && m.chipFocus == 3 {
			toggleChip = lipgloss.NewStyle().
				Background(styles.SignalDanger).
				Foreground(styles.SignalCanvas).
				Bold(true).Padding(0, 1).Render(label)
		} else {
			toggleChip = lipgloss.NewStyle().
				Background(styles.SignalDangerMuted).
				Foreground(styles.SignalDanger).
				Padding(0, 1).Render(label)
		}
	} else {
		label := "▶ Start"
		if isSelected && m.chipFocus == 3 {
			toggleChip = lipgloss.NewStyle().
				Background(styles.SignalSuccess).
				Foreground(styles.SignalCanvas).
				Bold(true).Padding(0, 1).Render(label)
		} else {
			toggleChip = lipgloss.NewStyle().
				Background(styles.SignalSuccessMuted).
				Foreground(styles.SignalSuccess).
				Padding(0, 1).Render(label)
		}
	}

	return openChip + " " + logsChip + " " + updateChip + " " + toggleChip
}

// renderRemoveAction makes instance removal discoverable without crowding the
// card's compact action row. It is visible only in the expanded card.
func (m AppModel) renderRemoveAction(cardIdx, innerW int) string {
	label := "✕ Remove instance"
	chipStyle := lipgloss.NewStyle().
		Background(styles.SignalDangerMuted).
		Foreground(styles.SignalDanger).
		Padding(0, 1)
	if cardIdx == m.selected && m.chipFocus == 4 {
		chipStyle = lipgloss.NewStyle().
			Background(styles.SignalDanger).
			Foreground(styles.SignalCanvas).
			Bold(true).
			Padding(0, 1)
	}
	chip := chipStyle.Render(label)
	title := styles.TUISecondaryText.Render("INSTANCE ACTION")
	gap := innerW - lipgloss.Width(title) - lipgloss.Width(chip)
	if gap < 1 {
		gap = 1
	}
	return title + safeRepeat(" ", gap) + chip
}

// renderCardAccordion renders the expanded accordion (metadata + resources + log tail).
// Uses plain text columns — no nested Lipgloss panels — to avoid ANSI rendering issues
// that arise when bordered/background-filled sub-panels are placed inside a styled card.
func (m AppModel) renderCardAccordion(inst *InstanceState, innerW int) string {
	cfg := inst.Info.Config
	colW := (innerW - 4) / 2 // two columns with 4-space gap
	if colW < 12 {
		colW = 12
	}

	colSep := styles.TUISubtleText.Render(safeRepeat("─", colW))

	// --- Metadata column rows ---
	mkv := func(k, v string, vstyle lipgloss.Style) string {
		key := styles.TUISecondaryText.Render(padRight(k, 9))
		val := vstyle.Render(tnTruncate(v, colW-10))
		return key + val
	}
	metaRows := []string{
		styles.TUISecondaryText.Bold(true).Render("METADATA"),
		colSep,
		mkv("image", cfg.Image, styles.TUIAccentHoverText),
		mkv("uptime", dashOr(inst.Uptime), styles.TUIPrimaryText),
		mkv("restarts", dashOr(inst.Restarts), styles.TUIPrimaryText),
		mkv("created", dashOr(inst.Created), styles.TUIPrimaryText),
		mkv("health", dashOr(inst.Health), healthStyle(inst.Health)),
	}

	// --- Resources column rows ---
	barW := colW - 14
	if barW < 4 {
		barW = 4
	}
	cpuBar := styles.TUIGradientBar(inst.CPUPct, barW, styles.SignalLogoDeep, styles.SignalAccent)
	ramBar := styles.TUIGradientBar(inst.RAMPct, barW, styles.SignalAccent, styles.SignalAccentHover)
	resRows := []string{
		styles.TUISecondaryText.Bold(true).Render("RESOURCES"),
		colSep,
		styles.TUISecondaryText.Render(padRight("CPU", 10)) + styles.TUIAccentText.Bold(true).Render(dashOr(inst.CPU)) + "  " + styles.TUISubtleText.Render("/ 100%"),
		cpuBar,
		"",
		styles.TUISecondaryText.Render(padRight("Memory", 10)) + styles.TUIAccentHoverText.Bold(true).Render(dashOr(inst.RAM)) + "  " + styles.TUISubtleText.Render("/ "+dashOr(inst.RAMTotal)),
		ramBar,
		"",
		styles.TUISecondaryText.Render("network") + "  " + styles.TUIPrimaryText.Render("↑ "+dashOr(inst.NetUp)+"  ↓ "+dashOr(inst.NetDown)),
	}
	// Distinguish "the engine couldn't read stats for a running instance"
	// (e.g. a stale container network namespace) from "stopped, so there's
	// nothing to show" — both would otherwise render as identical dashes.
	if inst.StatsUnavailable {
		resRows = append(resRows, "", styles.TUIWarningText.Render("⚠ resources unavailable"))
	}

	// Pad columns to same height.
	nRows := max(len(metaRows), len(resRows))
	for len(metaRows) < nRows {
		metaRows = append(metaRows, "")
	}
	for len(resRows) < nRows {
		resRows = append(resRows, "")
	}

	var sb strings.Builder
	for i := 0; i < nRows; i++ {
		ml := metaRows[i]
		rl := resRows[i]
		pad := colW - lipgloss.Width(ml)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(ml + safeRepeat(" ", pad) + "    " + rl + "\n")
	}

	// Full-width separator before log tail.
	sb.WriteString(styles.TUISubtleText.Render(safeRepeat("─", innerW)) + "\n")

	// Log tail — raw text, no nested panel.
	logTitle := styles.TUISecondaryText.Render("LOGS · TAIL")
	logHint := styles.TUISubtleText.Render("open full logs →")
	logGap := innerW - lipgloss.Width(logTitle) - lipgloss.Width(logHint)
	if logGap < 1 {
		logGap = 1
	}
	sb.WriteString(logTitle + safeRepeat(" ", logGap) + logHint + "\n")

	// logPrefixW: "  "(2) + ts(19) + " "(1) + lvl(5) + "  "(2) = 29
	const logPrefixW = 29
	msgAreaW := innerW - logPrefixW
	if msgAreaW < 10 {
		msgAreaW = 10
	}
	contAreaW := msgAreaW - 2
	if contAreaW < 4 {
		contAreaW = 4
	}
	// Continuation lines align one stop past the prefix.
	contIndent := safeRepeat(" ", logPrefixW+2)

	nLogLines := 4
	if len(inst.Logs) == 0 {
		sb.WriteString("  " + styles.TUISubtleText.Render("no logs yet") + "\n")
	} else {
		start := len(inst.Logs) - nLogLines
		if start < 0 {
			start = 0
		}
		for i, ll := range inst.Logs[start:] {
			if ll.Level != "" && i > 0 {
				sb.WriteString("\n")
			}
			ts := styles.TUISubtleText.Render(padRight(ll.Time, 19))
			lvl := styles.TUILogLevel(ll.Level)
			prefix := "  " + ts + " " + lvl + "  "
			parts := wrapWords(ll.Msg, msgAreaW, contAreaW)
			for j, part := range parts {
				if j == 0 {
					sb.WriteString(prefix + styles.TUISecondaryText.Render(part) + "\n")
				} else {
					sb.WriteString(contIndent + styles.TUISecondaryText.Render(part) + "\n")
				}
			}
		}
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// renderSparkline renders bars Unicode block characters scaled to absolute [0,1] values.
func renderSparkline(history []float64, color lipgloss.Color, bars int) string {
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	padded := make([]float64, bars)
	if len(history) > 0 {
		copy(padded[bars-len(history):], history)
	}

	activeStyle := lipgloss.NewStyle().Foreground(color)
	dimStyle := lipgloss.NewStyle().Foreground(styles.SignalTextTertiary)

	var sb strings.Builder
	dataStart := bars - len(history)
	for i, v := range padded {
		// Absolute scale: v is [0,1], floor at 6% so idle shows a baseline bar.
		if v < 0.06 {
			v = 0.06
		}
		if v > 1.0 {
			v = 1.0
		}
		idx := int(v*float64(len(blocks)-1) + 0.5)
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		ch := string(blocks[idx])
		if i < dataStart {
			sb.WriteString(dimStyle.Render(ch))
		} else {
			sb.WriteString(activeStyle.Render(ch))
		}
	}
	return sb.String()
}
