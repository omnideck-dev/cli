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

	var sb strings.Builder
	sb.WriteString("\n")

	// Title row with status chips right-aligned.
	title := styles.TNTextBold.Render("Instances")
	sub := styles.TNFaintText.Render(" managed by this host")
	titleLeft := title + sub

	counts := map[string]int{}
	for _, inst := range m.instances {
		counts[inst.Status]++
	}
	var chipParts []string
	for _, s := range []string{"running", "paused", "stopped"} {
		if n := counts[s]; n > 0 {
			dot := lipgloss.NewStyle().Foreground(styles.TNStatusColor(s)).Render("●")
			chip := lipgloss.NewStyle().
				Background(lipgloss.Color("#1e2030")).
				Foreground(styles.TNStatusColor(s)).
				Padding(0, 1).
				Render(fmt.Sprintf("%s %d %s", dot, n, s))
			chipParts = append(chipParts, chip)
		}
	}
	chipsStr := strings.Join(chipParts, "  ")
	titleGap := w - lipgloss.Width(titleLeft) - lipgloss.Width(chipsStr) - 4
	if titleGap < 1 {
		titleGap = 1
	}
	sb.WriteString("  " + titleLeft + safeRepeat(" ", titleGap) + chipsStr + "\n\n")

	if len(m.instances) == 0 {
		sb.WriteString("  " + styles.TNDimText.Render("No Omnideck instances are set up yet.") + "\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press ") + styles.TNKeyChip.Render("n") + styles.TNDimText.Render(" to set one up.") + "\n")
		return padToHeight(sb.String(), h)
	}

	// cardW is the Lipgloss Width() arg. Lipgloss wraps at cardW-2 (subtracts
	// left+right padding). Outer card = cardW + 2 (border). With "  " prefix
	// total line = cardW + 4. Set cardW = w - 6 so cards span w - 2 columns.
	cardW := w - 6
	if cardW < 20 {
		cardW = 20
	}

	for i := range m.instances {
		card := m.renderInstanceCard(i, cardW)
		for _, line := range strings.Split(card, "\n") {
			sb.WriteString("  " + line + "\n")
		}
		sb.WriteString("\n")
	}

	// Toast pinned at bottom.
	if m.toast != "" {
		toastLine := "  " + styles.TNBlueTxt.Render("  "+m.toast)
		sb.WriteString(toastLine + "\n")
	}

	return padToHeight(sb.String(), h)
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
	caret := styles.TNBlueTxt.Render(caretCh)
	caretW := lipgloss.Width(caret)

	// Status dot + name + port badge.
	dot := styles.TNStatusDot(inst.Status)
	name := styles.TNTextBold.Render(inst.Info.Name)
	portStr := ":" + inst.Info.Config.WebUIPortOrDefault()
	portBadge := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e2030")).
		Foreground(styles.TNFgMid).
		Padding(0, 1).Render(portStr)
	identity := caret + "  " + dot + " " + name + "  " + portBadge
	identityW := lipgloss.Width(identity)

	// Image path (line 2, below name).
	imgIndent := caretW + 5 // caret + "  " + dot + " "
	imageStr := styles.TNDimText.Render(tnTruncate(inst.Info.Config.Image, identityW+6))
	imageLine := safeRepeat(" ", imgIndent) + imageStr

	// CPU sparkline block.
	cpuVal := dashOr(inst.CPU)
	cpuLabel := styles.TNFaintText.Render("CPU") + " " + styles.TNBlueTxt.Bold(true).Render(cpuVal)
	cpuSpark := renderSparkline(inst.CPUHistory, styles.TNBlue, 16)
	cpuBlock := cpuLabel + "  " + cpuSpark

	// MEM sparkline block.
	ramVal := dashOr(inst.RAM)
	memLabel := styles.TNFaintText.Render("MEM") + " " + styles.TNPurpleTxt.Bold(true).Render(ramVal)
	memSpark := renderSparkline(inst.RAMHistory, styles.TNPurple, 16)
	memBlock := memLabel + "  " + memSpark

	stats := cpuBlock + "   " + memBlock
	statsW := lipgloss.Width(stats)

	// 4 action chips.
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
		sep := styles.TNFaintText.Render(safeRepeat("─", contentW))
		accordion := m.renderCardAccordion(inst, contentW)
		content = content + "\n" + sep + "\n" + accordion
	}

	borderColor := styles.TNBorder
	if selected {
		borderColor = styles.TNBlue
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.TNBgAlt).
		Padding(0, 1).
		Width(cardW).
		Render(content)
}

// renderActionChips renders 4 action chips for a card. Focused chip is highlighted.
func (m AppModel) renderActionChips(inst *InstanceState, cardIdx int) string {
	isSelected := cardIdx == m.selected
	chipBase := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e2030")).
		Foreground(styles.TNFgMid).
		Padding(0, 1)
	chipFocused := lipgloss.NewStyle().
		Background(styles.TNBlue).
		Foreground(lipgloss.Color("#16161e")).
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
				Background(styles.TNRed).
				Foreground(lipgloss.Color("#16161e")).
				Bold(true).Padding(0, 1).Render(label)
		} else {
			toggleChip = lipgloss.NewStyle().
				Background(lipgloss.Color("#3d1a25")).
				Foreground(styles.TNRed).
				Padding(0, 1).Render(label)
		}
	} else {
		label := "▶ Start"
		if isSelected && m.chipFocus == 3 {
			toggleChip = lipgloss.NewStyle().
				Background(styles.TNGreen).
				Foreground(lipgloss.Color("#16161e")).
				Bold(true).Padding(0, 1).Render(label)
		} else {
			toggleChip = lipgloss.NewStyle().
				Background(lipgloss.Color("#1a3020")).
				Foreground(styles.TNGreen).
				Padding(0, 1).Render(label)
		}
	}

	return openChip + " " + logsChip + " " + updateChip + " " + toggleChip
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

	colSep := styles.TNFaintText.Render(safeRepeat("─", colW))

	// --- Metadata column rows ---
	mkv := func(k, v string, vstyle lipgloss.Style) string {
		key := styles.TNDimText.Render(padRight(k, 9))
		val := vstyle.Render(tnTruncate(v, colW-10))
		return key + val
	}
	metaRows := []string{
		styles.TNDimText.Bold(true).Render("METADATA"),
		colSep,
		mkv("image", cfg.Image, styles.TNCyanTxt),
		mkv("uptime", dashOr(inst.Uptime), styles.TNTextSub),
		mkv("restarts", dashOr(inst.Restarts), styles.TNTextSub),
		mkv("created", dashOr(inst.Created), styles.TNTextSub),
		mkv("health", dashOr(inst.Health), healthStyle(inst.Health)),
	}

	// --- Resources column rows ---
	barW := colW - 14
	if barW < 4 {
		barW = 4
	}
	cpuBar := styles.TNGradientBar(inst.CPUPct, barW, lipgloss.Color("#2b4fa0"), styles.TNBlue)
	ramBar := styles.TNGradientBar(inst.RAMPct, barW, lipgloss.Color("#6b3aaf"), styles.TNPurple)
	resRows := []string{
		styles.TNDimText.Bold(true).Render("RESOURCES"),
		colSep,
		styles.TNTextMid.Render(padRight("CPU", 10)) + styles.TNBlueTxt.Bold(true).Render(dashOr(inst.CPU)) + "  " + styles.TNFaintText.Render("/ 100%"),
		cpuBar,
		"",
		styles.TNTextMid.Render(padRight("Memory", 10)) + styles.TNPurpleTxt.Bold(true).Render(dashOr(inst.RAM)) + "  " + styles.TNFaintText.Render("/ "+dashOr(inst.RAMTotal)),
		ramBar,
		"",
		styles.TNTextMid.Render("network") + "  " + styles.TNTextSub.Render("↑ "+dashOr(inst.NetUp)+"  ↓ "+dashOr(inst.NetDown)),
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
	sb.WriteString(styles.TNFaintText.Render(safeRepeat("─", innerW)) + "\n")

	// Log tail — raw text, no nested panel.
	logTitle := styles.TNDimText.Render("LOGS · TAIL")
	logHint := styles.TNFaintText.Render("open full logs →")
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
		sb.WriteString("  " + styles.TNFaintText.Render("no logs yet") + "\n")
	} else {
		start := len(inst.Logs) - nLogLines
		if start < 0 {
			start = 0
		}
		for i, ll := range inst.Logs[start:] {
			if ll.Level != "" && i > 0 {
				sb.WriteString("\n")
			}
			ts := styles.TNFaintText.Render(padRight(ll.Time, 19))
			lvl := styles.TNLogLevel(ll.Level)
			prefix := "  " + ts + " " + lvl + "  "
			parts := wrapWords(ll.Msg, msgAreaW, contAreaW)
			for j, part := range parts {
				if j == 0 {
					sb.WriteString(prefix + styles.TNTextMid.Render(part) + "\n")
				} else {
					sb.WriteString(contIndent + styles.TNTextMid.Render(part) + "\n")
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
	dimStyle := lipgloss.NewStyle().Foreground(styles.TNFaint)

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
