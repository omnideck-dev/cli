package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// View dispatches rendering to the active screen.
func (m DashboardModel) View() string {
	body := m.renderBody()
	return m.renderHeader() + body + m.renderFooter()
}

// --- Layout constants ---

const (
	headerLines = 1
	footerLines = 1
)

func (m DashboardModel) contentHeight() int {
	h := m.height - headerLines - footerLines
	if h < 0 {
		return 0
	}
	return h
}

// --- Header ---

func (m DashboardModel) renderHeader() string {
	running := 0
	for _, inst := range m.instances {
		if inst.Status == "running" {
			running++
		}
	}

	logo := styles.TNBoldBlue.Render("◆") + " " + styles.TNTextBold.Render("omnideck")
	sep := styles.TNFaintText.Render(" │ ")
	breadcrumb := styles.TNDimText.Render(m.breadcrumb())
	left := logo + sep + breadcrumb

	daemonDot := styles.TNGreenTxt.Render("●")
	daemonLabel := styles.TNDimText.Render(fmt.Sprintf(" %d running", running))
	countLabel := styles.TNFaintText.Render(fmt.Sprintf("  %d instances", len(m.instances)))
	clock := styles.TNFaintText.Render("  " + m.clock)
	right := daemonDot + daemonLabel + countLabel + clock

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + safeRepeat(" ", gap) + right
	return styles.TNHeaderBar.Width(m.width).Render(line) + "\n"
}

func (m DashboardModel) breadcrumb() string {
	switch m.screen {
	case ScreenDashboard:
		return "Instances"
	case ScreenLogs:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name + " › Logs"
		}
		return "Logs"
	case ScreenConfig:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name + " › Config"
		}
		return "Config"
	case ScreenDoctor:
		return "Doctor"
	case ScreenInstall:
		switch m.installModel.Phase {
		case PhasePreflight:
			return "Setup · Quick check"
		case PhaseRuntimeSetup:
			return "Setup · Container setup"
		case PhaseConfig:
			return "Setup · Settings"
		case PhaseConfirm:
			return "Setup · Review"
		case PhaseInstall:
			return "Setup · Working"
		case PhaseDone:
			return "Setup · Ready"
		case PhaseError:
			return "Setup · Needs attention"
		}
		return "Setup"
	case ScreenUpdate:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name + " › Update"
		}
		return "Update"
	}
	return ""
}

// --- Footer ---

func (m DashboardModel) renderFooter() string {
	hints := m.footerHints()
	right := styles.TNFaintText.Render("omnideck tui")
	gap := m.width - lipgloss.Width(hints) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := hints + safeRepeat(" ", gap) + right
	return styles.TNFooterBar.Width(m.width).Render(line)
}

func (m DashboardModel) footerHints() string {
	switch m.screen {
	case ScreenDashboard:
		if m.chipFocus >= 0 {
			return keyHints([][2]string{
				{"tab", "cycle"}, {"enter", "activate"}, {"esc", "deselect"},
			})
		}
		if m.isExpanded() {
			return keyHints([][2]string{
				{"↑↓", "move"}, {"enter", "collapse"}, {"tab", "chip"}, {"l", "logs"}, {"s", "toggle"}, {"esc", "collapse"},
			})
		}
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "expand"}, {"l", "logs"}, {"s", "toggle"}, {"n", "new"}, {"d", "doctor"}, {"q", "quit"},
		})
	case ScreenLogs:
		if m.logSearchMode {
			return keyHints([][2]string{{"type", "filter"}, {"enter", "done"}, {"esc", "clear"}})
		}
		if m.logSearchQuery != "" {
			return keyHints([][2]string{
				{"↑↓", "scroll"}, {"pg↑↓", "page"}, {"/", "edit filter"}, {"esc", "clear filter"}, {"y", "copy"}, {"r", "refresh"},
			})
		}
		return keyHints([][2]string{
			{"↑↓", "scroll"}, {"pg↑↓", "page"}, {"home/end", "top/bot"}, {"/", "search"}, {"y", "copy"}, {"r", "refresh"}, {"esc", "back"},
		})
	case ScreenConfig:
		if m.cfgEditing {
			return keyHints([][2]string{{"enter", "confirm"}, {"esc", "cancel"}})
		}
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "edit"}, {"ctrl+s", "save"}, {"esc", "close"},
		})
	case ScreenDoctor:
		return keyHints([][2]string{{"esc", "close"}})
	case ScreenInstall:
		switch m.installModel.Phase {
		case PhasePreflight:
			if m.installModel.preflightReady && m.installModel.preferredEngine == "" && len(m.installModel.availableEngines) > 1 {
				return keyHints([][2]string{{"tab", "switch"}, {"enter", "continue"}, {"q", "cancel"}})
			}
		case PhaseRuntimeSetup:
			if m.installModel.runtimeBusy {
				return keyHints([][2]string{{"working", "please wait"}})
			}
			if m.installModel.runtimeWaiting {
				hints := [][2]string{{"enter", "check again"}}
				if len(m.installModel.runtimePlans) > 0 && m.installModel.runtimePlans[m.installModel.runtimeChoice].URL != "" {
					label := "open page again"
					if m.installModel.runtimePlans[m.installModel.runtimeChoice].DirectDownload {
						label = "download again"
					}
					hints = append(hints, [2]string{"o", label})
				}
				return keyHints(append(hints, [2]string{"b", "back"}, [2]string{"q", "cancel"}))
			}
			if m.installModel.runtimeConfirm {
				action := "start these steps"
				if len(m.installModel.runtimePlans) > 0 && len(m.installModel.runtimePlans[m.installModel.runtimeChoice].Commands) == 0 {
					action = "open official page"
					if m.installModel.runtimePlans[m.installModel.runtimeChoice].DirectDownload {
						action = "download installer"
					}
				}
				return keyHints([][2]string{{"enter", action}, {"b", "back"}, {"d", "technical details"}, {"q", "cancel"}})
			}
			return keyHints([][2]string{{"↑↓", "choose"}, {"enter", "review"}, {"d", "technical details"}, {"r", "check again"}, {"q", "cancel"}})
		case PhaseConfig:
			if !m.installModel.configAdvanced {
				return keyHints([][2]string{{"enter", "use recommended"}, {"c", "customize"}, {"q", "cancel"}})
			}
			return keyHints([][2]string{{"tab", "next"}, {"shift+tab", "back"}, {"esc", "recommended settings"}})
		case PhaseConfirm:
			return keyHints([][2]string{{"enter", "start setup"}, {"b", "back"}, {"d", "technical details"}, {"q", "cancel"}})
		case PhaseDone:
			return keyHints([][2]string{{"any key", "return"}})
		case PhaseError:
			return keyHints([][2]string{{"r", "try again"}, {"d", "details for support"}, {"b", "return"}})
		}
		return ""
	case ScreenUpdate:
		if m.updateModel.Phase == PhaseDone || m.updateModel.Phase == PhaseError {
			return keyHints([][2]string{{"any key", "return"}})
		}
		return ""
	}
	return ""
}

func keyHints(pairs [][2]string) string {
	var sb strings.Builder
	for i, p := range pairs {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(styles.TNKeyChip.Render(p[0]))
		sb.WriteString(" ")
		sb.WriteString(styles.TNDimText.Render(p[1]))
	}
	return sb.String()
}

// --- Body dispatcher ---

func (m DashboardModel) renderBody() string {
	switch m.screen {
	case ScreenDashboard:
		return m.viewDashboard()
	case ScreenLogs:
		return m.viewLogs()
	case ScreenConfig:
		return m.viewConfig()
	case ScreenDoctor:
		return m.viewDoctor()
	case ScreenInstall:
		return m.viewInstall()
	case ScreenUpdate:
		return m.viewUpdate()
	}
	return ""
}

// viewModalOverlay centers modal in the content area over a dark background.
func (m DashboardModel) viewModalOverlay(modal string) string {
	h := m.contentHeight()
	w := m.width
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(styles.TNBg),
		lipgloss.WithWhitespaceForeground(styles.TNBg))
}

// --- Dashboard screen (card layout) ---

func (m DashboardModel) viewDashboard() string {
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
func (m DashboardModel) renderInstanceCard(idx, cardW int) string {
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
func (m DashboardModel) renderActionChips(inst *InstanceState, cardIdx int) string {
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
func (m DashboardModel) renderCardAccordion(inst *InstanceState, innerW int) string {
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

// --- Metadata and resource panels (shared by accordion and detail views) ---

func (m DashboardModel) renderMetaPanel(inst *InstanceState, w, h int) string {
	cfg := inst.Info.Config
	innerW := w - 2

	kv := func(k, v string, vstyle lipgloss.Style) string {
		key := styles.TNTextMid.Render(padRight(k, 10))
		val := vstyle.Bold(true).Render(tnTruncate(v, innerW-12))
		return key + val
	}

	rows := []string{
		kv("image", cfg.Image, styles.TNCyanTxt),
		kv("uptime", dashOr(inst.Uptime), styles.TNTextSub),
		kv("restarts", dashOr(inst.Restarts), styles.TNTextSub),
		kv("created", dashOr(inst.Created), styles.TNTextSub),
		kv("health", dashOr(inst.Health), healthStyle(inst.Health)),
	}

	content := strings.Join(rows, "\n")
	label := styles.TNDimText.Bold(true).Render("METADATA")
	return styles.TNPanel.Width(w).Height(h).Render(label + "\n\n" + content)
}

func (m DashboardModel) renderResourcePanel(inst *InstanceState, w, h int) string {
	barW := w - 22
	if barW < 4 {
		barW = 4
	}

	cpuBar := styles.TNGradientBar(inst.CPUPct, barW,
		lipgloss.Color("#2b4fa0"), styles.TNBlue)
	ramBar := styles.TNGradientBar(inst.RAMPct, barW,
		lipgloss.Color("#6b3aaf"), styles.TNPurple)

	cpuVal := dashOr(inst.CPU)
	ramVal := dashOr(inst.RAM)
	cpuMax := styles.TNFaintText.Render("/ 100%")
	ramMax := styles.TNFaintText.Render("/ " + dashOr(inst.RAMTotal))

	rows := []string{
		styles.TNTextMid.Render(padRight("CPU", 8)) + styles.TNTextSub.Bold(true).Render(cpuVal),
		cpuBar + "  " + cpuMax,
		"",
		styles.TNTextMid.Render(padRight("Memory", 8)) + styles.TNTextSub.Bold(true).Render(ramVal),
		ramBar + "  " + ramMax,
		"",
		styles.TNTextMid.Render("network") + "  " + styles.TNTextSub.Bold(true).Render("↑ "+dashOr(inst.NetUp)+"  ↓ "+dashOr(inst.NetDown)),
	}

	content := strings.Join(rows, "\n")
	label := styles.TNDimText.Bold(true).Render("RESOURCES")
	return styles.TNPanel.Width(w).Height(h).Render(label + "\n\n" + content)
}

func (m DashboardModel) renderLogsPreview(inst *InstanceState, w, h int) string {
	title := styles.TNDimText.Render("LOGS · TAIL")
	hint := styles.TNFaintText.Render("open full logs →")
	titleRow := title + safeRepeat(" ", max(1, w-lipgloss.Width(title)-lipgloss.Width(hint)-2)) + hint

	var lines []string
	if len(inst.Logs) == 0 {
		lines = append(lines, styles.TNFaintText.Render("  no logs yet"))
	} else {
		start := len(inst.Logs) - (h - 2)
		if start < 0 {
			start = 0
		}
		for _, ll := range inst.Logs[start:] {
			ts := styles.TNFaintText.Render(padRight(ll.Time, 19))
			lvl := styles.TNLogLevel(ll.Level)
			msg := styles.TNTextMid.Render(tnTruncate(ll.Msg, w-28))
			lines = append(lines, "  "+ts+" "+lvl+"  "+msg)
		}
	}

	inner := strings.Join(lines, "\n")
	return styles.TNPanel.Width(w).Height(h).Render(titleRow + "\n" + inner)
}

// --- Full logs screen (modal overlay) ---

func (m DashboardModel) viewLogs() string {
	inst := m.CurrentInstance()
	h := m.contentHeight()
	w := m.width

	// Modal dimensions: span nearly the full terminal width.
	contentW := w - 4
	if contentW < 40 {
		contentW = 40
	}
	modalH := h - 6
	if modalH < 8 {
		modalH = 8
	}

	// Header row.
	instName := ""
	if inst != nil {
		instName = inst.Info.Name
	}
	filtered := m.filteredLogs()
	var totalLines int
	if inst != nil {
		totalLines = len(inst.Logs)
	}

	hdrLeft := styles.TNTextBold.Render(instName) + " " + styles.TNFaintText.Render("stdout + stderr")
	var hdrRight string
	switch {
	case m.logCopied:
		hdrRight = styles.TNGreenTxt.Render("✓ copied!")
	case m.logSearchQuery != "":
		hdrRight = styles.TNBlueTxt.Render(fmt.Sprintf("%d", len(filtered))) +
			styles.TNFaintText.Render(fmt.Sprintf(" of %d", totalLines))
	default:
		hdrRight = styles.TNFaintText.Render(fmt.Sprintf("%d lines", totalLines))
	}
	hdrRight += "  " + styles.TNFaintText.Render("following")

	hdrGap := max(1, contentW-lipgloss.Width(hdrLeft)-lipgloss.Width(hdrRight)-2)
	hdrLine := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TNFaintText.Render(safeRepeat("─", contentW))

	// Log lines with word-wrap. prefix = "  " + num(4) + "  " + ts(9) + "  " + lvl(5) + "  " = 26
	// logViewPrefixW: "  "(2) + num(4) + "  "(2) + ts(19) + "  "(2) + lvl(5) + "  "(2) = 36
	const logViewPrefixW = 36
	msgW := contentW - logViewPrefixW
	if msgW < 10 {
		msgW = 10
	}
	msgContW := msgW - 2
	if msgContW < 4 {
		msgContW = 4
	}
	logContIndent := safeRepeat(" ", logViewPrefixW+2)

	visibleH := m.logModalVisibleLines()
	start := m.logScroll
	if start < 0 {
		start = 0
	}

	var logSb strings.Builder
	rendered := 0
	for entryNum, ll := range filtered[start:] {
		if rendered >= visibleH {
			break
		}
		// Blank line before labeled entries to visually group them — skip for the first visible line.
		if ll.Level != "" && entryNum > 0 && rendered < visibleH {
			logSb.WriteString("\n")
			rendered++
		}
		if rendered >= visibleH {
			break
		}
		num := styles.TNFaintText.Render(fmt.Sprintf("%4d", start+entryNum+1))
		ts := styles.TNFaintText.Render(padRight(ll.Time, 19))
		lvl := styles.TNLogLevel(ll.Level)
		prefix := "  " + num + "  " + ts + "  " + lvl + "  "
		for i, part := range wrapWords(ll.Msg, msgW, msgContW) {
			if rendered >= visibleH {
				break
			}
			if i == 0 {
				logSb.WriteString(prefix + styles.TNTextMid.Render(part) + "\n")
			} else {
				logSb.WriteString(logContIndent + styles.TNTextMid.Render(part) + "\n")
			}
			rendered++
		}
	}
	if start >= len(filtered) {
		if m.logSearchQuery != "" {
			logSb.WriteString("  " + styles.TNFaintText.Render("no matching lines") + "\n")
		} else {
			logSb.WriteString("  " + styles.TNFaintText.Render("no logs available") + "\n")
		}
	}

	body := hdrLine + "\n" + sep + "\n" + logSb.String()

	// Search bar below log content.
	if m.logSearchMode || m.logSearchQuery != "" {
		prefix := styles.TNBlueTxt.Render("/  ")
		queryStr := m.logSearchQuery
		if m.logSearchMode {
			queryStr += "█"
		}
		var searchRight string
		if m.logSearchQuery != "" {
			searchRight = styles.TNFaintText.Render(fmt.Sprintf("%d of %d", len(filtered), totalLines))
		} else {
			searchRight = styles.TNFaintText.Render("type to filter…")
		}
		queryRender := styles.TNTextSub.Render(queryStr)
		searchGap := max(1, contentW-lipgloss.Width(prefix)-lipgloss.Width(queryRender)-lipgloss.Width(searchRight)-2)
		searchLine := sep + "\n" + prefix + queryRender + safeRepeat(" ", searchGap) + searchRight
		body += searchLine
	}

	modal := styles.TNModal.Width(contentW).Height(modalH).Render(body)
	return m.viewModalOverlay(modal)
}

// --- Config modal ---

func (m DashboardModel) viewConfig() string {
	inst := m.CurrentInstance()
	h := m.contentHeight()
	w := m.width

	contentW := w - 22
	if contentW > 68 {
		contentW = 68
	}
	if contentW < 40 {
		contentW = 40
	}

	var hdrRight string
	if inst != nil {
		hdrRight = styles.TNFaintText.Render(inst.Info.Name + ".yaml")
	}
	hdrLeft := styles.TNPurpleTxt.Render("⚙") + "  " + styles.TNTextBold.Render("Edit config")
	hdrGap := contentW - lipgloss.Width(hdrLeft) - lipgloss.Width(hdrRight) - 2
	if hdrGap < 1 {
		hdrGap = 1
	}
	header := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TNFaintText.Render(safeRepeat("─", contentW))

	var rows []string
	for i, f := range m.cfgFields {
		selected := i == m.cfgFocus
		keyS := styles.TNTextSub
		if selected {
			keyS = styles.TNBlueTxt
		}
		typeS := styles.TNFaintText.Render(f.Type)

		var valStr string
		if selected && m.cfgEditing {
			valStr = styles.TNText.Render(m.cfgBuf) + styles.TNBlueTxt.Render("█")
		} else {
			valS := styles.TNTextSub
			if f.Changed {
				valS = styles.TNGreenTxt
			}
			valStr = valS.Render(tnTruncate(f.Value, contentW-30))
			if f.Changed {
				valStr += "  " + styles.TNGreenTxt.Render("●")
			}
		}

		caret := " "
		if selected {
			caret = styles.TNBlueTxt.Render("▸")
		}
		row := caret + " " + keyS.Render(padRight(f.Key, 16)) + typeS + "  " + valStr
		rows = append(rows, row)
	}

	legend := styles.TNGreenTxt.Render("●") + styles.TNFaintText.Render(" changed since last save")
	keyhints := styles.TNFaintText.Render("ctrl+s") + styles.TNDimText.Render("  save    ") +
		styles.TNFaintText.Render("esc") + styles.TNDimText.Render("  close")
	body := header + "\n" + sep + "\n" + strings.Join(rows, "\n") + "\n" + sep + "\n" + legend + "\n" + keyhints
	modal := styles.TNModal.Width(contentW).Padding(1, 2).Render(body)

	_ = h // height used implicitly by Place
	return m.viewModalOverlay(modal)
}

// --- Doctor modal ---

func (m DashboardModel) viewDoctor() string {
	w := m.width

	header := styles.TNCyanTxt.Render("✚") + "  " + styles.TNTextBold.Render("Doctor") + "  " + styles.TNFaintText.Render("system diagnostics")
	sep := styles.TNFaintText.Render(strings.Repeat("─", 50))

	var bodyLines []string
	if !m.doctorDone {
		bodyLines = append(bodyLines, m.doctorSpinner.View()+" "+styles.TNDimText.Render("Running diagnostics…"))
	} else {
		for _, r := range m.doctorResults {
			var icon, labelS string
			switch r.Status {
			case CheckPass:
				icon = styles.TNGreenTxt.Render("✓")
				labelS = styles.TNTextMid.Render(r.Label)
			case CheckFail:
				icon = styles.TNRedTxt.Render("✗")
				labelS = styles.TNRedTxt.Render(r.Label)
			case CheckWarn:
				icon = styles.TNYellowTxt.Render("!")
				labelS = styles.TNYellowTxt.Render(r.Label)
			}
			detail := styles.TNFaintText.Render(tnTruncate(r.Detail, 40))
			row := icon + "  " + padRightStyled(labelS, 26) + "  " + detail
			bodyLines = append(bodyLines, row)
			if r.Hint != "" && r.Status != CheckPass {
				bodyLines = append(bodyLines, "     "+styles.TNDimText.Render("→ "+tnTruncate(r.Hint, 60)))
			}
		}
	}

	innerW := 58
	if w-24 < innerW {
		innerW = w - 24
	}
	inner := header + "\n" + sep + "\n" + strings.Join(bodyLines, "\n")
	modal := styles.TNModal.Width(innerW).Padding(1, 2).Render(inner)

	return m.viewModalOverlay(modal)
}

// --- Install screen (modal) ---

func (m DashboardModel) viewInstall() string {
	h := m.contentHeight()
	w := m.width

	contentW := w - 10
	if contentW > 88 {
		contentW = 88
	}
	if contentW < 40 {
		contentW = 40
	}
	modalH := h - 4
	if modalH < 8 {
		modalH = 8
	}

	titleRow := styles.TNBoldBlue.Render("◆") + "  " + styles.TNTextBold.Render("Setup")
	sep := styles.TNFaintText.Render(safeRepeat("─", contentW))

	innerW := contentW - 4
	content := titleRow + "\n" + sep + "\n" + m.installModel.TNView(innerW, modalH-4)
	modal := styles.TNModal.Width(contentW).Height(modalH).Render(content)
	return m.viewModalOverlay(modal)
}

// --- Update screen (modal) ---

func (m DashboardModel) viewUpdate() string {
	inst := m.CurrentInstance()
	h := m.contentHeight()
	w := m.width

	contentW := w - 14
	if contentW > 78 {
		contentW = 78
	}
	if contentW < 40 {
		contentW = 40
	}
	modalH := h - 6
	if modalH < 8 {
		modalH = 8
	}

	instName := ""
	if inst != nil {
		instName = inst.Info.Name
	}
	title := styles.TNBlueTxt.Render("◆") + "  " + styles.TNTextBold.Render("Update") + "  " + styles.TNFaintText.Render(instName)
	hint := styles.TNFaintText.Render("press ") + styles.TNKeyChip.Render("any key") + styles.TNFaintText.Render(" when done")
	gap := contentW - lipgloss.Width(title) - lipgloss.Width(hint) - 2
	titleRow := title + safeRepeat(" ", max(1, gap)) + hint

	updateContent := titleRow + "\n" + m.updateModel.TNView(contentW-4)
	modal := styles.TNModal.Width(contentW).Height(modalH).Render(updateContent)
	return m.viewModalOverlay(modal)
}

// --- Helpers ---

// prefixLines adds prefix to every line of a rendered multi-line block.
func prefixLines(block, prefix string) string {
	block = strings.TrimSuffix(block, "\n")
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}

// wrapWords word-wraps text into lines, preserving original spacing.
// It scans rune-by-rune, breaking at the last space within the width limit.
// Long runs with no spaces are hard-broken at the limit.
// firstW is the max rune-width for the first line; contW for subsequent ones.
func wrapWords(text string, firstW, contW int) []string {
	if firstW <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}

	var lines []string
	limit := firstW
	pos := 0

	for pos < len(runes) {
		end := pos + limit
		if end >= len(runes) {
			lines = append(lines, string(runes[pos:]))
			break
		}
		// Scan backwards from end-1 for a space to break on.
		breakAt := -1
		for i := end - 1; i > pos; i-- {
			if runes[i] == ' ' {
				breakAt = i
				break
			}
		}
		if breakAt < 0 {
			// No space found — hard break at limit.
			lines = append(lines, string(runes[pos:end]))
			pos = end
		} else {
			lines = append(lines, string(runes[pos:breakAt]))
			pos = breakAt + 1 // skip the break-point space
		}
		limit = contW
	}
	return lines
}

// safeRepeat is strings.Repeat guarded against negative counts.
func safeRepeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

func padToHeight(s string, h int) string {
	lines := strings.Count(s, "\n")
	if lines >= h {
		return s
	}
	return s + strings.Repeat("\n", h-lines)
}

// padRightStyled pads a styled string to width n (measures rendered width).
func padRightStyled(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func dashOr(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func healthStyle(health string) lipgloss.Style {
	switch health {
	case "healthy":
		return styles.TNGreenTxt
	case "degraded", "unhealthy":
		return styles.TNYellowTxt
	default:
		return styles.TNDimText
	}
}
