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
	logo := styles.TNBoldBlue.Render("◆") + " " + styles.TNTextBold.Render("omnideck")
	sep := styles.TNFaintText.Render(" │ ")
	breadcrumb := styles.TNDimText.Render(m.breadcrumb())
	left := logo + sep + breadcrumb

	// Setup already explains the current task in its breadcrumb and screen. A
	// live instance summary here would remain unknown while setup owns runtime
	// detection, so leave the right side quiet on that screen.
	right := ""
	if m.router.Current() != RouteSetup {
		label, tone := summarizeInstances(m.instances)
		dot := styles.TNFaintText.Render("●")
		switch tone {
		case headerHealthy:
			dot = styles.TNGreenTxt.Render("●")
		case headerAttention:
			dot = styles.TNYellowTxt.Render("●")
		case headerError:
			dot = styles.TNRedTxt.Render("●")
		}
		right = dot + styles.TNDimText.Render(" "+label)
	}

	innerWidth := max(1, m.width-styles.TNHeaderBar.GetHorizontalFrameSize())
	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + safeRepeat(" ", gap) + right
	line = ansi.Truncate(line, innerWidth, "")
	line += safeRepeat(" ", innerWidth-lipgloss.Width(line))
	return styles.TNHeaderBar.Render(line) + "\n"
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
		return "Checking instances…", headerNeutral
	}
	if running == len(instances) {
		return label, headerHealthy
	}
	return label, headerAttention
}

func (m AppModel) breadcrumb() string {
	switch m.router.Current() {
	case RouteDashboard:
		return "Instances"
	case RouteLogs:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name + " › Logs"
		}
		return "Logs"
	case RouteSettings:
		if inst := m.CurrentInstance(); inst != nil {
			return "Instances › " + inst.Info.Name + " › Settings"
		}
		return "Settings"
	case RouteDoctor:
		return "Doctor"
	case RouteSetup:
		switch m.setupModel.Stage {
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
			return "Instances › " + inst.Info.Name + " › " + title
		}
		return title
	}
	return ""
}

// --- Footer ---

func (m AppModel) renderFooter() string {
	hints := m.footerHints()
	right := styles.TNFaintText.Render("omnideck tui")
	innerWidth := max(1, m.width-styles.TNFooterBar.GetHorizontalFrameSize())
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
	return styles.TNFooterBar.Render(line)
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
				{"↑↓", "move"}, {"tab", "actions"}, {"enter", "open/collapse"}, {"esc", "collapse"},
			})
		}
		return keyHints([][2]string{
			{"↑↓", "move"}, {"enter", "open"}, {"n", "new"}, {"d", "doctor"}, {"q", "quit"},
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
		case SetupStageQuickCheck:
			if m.setupModel.quickCheckReady && m.setupModel.preferredEngine == "" && (len(m.setupModel.availableEngines) > 1 || m.setupModel.setupAlternativeRuntime() != "") {
				return keyHints([][2]string{{"tab", "switch"}, {"enter", "continue"}, {"esc", "cancel"}})
			}
		case SetupStageRuntime:
			if m.setupModel.runtimeSetupStage == runtimeSetupWorking {
				return keyHints([][2]string{{"working", "please wait"}})
			}
			if len(m.setupModel.runtimePlans) == 0 {
				hints := [][2]string{{"r / enter", "check again"}}
				if m.setupModel.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
					hints = append(hints, [2]string{"esc", "back"}, [2]string{"q", "cancel"})
				} else {
					hints = append(hints, [2]string{"esc / q", "cancel"})
				}
				return keyHints(hints)
			}
			if m.setupModel.runtimeSetupStage == runtimeSetupWaiting {
				hints := [][2]string{{"enter", "check again"}, {"esc", "back"}, {"q", "cancel"}}
				if len(m.setupModel.runtimePlans) > 0 && m.setupModel.runtimePlans[m.setupModel.runtimeChoice].URL != "" {
					label := "open page again"
					if m.setupModel.runtimePlans[m.setupModel.runtimeChoice].DirectDownload {
						label = "download again"
					}
					hints = append(hints, [2]string{"o", label})
				}
				return keyHints(hints)
			}
			if m.setupModel.runtimeSetupStage == runtimeSetupReview {
				action := "start these steps"
				if len(m.setupModel.runtimePlans) > 0 && len(m.setupModel.runtimePlans[m.setupModel.runtimeChoice].Commands) == 0 {
					action = "open official page"
					if m.setupModel.runtimePlans[m.setupModel.runtimeChoice].DirectDownload {
						action = "download installer"
					}
				}
				hints := [][2]string{{"enter", action}, {"esc", "back"}}
				if m.setupModel.runtimeDetailsAvailable() {
					hints = append(hints, [2]string{"d", m.setupModel.runtimeDetailsLabel()})
				}
				hints = append(hints, [2]string{"q", "cancel"})
				return keyHints(hints)
			}
			hints := [][2]string{{"enter", "review"}}
			if len(m.setupModel.runtimePlans) > 1 {
				hints = append([][2]string{{"↑↓", "choose"}}, hints...)
			}
			if m.setupModel.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
				hints = append(hints, [2]string{"esc", "back"}, [2]string{"q", "cancel"})
			} else {
				hints = append(hints, [2]string{"esc / q", "cancel"})
			}
			if m.setupModel.runtimeDetailsAvailable() {
				hints = append(hints, [2]string{"d", m.setupModel.runtimeDetailsLabel()})
			}
			hints = append(hints, [2]string{"r", "check again"})
			return keyHints(hints)
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
	}
	return ""
}

// renderScreen fills the application content area. Substantial journeys use a
// full screen; short blocking decisions use the separate dialog layer.
func (m AppModel) renderScreen(body string) string {
	style := lipgloss.NewStyle().Background(styles.TNBgAlt).Padding(1, 2)
	w := max(1, m.width)
	h := max(1, m.contentHeight())
	return style.Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(body)
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

func padToHeight(s string, h int) string {
	lines := strings.Count(s, "\n")
	if lines >= h {
		return s
	}
	return s + strings.Repeat("\n", h-lines)
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
