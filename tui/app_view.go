package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/omnideck-dev/cli/styles"
)

// View dispatches rendering to the active screen.
func (m AppModel) View() string {
	body := m.renderBody()
	if m.dialog != nil {
		body = renderDialogArea(*m.dialog, m.width, m.contentHeight())
	}
	return m.renderHeader() + body + "\n" + m.renderFooter()
}

// --- Layout constants ---

const (
	headerLines = 1
	footerLines = 1
)

func (m AppModel) contentHeight() int {
	h := m.height - headerLines - footerLines
	if h < 0 {
		return 0
	}
	return h
}

// --- Header ---

func (m AppModel) renderHeader() string {
	logo := styles.BrandMark(styles.SignalSurface) + " " + styles.TUIPrimaryBold.Render("omnideck")
	sep := styles.TUISubtleText.Render(" │ ")
	breadcrumb := styles.TUISecondaryText.Render(m.breadcrumb())
	left := logo + sep + breadcrumb

	// Setup already explains the current task in its breadcrumb and screen. A
	// live instance summary here would remain unknown while setup owns runtime
	// detection, so leave the right side quiet on that screen.
	right := ""
	if m.router.Current() != RouteSetup {
		label, tone := summarizeInstances(m.instances)
		dot := styles.TUISubtleText.Render("●")
		switch tone {
		case headerHealthy:
			dot = styles.TUISuccessText.Render("●")
		case headerAttention:
			dot = styles.TUIWarningText.Render("●")
		case headerError:
			dot = styles.TUIDangerText.Render("●")
		}
		right = dot + styles.TUISecondaryText.Render(" "+label)
	}

	innerWidth := max(1, m.width-styles.TUIHeaderBar.GetHorizontalFrameSize())
	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + safeRepeat(" ", gap) + right
	line = ansi.Truncate(line, innerWidth, "")
	line += safeRepeat(" ", innerWidth-lipgloss.Width(line))
	return styles.RenderOnBackground(styles.TUIHeaderBar.Render(line), styles.SignalSurface) + "\n"
}

type headerStatusTone int

const (
	headerNeutral headerStatusTone = iota
	headerHealthy
	headerAttention
	headerError
)

func summarizeInstances(instances []InstanceState) (string, headerStatusTone) {
	if len(instances) == 0 {
		return "No instances yet", headerNeutral
	}
	if len(instances) == 1 {
		switch instances[0].Status {
		case "running":
			return "Omnideck is running", headerHealthy
		case "paused":
			return "Omnideck is paused", headerAttention
		case "restarting":
			return "Omnideck is restarting", headerAttention
		case "dead":
			return "Omnideck needs attention", headerError
		case "", "unknown":
			return "Checking Omnideck…", headerNeutral
		default:
			return "Omnideck is stopped", headerAttention
		}
	}

	running := 0
	unknown := 0
	for _, inst := range instances {
		switch inst.Status {
		case "running":
			running++
		case "", "unknown":
			unknown++
		}
	}
	label := fmt.Sprintf("%d of %d running", running, len(instances))
	if unknown == len(instances) {
		return "Checking decks…", headerNeutral
	}
	if running == len(instances) {
		return label, headerHealthy
	}
	return label, headerAttention
}

func (m AppModel) breadcrumb() string {
	switch m.router.Current() {
	case RouteDashboard:
		return "Decks"
	case RouteLogs:
		if inst := m.CurrentInstance(); inst != nil {
			return "Decks › " + inst.Info.Name + " › Logs"
		}
		return "Logs"
	case RouteSettings:
		if inst := m.CurrentInstance(); inst != nil {
			return "Decks › " + inst.Info.Name + " › Settings"
		}
		return "Settings"
	case RouteDoctor:
		return "Doctor"
	case RouteSetup:
		switch m.setupModel.Stage {
		case SetupStageWelcome:
			return "Setup"
		case SetupStageQuickCheck:
			return "Setup · Quick check"
		case SetupStageRuntime:
			return "Setup · Container setup"
		case SetupStageSettings:
			return "Setup · Settings"
		case SetupStageReview:
			return "Setup · Review"
		case SetupStageApplying:
			return "Setup · Working"
		case SetupStageComplete:
			return "Setup · Ready"
		case SetupStageFailed:
			return "Setup · Needs attention"
		}
		return "Setup"
	case RouteMaintenance:
		title := m.maintenanceModel.title()
		if inst := m.CurrentInstance(); inst != nil {
			return "Decks › " + inst.Info.Name + " › " + title
		}
		return title
	case RouteRemoval:
		if inst := m.CurrentInstance(); inst != nil {
			return "Decks › " + inst.Info.Name + " › Remove"
		}
		return "Remove instance"
	}
	return ""
}

// --- Footer ---

func (m AppModel) renderFooter() string {
	hints := m.footerHints()
	right := styles.TUISubtleText.Render("omnideck tui")
	innerWidth := max(1, m.width-styles.TUIFooterBar.GetHorizontalFrameSize())
	if lipgloss.Width(hints)+lipgloss.Width(right)+1 > innerWidth {
		right = ""
	}
	hints = ansi.Truncate(hints, max(1, innerWidth-lipgloss.Width(right)-1), "")
	gap := innerWidth - lipgloss.Width(hints) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := hints + safeRepeat(" ", gap) + right
	line = ansi.Truncate(line, innerWidth, "")
	line += safeRepeat(" ", innerWidth-lipgloss.Width(line))
	return styles.RenderOnBackground(styles.TUIFooterBar.Render(line), styles.SignalSurface)
}

func (m AppModel) footerHints() string {
	if m.dialog != nil {
		return keyHints([][2]string{{"←/→", "choose"}, {"enter", "confirm"}, {"esc", "keep editing"}})
	}
	switch m.router.Current() {
	case RouteDashboard:
		if m.chipFocus >= 0 {
			return keyHints([][2]string{
				{"tab", "cycle"}, {"enter", "activate"}, {"esc", "deselect"},
			})
		}
		if m.isExpanded() {
			return keyHints([][2]string{
				{"↑↓", "move"}, {"pg↑↓", "scroll"}, {"tab", "actions"}, {"x", "remove"}, {"esc", "collapse"},
			})
		}
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "open"}, {"n", "new"}, {"x", "remove"}, {"d", "doctor"}, {"q", "quit"},
		})
	case RouteLogs:
		if m.logSearchMode {
			return keyHints([][2]string{{"type", "filter"}, {"enter", "done"}, {"esc", "clear"}})
		}
		if m.logSearchQuery != "" {
			return keyHints([][2]string{
				{"↑↓", "scroll"}, {"esc", "clear filter"}, {"/", "edit filter"}, {"y", "copy"}, {"r", "refresh"},
			})
		}
		return keyHints([][2]string{
			{"↑↓", "scroll"}, {"pg↑↓", "page"}, {"esc", "back"}, {"/", "search"}, {"y", "copy"}, {"r", "refresh"},
		})
	case RouteSettings:
		if m.settingsStage == settingsStageApplying {
			return keyHints([][2]string{{"working", "please wait"}})
		}
		if m.settingEditing {
			return keyHints([][2]string{{"enter", "confirm"}, {"esc", "cancel"}})
		}
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "edit"}, {"ctrl+s", "apply"}, {"esc", "back"},
		})
	case RouteDoctor:
		if m.doctorStage == doctorStageActing {
			return keyHints([][2]string{{"working", "please wait"}})
		}
		hints := [][2]string{{"esc", "back"}, {"r", "check again"}}
		if m.doctorStage == doctorStageResults && m.doctorFocus >= 0 && m.doctorFocus < len(m.doctorResults) {
			hints = append([][2]string{{"↑↓", "choose action"}, {"enter", m.doctorResults[m.doctorFocus].ActionLabel}}, hints...)
		}
		return keyHints(hints)
	case RouteSetup:
		switch m.setupModel.Stage {
		case SetupStageWelcome:
			return keyHints([][2]string{{"enter", "set up omnideck"}, {"q", "cancel"}})
		case SetupStageRuntime:
			if m.setupModel.runtimeSetupStage == runtimeSetupRestart {
				return keyHints([][2]string{{"enter", "restart now"}, {"l", "restart later"}})
			}
			return keyHints([][2]string{{"working", "please wait"}})
		case SetupStageSettings:
			if !m.setupModel.settingsAdvanced {
				return keyHints([][2]string{{"enter", "use recommended"}, {"c", "customize"}, {"esc", "cancel"}})
			}
			return keyHints([][2]string{{"tab", "next"}, {"shift+tab", "back"}, {"esc", "recommended settings"}})
		case SetupStageReview:
			return keyHints([][2]string{{"enter", "start setup"}, {"esc", "back"}, {"q", "cancel"}})
		case SetupStageComplete:
			return keyHints([][2]string{{"any key", "return"}})
		case SetupStageFailed:
			return keyHints([][2]string{{"r", "try again"}, {"d", "details for support"}, {"esc", "return"}})
		}
		return ""
	case RouteMaintenance:
		switch m.maintenanceModel.Stage {
		case MaintenanceStageReview:
			return keyHints([][2]string{{"enter", m.maintenanceModel.actionVerb()}, {"esc", "go back"}})
		case MaintenanceStageApplying:
			return keyHints([][2]string{{"working", "please wait"}})
		case MaintenanceStageComplete:
			return keyHints([][2]string{{"any key", "return"}})
		case MaintenanceStageFailed:
			return keyHints([][2]string{{"r", "try again"}, {"esc", "return"}})
		}
		return ""
	case RouteRemoval:
		switch m.removalModel.Stage {
		case RemovalStageDataChoice, RemovalStageBackupChoice:
			return keyHints([][2]string{{"↑↓", "choose"}, {"enter", "continue"}, {"esc", "go back"}})
		case RemovalStageReview:
			return keyHints([][2]string{{"enter", "remove instance"}, {"esc", "go back"}})
		case RemovalStageDeleteConfirm:
			return keyHints([][2]string{{"type", "instance name"}, {"enter", "confirm deletion"}, {"esc", "go back"}})
		case RemovalStageApplying:
			return keyHints([][2]string{{"working", "please wait"}})
		case RemovalStageComplete:
			return keyHints([][2]string{{"any key", "return to instances"}})
		case RemovalStageFailed:
			return keyHints([][2]string{{"r", "try again"}, {"esc", "return to instances"}})
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
		sb.WriteString(styles.TUIKeyChip.Render(p[0]))
		sb.WriteString(" ")
		sb.WriteString(styles.TUISecondaryText.Render(p[1]))
	}
	return sb.String()
}

// --- Body dispatcher ---

func (m AppModel) renderBody() string {
	switch m.router.Current() {
	case RouteDashboard:
		return m.viewDashboard()
	case RouteLogs:
		return m.viewLogs()
	case RouteSettings:
		return m.viewSettings()
	case RouteDoctor:
		return m.viewDoctor()
	case RouteSetup:
		return m.viewSetup()
	case RouteMaintenance:
		return m.viewMaintenance()
	case RouteRemoval:
		return m.viewRemoval()
	}
	return ""
}

// renderScreen fills the application content area. Substantial journeys use a
// full screen; short blocking decisions use the separate dialog layer.
func (m AppModel) renderScreen(body string) string {
	style := lipgloss.NewStyle().Padding(1, 2)
	w := max(1, m.width)
	h := max(1, m.contentHeight())
	return styles.RenderOnBackground(
		style.Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(body),
		styles.SignalCanvas,
	)
}

// --- Helpers ---

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

func dashOr(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func healthStyle(health string) lipgloss.Style {
	switch health {
	case "healthy":
		return styles.TUISuccessText
	case "degraded", "unhealthy":
		return styles.TUIWarningText
	default:
		return styles.TUISecondaryText
	}
}
