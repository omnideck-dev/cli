package tui

import (
	"fmt"
	"strconv"
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

	// Pad to full width.
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2 // -2 for padding
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
	case ScreenDetail:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name
		}
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
			return "Install · Preflight"
		case PhaseConfig:
			return "Install · Configure"
		case PhaseConfirm:
			return "Install · Confirm"
		case PhaseInstall:
			return "Install · Running"
		case PhaseDone:
			return "Install · Done"
		case PhaseError:
			return "Install · Error"
		}
		return "Install"
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
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "inspect"}, {"n", "new"}, {"d", "doctor"}, {"r", "refresh"}, {"q", "quit"},
		})
	case ScreenDetail:
		return keyHints([][2]string{
			{"↑↓", "navigate"}, {"enter", "select"}, {"c", "config"}, {"d", "doctor"}, {"esc", "back"},
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
		case PhaseConfig:
			return keyHints([][2]string{{"tab", "next"}, {"shift+tab", "back"}, {"esc", "cancel"}})
		case PhaseConfirm:
			return keyHints([][2]string{{"i", "install"}, {"b", "back"}, {"q", "cancel"}})
		case PhaseDone, PhaseError:
			return keyHints([][2]string{{"any key", "return"}})
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
	case ScreenDetail:
		return m.viewDetail()
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

// --- Dashboard screen ---

const (
	colPort   = 7
	colStatus = 14 // "● running  "
	colCPU    = 8
	colRAM    = 12
	tableGap  = 2
)

func (m DashboardModel) viewDashboard() string {
	h := m.contentHeight()
	w := m.width

	var sb strings.Builder

	// Title row.
	sb.WriteString("\n")
	title := styles.TNTextBold.Render("Instances")
	sub := styles.TNFaintText.Render(" managed by this host")
	sb.WriteString("  " + title + sub + "\n\n")

	// Column headers.
	nameW := w - colPort - colStatus - colCPU - colRAM - 4 // 4 for padding+gap
	if nameW < 10 {
		nameW = 10
	}

	hdr := padRight("NAME", nameW) + padRight("PORT", colPort) + padRight("STATUS", colStatus) + padRight("CPU", colCPU) + "MEMORY"
	sb.WriteString("  " + styles.TNDimText.Render(strings.ToUpper(hdr)) + "\n")
	sb.WriteString("  " + styles.TNFaintText.Render(safeRepeat("─", w-4)) + "\n")

	if len(m.instances) == 0 {
		sb.WriteString("\n  " + styles.TNDimText.Render("No instances installed.") + "\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press ") + styles.TNKeyChip.Render("n") + styles.TNDimText.Render(" to install one.") + "\n")
		return padToHeight(sb.String(), h)
	}

	for i, inst := range m.instances {
		selected := i == m.selected
		caret := " "
		nameStyle := styles.TNTextMid
		if selected {
			caret = styles.TNBlueTxt.Render("▸")
			nameStyle = styles.TNTextBold
		}

		name := nameStyle.Render(tnTruncate(inst.Info.Name, nameW-2))
		name = padRightStyled(name, nameW)

		port := ":" + inst.Info.Config.WebUIPortOrDefault()
		portS := styles.TNDimText.Render(padRight(port, colPort))

		dot := styles.TNStatusDot(inst.Status)
		statusLabel := inst.Status
		if statusLabel == "unknown" {
			statusLabel = "…"
		}
		statusS := dot + " " + lipgloss.NewStyle().Foreground(styles.TNStatusColor(inst.Status)).Render(padRight(statusLabel, colStatus-3))

		cpu := inst.CPU
		if cpu == "" {
			cpu = "—"
		}
		cpuS := styles.TNTextSub.Render(padRight(cpu, colCPU))

		ram := inst.RAM
		if ram == "" {
			ram = "—"
		}
		ramS := styles.TNTextSub.Render(ram)

		row := " " + caret + " " + name + portS + statusS + cpuS + ramS

		if selected {
			row = styles.TNSelRow.Render(row)
		}
		sb.WriteString(row + "\n")
	}

	// Summary chips.
	sb.WriteString("\n  ")
	counts := map[string]int{}
	for _, inst := range m.instances {
		counts[inst.Status]++
	}
	for _, s := range []string{"running", "stopped", "paused"} {
		if n := counts[s]; n > 0 {
			dot := lipgloss.NewStyle().Foreground(styles.TNStatusColor(s)).Render("●")
			label := styles.TNDimText.Render(fmt.Sprintf(" %d %s  ", n, s))
			sb.WriteString(dot + label)
		}
	}
	sb.WriteString("\n")

	return padToHeight(sb.String(), h)
}

// --- Detail screen ---

func (m DashboardModel) viewDetail() string {
	inst := m.CurrentInstance()
	if inst == nil {
		return "\n  " + styles.TNDimText.Render("No instance selected.")
	}
	h := m.contentHeight()
	w := m.width

	var sb strings.Builder

	// Instance title row.
	dot := styles.TNStatusDot(inst.Status)
	statusLabel := lipgloss.NewStyle().Foreground(styles.TNStatusColor(inst.Status)).Render(inst.Status)
	imgShort := tnTruncate(inst.Info.Config.Image, 50)
	sb.WriteString("\n  " + styles.TNTextBold.Render("▸ "+inst.Info.Name) + "  " + dot + " " + statusLabel + "  " + styles.TNFaintText.Render(imgShort) + "\n\n")

	// Two-panel row: metadata + resources.
	panelH := 10
	halfW := (w - 6) / 2 // -6 for margins + gap

	meta := m.renderMetaPanel(inst, halfW, panelH)
	res := m.renderResourcePanel(inst, halfW, panelH)

	// Render side by side.
	metaLines := strings.Split(meta, "\n")
	resLines := strings.Split(res, "\n")
	for len(metaLines) < panelH+2 {
		metaLines = append(metaLines, safeRepeat(" ", halfW+2))
	}
	for len(resLines) < panelH+2 {
		resLines = append(resLines, safeRepeat(" ", halfW+2))
	}
	maxL := len(metaLines)
	if len(resLines) > maxL {
		maxL = len(resLines)
	}
	for i := range maxL {
		ml := ""
		if i < len(metaLines) {
			ml = metaLines[i]
		}
		rl := ""
		if i < len(resLines) {
			rl = resLines[i]
		}
		sb.WriteString("  " + ml + "  " + rl + "\n")
	}

	// Actions menu panel.
	menuH := h - panelH - 5 // remaining space
	if menuH < 4 {
		menuH = 4
	}
	sb.WriteString(prefixLines(m.renderDetailMenu(inst, w-4, menuH), "  "))

	return padToHeight(sb.String(), h)
}

func (m DashboardModel) renderMetaPanel(inst *InstanceState, w, h int) string {
	cfg := inst.Info.Config
	innerW := w - 2 // -2 for borders

	kv := func(k, v string, vstyle lipgloss.Style) string {
		key := styles.TNTextMid.Render(padRight(k, 10))
		val := vstyle.Bold(true).Render(tnTruncate(v, innerW-12))
		return key + val
	}

	rows := []string{
		kv("port", ":"+cfg.WebUIPortOrDefault(), styles.TNTextSub),
		kv("image", cfg.Image, styles.TNCyanTxt),
		kv("uptime", dashOr(inst.Uptime), styles.TNTextSub),
		kv("restarts", dashOr(inst.Restarts), styles.TNTextSub),
		kv("created", dashOr(inst.Created), styles.TNTextSub),
		kv("health", dashOr(inst.Health), healthStyle(inst.Health)),
	}

	content := strings.Join(rows, "\n")
	label := styles.TNTextBold.Render("METADATA")
	panel := styles.TNPanel.
		Width(w).
		Height(h).
		Render(label + "\n\n" + content)
	return panel
}

func (m DashboardModel) renderResourcePanel(inst *InstanceState, w, h int) string {
	barW := w - 22 // space for label + value
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
	label := styles.TNTextBold.Render("RESOURCES")
	panel := styles.TNPanel.
		Width(w).
		Height(h).
		Render(label + "\n\n" + content)
	return panel
}

func (m DashboardModel) renderDetailMenu(inst *InstanceState, w, h int) string {
	toggleLabel := "Stop container"
	if inst.Status != "running" {
		toggleLabel = "Start container"
	}
	items := []string{"Open UI", "Logs", "Update", toggleLabel}

	if m.detailBusy {
		statusLine := m.detailSpinner.View() + "  " + styles.TNDimText.Render(m.detailBusyAction+"…")
		rows := []string{statusLine}
		for _, item := range items {
			rows = append(rows, "")
			rows = append(rows, styles.TNFaintText.Render("      "+item))
		}
		inner := strings.Join(rows, "\n")
		return styles.TNPanelAccent.Width(w).Height(h).Render(inner) + "\n"
	}

	title := styles.TNTextBold.Render("ACTIONS")
	hint := styles.TNFaintText.Render("press ") + styles.TNKeyChip.Render("enter") + styles.TNFaintText.Render(" to select")
	gap := max(1, w-lipgloss.Width(title)-lipgloss.Width(hint)-4)
	titleRow := title + safeRepeat(" ", gap) + hint

	// Each item is preceded by a blank line for visual breathing room.
	var rows []string
	for i, item := range items {
		rows = append(rows, "") // blank spacer before each item
		if i == m.detailMenuIdx {
			rows = append(rows, styles.TNBlueTxt.Render("   ▸  ")+styles.TNTextBold.Render(item))
		} else {
			rows = append(rows, styles.TNDimText.Render("      ")+styles.TNTextMid.Render(item))
		}
	}

	inner := strings.Join(rows, "\n")
	return styles.TNPanelAccent.Width(w).Height(h).Render(titleRow+"\n"+inner) + "\n"
}

func (m DashboardModel) renderLogsPreview(inst *InstanceState, w, h int) string {
	title := styles.TNDimText.Render("LOGS · TAIL")
	hint := styles.TNFaintText.Render("press ") + styles.TNKeyChip.Render("l") + styles.TNFaintText.Render(" for full logs")
	titleRow := title + safeRepeat(" ", max(1, w-lipgloss.Width(title)-lipgloss.Width(hint)-4)) + hint

	var lines []string
	if len(inst.Logs) == 0 {
		lines = append(lines, styles.TNFaintText.Render("  no logs yet — container may be starting"))
	} else {
		start := len(inst.Logs) - (h - 2)
		if start < 0 {
			start = 0
		}
		for _, ll := range inst.Logs[start:] {
			ts := styles.TNFaintText.Render(padRight(ll.Time, 9))
			lvl := styles.TNLogLevel(ll.Level)
			msg := styles.TNTextMid.Render(tnTruncate(ll.Msg, w-30))
			lines = append(lines, "  "+ts+" "+lvl+"  "+msg)
		}
	}

	inner := strings.Join(lines, "\n")
	return styles.TNPanel.Width(w).Height(h).Render(titleRow + "\n" + inner) + "\n"
}

// --- Full logs screen (embedded in the Detail layout) ---

func (m DashboardModel) viewLogs() string {
	inst := m.CurrentInstance()
	if inst == nil {
		return "\n  " + styles.TNDimText.Render("No instance selected.")
	}
	h := m.contentHeight()
	w := m.width

	var sb strings.Builder

	// Instance title row — same as viewDetail.
	dot := styles.TNStatusDot(inst.Status)
	statusLabel := lipgloss.NewStyle().Foreground(styles.TNStatusColor(inst.Status)).Render(inst.Status)
	imgShort := tnTruncate(inst.Info.Config.Image, 50)
	sb.WriteString("\n  " + styles.TNTextBold.Render("▸ "+inst.Info.Name) + "  " + dot + " " + statusLabel + "  " + styles.TNFaintText.Render(imgShort) + "\n\n")

	// Metadata + resources panels — same as viewDetail.
	panelH := 10
	halfW := (w - 6) / 2
	meta := m.renderMetaPanel(inst, halfW, panelH)
	res := m.renderResourcePanel(inst, halfW, panelH)
	metaLines := strings.Split(meta, "\n")
	resLines := strings.Split(res, "\n")
	for len(metaLines) < panelH+2 {
		metaLines = append(metaLines, safeRepeat(" ", halfW+2))
	}
	for len(resLines) < panelH+2 {
		resLines = append(resLines, safeRepeat(" ", halfW+2))
	}
	maxPanelL := len(metaLines)
	if len(resLines) > maxPanelL {
		maxPanelL = len(resLines)
	}
	for i := range maxPanelL {
		ml, rl := "", ""
		if i < len(metaLines) {
			ml = metaLines[i]
		}
		if i < len(resLines) {
			rl = resLines[i]
		}
		sb.WriteString("  " + ml + "  " + rl + "\n")
	}

	// Log panel — replaces the actions menu, fills remaining height.
	showSearch := m.logSearchMode || m.logSearchQuery != ""
	logH := h - panelH - 5 // mirrors the logsH/menuH formula in viewDetail
	if showSearch {
		logH -= 3 // search bar panel = 3 terminal lines
	}
	if logH < 4 {
		logH = 4
	}
	panelW := w - 4

	filtered := m.filteredLogs()
	totalLines := len(inst.Logs)

	// Panel title row.
	logTitle := styles.TNDimText.Render("LOGS")
	var logRight string
	switch {
	case m.logCopied:
		logRight = styles.TNGreenTxt.Render("✓ copied!")
	case m.logSearchQuery != "":
		logRight = styles.TNBlueTxt.Render(fmt.Sprintf("%d", len(filtered))) +
			styles.TNFaintText.Render(fmt.Sprintf(" of %d", totalLines))
	default:
		logRight = styles.TNFaintText.Render(fmt.Sprintf("%d lines", totalLines))
	}
	titleGap := max(1, panelW-lipgloss.Width(logTitle)-lipgloss.Width(logRight)-6)
	logTitleRow := logTitle + safeRepeat(" ", titleGap) + logRight

	// Log content — logH inner height, minus 1 line for the title row.
	contentLines := logH - 1
	if contentLines < 1 {
		contentLines = 1
	}
	start := m.logScroll
	if start < 0 {
		start = 0
	}
	end := start + contentLines
	if end > len(filtered) {
		end = len(filtered)
	}

	var logSb strings.Builder
	logSb.WriteString(logTitleRow + "\n")
	for lineNum, ll := range filtered[start:end] {
		num := styles.TNFaintText.Render(fmt.Sprintf("%4d", start+lineNum+1))
		ts := styles.TNFaintText.Render(padRight(ll.Time, 9))
		lvl := styles.TNLogLevel(ll.Level)
		msg := styles.TNTextMid.Render(tnTruncate(ll.Msg, panelW-32))
		logSb.WriteString("  " + num + "  " + ts + "  " + lvl + "  " + msg + "\n")
	}
	if start >= len(filtered) {
		if m.logSearchQuery != "" {
			logSb.WriteString("  " + styles.TNFaintText.Render("no matching lines") + "\n")
		} else {
			logSb.WriteString("  " + styles.TNFaintText.Render("no logs available") + "\n")
		}
	}

	sb.WriteString(prefixLines(styles.TNPanel.Width(panelW).Height(logH).Render(logSb.String()), "  "))

	// Search bar below the log panel.
	if showSearch {
		prefix := styles.TNBlueTxt.Render("/  ")
		queryStr := m.logSearchQuery
		if m.logSearchMode {
			queryStr += "█"
		}
		queryRender := styles.TNTextSub.Render(queryStr)
		var searchRight string
		if m.logSearchQuery != "" {
			searchRight = styles.TNFaintText.Render(fmt.Sprintf("%d of %d", len(filtered), totalLines))
		} else {
			searchRight = styles.TNFaintText.Render("type to filter…")
		}
		searchGap := max(1, panelW-8-lipgloss.Width(prefix)-lipgloss.Width(queryRender)-lipgloss.Width(searchRight))
		searchLine := prefix + queryRender + safeRepeat(" ", searchGap) + searchRight
		sb.WriteString(prefixLines(styles.TNPanel.Width(panelW).Height(1).Render(searchLine), "  "))
	}

	return padToHeight(sb.String(), h)
}

// --- Config modal ---

func (m DashboardModel) viewConfig() string {
	inst := m.CurrentInstance()
	h := m.contentHeight()
	w := m.width

	modalW := w - 20
	if modalW > 700 {
		modalW = 70 // in chars, not px
	}
	if modalW < 40 {
		modalW = 40
	}

	var hdrRight string
	if inst != nil {
		hdrRight = styles.TNFaintText.Render(inst.Info.Name + ".yaml")
	}
	hdrLeft := styles.TNPurpleTxt.Render("⚙") + "  " + styles.TNTextBold.Render("Edit config")
	hdrGap := modalW - lipgloss.Width(hdrLeft) - lipgloss.Width(hdrRight) - 4
	if hdrGap < 1 {
		hdrGap = 1
	}
	header := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TNFaintText.Render(safeRepeat("─", modalW-2))

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
			valStr = valS.Render(tnTruncate(f.Value, modalW-30))
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

	footer := styles.TNGreenTxt.Render("●") + styles.TNFaintText.Render(" changed since last save")

	body := header + "\n" + sep + "\n" + strings.Join(rows, "\n") + "\n" + sep + "\n" + footer
	modal := styles.TNModal.Width(modalW).Render(body)

	// Center the modal.
	topPad := (h - strings.Count(modal, "\n") - 2) / 2
	if topPad < 0 {
		topPad = 0
	}
	var sb strings.Builder
	for range topPad {
		sb.WriteString("\n")
	}
	leftPad := (w - lipgloss.Width(modal)) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := safeRepeat(" ", leftPad)
	for _, line := range strings.Split(modal, "\n") {
		sb.WriteString(pad + line + "\n")
	}
	return sb.String()
}

// --- Doctor modal ---

func (m DashboardModel) viewDoctor() string {
	h := m.contentHeight()
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

	topPad := (h - strings.Count(modal, "\n") - 2) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (w - lipgloss.Width(modal)) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	var sb strings.Builder
	for range topPad {
		sb.WriteString("\n")
	}
	pad := safeRepeat(" ", leftPad)
	for _, line := range strings.Split(modal, "\n") {
		sb.WriteString(pad + line + "\n")
	}
	return sb.String()
}

// --- Install screen ---

func (m DashboardModel) viewInstall() string {
	h := m.contentHeight()
	w := m.width

	panelW := w - 4
	title := styles.TNBoldBlue.Render("◆") + "  " + styles.TNTextBold.Render("Install new instance")
	hint := styles.TNFaintText.Render("preflight → configure → install")
	gap := panelW - lipgloss.Width(title) - lipgloss.Width(hint) - 6
	titleRow := title + safeRepeat(" ", max(1, gap)) + hint
	sep := styles.TNFaintText.Render(safeRepeat("─", panelW-4))

	innerW := panelW - 4
	content := titleRow + "\n" + sep + "\n" + m.installModel.TNView(innerW, h-4)
	return prefixLines(styles.TNPanel.Width(panelW).Height(h).Render(content), "  ")
}

// --- Update screen (Detail layout with log panel replaced) ---

func (m DashboardModel) viewUpdate() string {
	inst := m.CurrentInstance()
	if inst == nil {
		return "\n  " + styles.TNDimText.Render("No instance selected.")
	}
	h := m.contentHeight()
	w := m.width

	var sb strings.Builder

	// Instance title row (same as viewDetail).
	dot := styles.TNStatusDot(inst.Status)
	statusLabel := lipgloss.NewStyle().Foreground(styles.TNStatusColor(inst.Status)).Render(inst.Status)
	imgShort := tnTruncate(inst.Info.Config.Image, 50)
	sb.WriteString("\n  " + styles.TNTextBold.Render("▸ "+inst.Info.Name) + "  " + dot + " " + statusLabel + "  " + styles.TNFaintText.Render(imgShort) + "\n\n")

	// Two-panel row: metadata + resources (same as viewDetail).
	panelH := 10
	halfW := (w - 6) / 2
	meta := m.renderMetaPanel(inst, halfW, panelH)
	res := m.renderResourcePanel(inst, halfW, panelH)

	metaLines := strings.Split(meta, "\n")
	resLines := strings.Split(res, "\n")
	for len(metaLines) < panelH+2 {
		metaLines = append(metaLines, safeRepeat(" ", halfW+2))
	}
	for len(resLines) < panelH+2 {
		resLines = append(resLines, safeRepeat(" ", halfW+2))
	}
	maxL := len(metaLines)
	if len(resLines) > maxL {
		maxL = len(resLines)
	}
	for i := range maxL {
		ml, rl := "", ""
		if i < len(metaLines) {
			ml = metaLines[i]
		}
		if i < len(resLines) {
			rl = resLines[i]
		}
		sb.WriteString("  " + ml + "  " + rl + "\n")
	}

	// Update progress panel replaces the log panel.
	updateH := h - panelH - 5
	if updateH < 4 {
		updateH = 4
	}
	panelW := w - 4

	title := styles.TNBlueTxt.Render("◆") + "  " + styles.TNTextBold.Render("Update  ") + styles.TNFaintText.Render(inst.Info.Name)
	hint := styles.TNFaintText.Render("press ") + styles.TNKeyChip.Render("any key") + styles.TNFaintText.Render(" when done")
	gap := panelW - lipgloss.Width(title) - lipgloss.Width(hint) - 6
	titleRow := title + safeRepeat(" ", max(1, gap)) + hint

	updateContent := titleRow + "\n" + m.updateModel.TNView(panelW-4)
	updatePanel := styles.TNPanel.Width(panelW).Height(updateH).Render(updateContent)
	sb.WriteString(prefixLines(updatePanel, "  "))

	return padToHeight(sb.String(), h)
}

// --- Helpers ---

// prefixLines adds prefix to every line of a rendered multi-line block.
// This is necessary because "prefix + panel" only indents the first line.
func prefixLines(block, prefix string) string {
	block = strings.TrimSuffix(block, "\n")
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
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

func parsePct(s string) float64 {
	s = strings.TrimSuffix(s, "%")
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f / 100.0
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
