package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

// Update dispatches messages to the appropriate screen handler.
func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// WorkflowExitMsg is handled at the dashboard level regardless of screen.
	if _, ok := msg.(WorkflowExitMsg); ok {
		m.screen = ScreenDashboard
		return m, reloadInstancesCmd()
	}

	// Global handlers first.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.screen == ScreenSetup {
			m.setupModel.HandleWindowSize(msg)
		}
		if m.screen == ScreenMaintenance {
			m.maintenanceModel.HandleWindowSize(msg)
		}
		return m, nil

	case statsTickMsg:
		var cmds []tea.Cmd
		for i := range m.instances {
			cmds = append(cmds, m.pollStats(i))
		}
		return m, tea.Batch(append(cmds,
			tea.Tick(time.Second, func(t time.Time) tea.Msg { return statsTickMsg(t) }),
		)...)

	case instanceStatsMsg:
		if msg.idx >= 0 && msg.idx < len(m.instances) {
			inst := &m.instances[msg.idx]
			inst.Status = msg.status
			inst.CPU = msg.cpu
			inst.CPUPct = msg.cpuPct
			inst.RAM = msg.ram
			inst.RAMTotal = msg.ramTotal
			inst.RAMPct = msg.ramPct
			if msg.uptime != "" {
				inst.Uptime = msg.uptime
			}
			if msg.restarts != "" {
				inst.Restarts = msg.restarts
			}
			if msg.created != "" {
				inst.Created = msg.created
			}
			if msg.health != "" {
				inst.Health = msg.health
			}
			inst.CPUHistory = pushHistory(inst.CPUHistory, msg.cpuPct)
			inst.RAMHistory = pushHistory(inst.RAMHistory, msg.ramPct)
		}
		return m, nil

	case instanceLogsMsg:
		if msg.idx >= 0 && msg.idx < len(m.instances) {
			m.instances[msg.idx].Logs = msg.lines
		}
		if m.screen == ScreenLogs {
			m.logScroll = 0
		}
		return m, nil

	case instancesRefreshedMsg:
		if msg.err != nil {
			m.toast = "Could not refresh saved installations: " + msg.err.Error()
			return m, clearToastCmd()
		}
		existing := map[string]InstanceState{}
		for _, inst := range m.instances {
			if inst.Info.Config == nil {
				continue
			}
			existing[inst.Info.Config.ContainerName] = inst
		}
		newStates := make([]InstanceState, 0, len(msg.instances))
		for _, info := range msg.instances {
			if info.Config == nil {
				continue
			}
			if prev, ok := existing[info.Config.ContainerName]; ok {
				prev.Info = info
				newStates = append(newStates, prev)
			} else {
				newStates = append(newStates, InstanceState{Info: info, Status: "unknown"})
			}
		}
		m.instances = newStates
		if m.selected >= len(m.instances) {
			m.selected = max(0, len(m.instances)-1)
		}
		var cmds []tea.Cmd
		for i := range m.instances {
			cmds = append(cmds, m.pollStats(i))
		}
		return m, tea.Batch(cmds...)

	case doctorResultsMsg:
		m.doctorResults = msg.results
		if msg.eng != nil {
			m.eng = msg.eng
		}
		m.doctorStage = doctorStageResults
		m.doctorMessage = ""
		m.doctorFocus = firstDoctorAction(m.doctorResults)
		return m, nil

	case doctorActionDoneMsg:
		if msg.err != nil {
			m.doctorStage = doctorStageResults
			m.doctorMessage = "Omnideck could not finish that action: " + msg.err.Error()
			return m, nil
		}
		m.doctorStage = doctorStageChecking
		m.doctorMessage = "Action finished. Checking again…"
		return m, tea.Batch(m.doctorSpinner.Tick, m.runDoctorCmd())

	case logCopyResultMsg:
		if msg.err == nil {
			m.logCopied = true
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return logCopyClearMsg{} })
		}
		return m, nil

	case logCopyClearMsg:
		m.logCopied = false
		return m, nil

	case toastClearMsg:
		m.toast = ""
		return m, nil

	case settingsApplyDoneMsg:
		m.settingsStage = settingsStageEditing
		if msg.err != nil {
			m.screen = ScreenSettings
			m.toast = ""
			m.settingsMessage = "The new settings could not be applied: " + msg.err.Error()
			return m, nil
		}
		m.screen = ScreenDashboard
		m.settingFields = nil
		if msg.idx >= 0 && msg.idx < len(m.instances) && msg.cfg != nil {
			*m.instances[msg.idx].Info.Config = *msg.cfg
		}
		name := "Omnideck"
		if msg.cfg != nil {
			name = msg.cfg.ContainerName
		}
		m.toast = "Settings applied — " + name + " restarted"
		return m, tea.Batch(m.pollStats(msg.idx), clearToastCmd())

	case containerToggleDoneMsg:
		if msg.err != nil {
			m.toast = "Could not " + msg.action + " Omnideck: " + msg.err.Error()
			return m, clearToastCmd()
		}
		stats := msg.stats
		if stats.idx >= 0 && stats.idx < len(m.instances) {
			inst := &m.instances[stats.idx]
			inst.Status = stats.status
			inst.CPU = stats.cpu
			inst.CPUPct = stats.cpuPct
			inst.RAM = stats.ram
			inst.RAMTotal = stats.ramTotal
			inst.RAMPct = stats.ramPct
			if stats.uptime != "" {
				inst.Uptime = stats.uptime
			}
			if stats.restarts != "" {
				inst.Restarts = stats.restarts
			}
			if stats.created != "" {
				inst.Created = stats.created
			}
			if stats.health != "" {
				inst.Health = stats.health
			}
			inst.CPUHistory = pushHistory(inst.CPUHistory, stats.cpuPct)
			inst.RAMHistory = pushHistory(inst.RAMHistory, stats.ramPct)
			action := "Stopped"
			if stats.status == "running" {
				action = "Started"
			}
			m.toast = action + " " + inst.Info.Name
		}
		return m, clearToastCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.doctorSpinner, cmd = m.doctorSpinner.Update(msg)
		return m, cmd
	}

	// Per-screen handlers.
	switch m.screen {
	case ScreenSetup:
		newModel, cmd := m.setupModel.Update(msg)
		m.setupModel = newModel.(SetupModel)
		if m.setupModel.eng != nil {
			m.eng = m.setupModel.eng
		}
		return m, cmd
	case ScreenMaintenance:
		newModel, cmd := m.maintenanceModel.Update(msg)
		m.maintenanceModel = newModel.(MaintenanceModel)
		return m, cmd
	case ScreenDashboard:
		return m.updateDashboard(msg)
	case ScreenLogs:
		return m.updateLogs(msg)
	case ScreenSettings:
		return m.updateSettings(msg)
	case ScreenDoctor:
		return m.updateDoctor(msg)
	}
	return m, nil
}

func (m DashboardModel) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	chipFocused := m.chipFocus >= 0

	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "down", "j":
		if m.selected < len(m.instances)-1 {
			m.selected++
			m.chipFocus = -1
		}
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.chipFocus = -1
		}

	case "enter":
		if len(m.instances) == 0 {
			break
		}
		if chipFocused {
			return m.execChip()
		}
		name := m.instances[m.selected].Info.Name
		m.expanded[name] = !m.expanded[name]
		if m.expanded[name] {
			return m, m.pollLogs(m.selected)
		}
		m.chipFocus = -1

	case "tab":
		if len(m.instances) > 0 && m.isExpanded() {
			if m.chipFocus < 0 {
				m.chipFocus = 0
			} else {
				m.chipFocus = (m.chipFocus + 1) % 4
			}
		} else if len(m.instances) > 0 {
			if m.selected < len(m.instances)-1 {
				m.selected++
			}
		}

	case "shift+tab":
		if len(m.instances) > 0 && m.isExpanded() {
			if m.chipFocus <= 0 {
				m.chipFocus = 3
			} else {
				m.chipFocus--
			}
		} else if len(m.instances) > 0 {
			if m.selected > 0 {
				m.selected--
			}
		}

	case "right":
		if len(m.instances) > 0 && m.isExpanded() {
			if m.chipFocus < 0 {
				m.chipFocus = 0
			} else {
				m.chipFocus = (m.chipFocus + 1) % 4
			}
		}
	case "left":
		if len(m.instances) > 0 && m.isExpanded() {
			if m.chipFocus < 0 {
				m.chipFocus = 3
			} else {
				m.chipFocus = (m.chipFocus + 3) % 4
			}
		}

	case "esc":
		if chipFocused {
			m.chipFocus = -1
		} else if m.isExpanded() {
			m.expanded[m.instances[m.selected].Info.Name] = false
		}

	case "s":
		return m.doToggleContainer()

	case "l":
		if len(m.instances) > 0 {
			m.screen = ScreenLogs
			m.logScroll = 0
			return m, m.pollLogs(m.selected)
		}

	case "c":
		if len(m.instances) > 0 {
			m.screen = ScreenSettings
			m.buildSettingFields()
		}

	case "n":
		return m.startEmbeddedSetup()

	case "d":
		m.screen = ScreenDoctor
		m.doctorStage = doctorStageChecking
		m.doctorResults = nil
		m.doctorFocus = -1
		m.doctorMessage = ""
		return m, tea.Batch(m.doctorSpinner.Tick, m.runDoctorCmd())

	case "r":
		var cmds []tea.Cmd
		for i := range m.instances {
			cmds = append(cmds, m.pollStats(i))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// isExpanded returns true if the currently selected card is expanded.
func (m DashboardModel) isExpanded() bool {
	if m.selected < 0 || m.selected >= len(m.instances) {
		return false
	}
	return m.expanded[m.instances[m.selected].Info.Name]
}

// doToggleContainer initiates a stop/start for the selected instance with a toast.
func (m DashboardModel) doToggleContainer() (DashboardModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil {
		return m, nil
	}
	action := "Stopping"
	if inst.Status != "running" {
		action = "Starting"
	}
	m.toast = action + " " + inst.Info.Name + "…"
	return m, m.toggleContainer()
}

// execChip activates the currently focused action chip.
func (m DashboardModel) execChip() (DashboardModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil {
		return m, nil
	}
	switch m.chipFocus {
	case 0: // Open UI
		port := inst.Info.Config.WebUIPortOrDefault()
		m.toast = "Opened " + inst.Info.Name + " in browser ↗"
		return m, tea.Batch(openBrowserCmd("http://localhost:"+port), clearToastCmd())
	case 1: // Logs
		m.screen = ScreenLogs
		m.logScroll = 0
		m.chipFocus = -1
		return m, m.pollLogs(m.selected)
	case 2: // Update
		m.chipFocus = -1
		return m.startEmbeddedUpdate()
	case 3: // Stop/Start
		m.chipFocus = -1
		return m.doToggleContainer()
	}
	return m, nil
}

func (m DashboardModel) updateLogs(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Search input mode: characters feed the live filter.
	if m.logSearchMode {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			m.logSearchMode = false
		case tea.KeyEsc:
			m.logSearchQuery = ""
			m.logSearchMode = false
			m.logScroll = 0
		case tea.KeyBackspace:
			if len(m.logSearchQuery) > 0 {
				runes := []rune(m.logSearchQuery)
				m.logSearchQuery = string(runes[:len(runes)-1])
				m.logScroll = 0
			}
		default:
			if key.Type == tea.KeyRunes {
				m.logSearchQuery += string(key.Runes)
				m.logScroll = 0
			}
		}
		return m, nil
	}

	// Normal mode.
	filtered := m.filteredLogs()
	totalLines := len(filtered)
	visibleLines := m.logModalVisibleLines()

	switch key.String() {
	case "esc", "backspace":
		if m.logSearchQuery != "" {
			m.logSearchQuery = ""
			m.logScroll = 0
		} else {
			m.screen = ScreenDashboard
		}
	case "q":
		if m.logSearchQuery == "" {
			m.screen = ScreenDashboard
		}

	case "down":
		if maxS := totalLines - visibleLines; m.logScroll < maxS {
			m.logScroll++
		}
	case "up":
		if m.logScroll > 0 {
			m.logScroll--
		}
	case "pgdown", "ctrl+f", "ctrl+v":
		maxS := totalLines - visibleLines
		if maxS < 0 {
			maxS = 0
		}
		m.logScroll = min(m.logScroll+visibleLines, maxS)
	case "pgup", "ctrl+b", "alt+v":
		m.logScroll = max(m.logScroll-visibleLines, 0)
	case "home", "g":
		m.logScroll = 0
	case "end", "G":
		if maxS := totalLines - visibleLines; maxS > 0 {
			m.logScroll = maxS
		}

	case "r":
		return m, m.pollLogs(m.selected)
	case "/":
		m.logSearchMode = true
	case "y":
		return m, m.copyLogsCmd()
	}
	return m, nil
}

// logModalVisibleLines calculates visible log lines in the logs modal.
// Must stay in sync with viewLogs() layout.
func (m DashboardModel) logModalVisibleLines() int {
	modalH := m.contentHeight() - 6
	if modalH < 4 {
		modalH = 4
	}
	// subtract header line + separator
	visible := modalH - 2
	if m.logSearchMode || m.logSearchQuery != "" {
		visible -= 2 // separator + search bar
	}
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m DashboardModel) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settingsStage == settingsStageApplying {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.settingEditing {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			m.settingFields[m.settingFocus].Value = m.settingBuffer
			m.settingFields[m.settingFocus].Changed = (m.settingBuffer != m.settingFields[m.settingFocus].Orig)
			m.settingEditing = false
			m.settingBuffer = ""
		case tea.KeyEsc:
			m.settingEditing = false
			m.settingBuffer = ""
		case tea.KeyBackspace:
			if len(m.settingBuffer) > 0 {
				m.settingBuffer = m.settingBuffer[:len(m.settingBuffer)-1]
			}
		default:
			if key.Type == tea.KeyRunes {
				m.settingBuffer += string(key.Runes)
			}
		}
		return m, nil
	}

	switch key.String() {
	case "esc", "q", "backspace":
		m.screen = ScreenDashboard
		m.settingFields = nil

	case "ctrl+s":
		inst := m.CurrentInstance()
		if inst == nil {
			break
		}
		needsRestart := false
		for _, f := range m.settingFields {
			if !f.Changed {
				continue
			}
			switch f.Key {
			case "home_volume", "state_volume", "memory", "shm_size", "web_ui_port", "image":
				needsRestart = true
			}
		}
		if err := m.validateSettingFields(); err != nil {
			m.settingsMessage = "Cannot apply these settings: " + err.Error()
			return m, nil
		}
		m.settingsMessage = ""
		candidate := m.configFromSettingFields()
		if needsRestart {
			m.toast = "Applying settings…"
			m.settingsStage = settingsStageApplying
			return m, applySettingsCmd(inst.Info.Config, candidate, inst.Info.Path, m.eng, m.selected)
		}
		m.toast = "No settings changed"
		return m, clearToastCmd()

	case "down", "tab":
		if m.settingFocus < len(m.settingFields)-1 {
			m.settingFocus++
		}

	case "up", "shift+tab":
		if m.settingFocus > 0 {
			m.settingFocus--
		}

	case "enter":
		if len(m.settingFields) > 0 {
			m.settingBuffer = m.settingFields[m.settingFocus].Value
			m.settingEditing = true
		}
	}
	return m, nil
}

func (m DashboardModel) updateDoctor(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q", "backspace":
		m.screen = ScreenDashboard
	case "r":
		m.doctorStage = doctorStageChecking
		m.doctorMessage = "Checking again…"
		return m, tea.Batch(m.doctorSpinner.Tick, m.runDoctorCmd())
	case "up", "shift+tab":
		m.doctorFocus = nextDoctorAction(m.doctorResults, m.doctorFocus, -1)
	case "down", "tab":
		m.doctorFocus = nextDoctorAction(m.doctorResults, m.doctorFocus, 1)
	case "enter", " ":
		if m.doctorStage != doctorStageResults || m.doctorFocus < 0 || m.doctorFocus >= len(m.doctorResults) {
			return m, nil
		}
		return m.runDoctorAction(m.doctorResults[m.doctorFocus])
	}
	return m, nil
}

func firstDoctorAction(results []CheckResult) int {
	for i, result := range results {
		if result.Action != DoctorActionNone {
			return i
		}
	}
	return -1
}

func nextDoctorAction(results []CheckResult, current, direction int) int {
	if len(results) == 0 {
		return -1
	}
	if direction == 0 {
		direction = 1
	}
	for step := 1; step <= len(results); step++ {
		candidate := (current + direction*step) % len(results)
		if candidate < 0 {
			candidate += len(results)
		}
		if results[candidate].Action != DoctorActionNone {
			return candidate
		}
	}
	return -1
}

func (m DashboardModel) runDoctorAction(result CheckResult) (tea.Model, tea.Cmd) {
	switch result.Action {
	case DoctorActionRuntimeSetup:
		if len(m.instances) == 0 {
			return m.startEmbeddedSetupWithRuntime(result.ActionValue)
		}
		inst := m.CurrentInstance()
		cfg := config.DefaultConfig()
		if inst != nil && inst.Info.Config != nil {
			cfg = inst.Info.Config
		}
		instances := make([]config.InstanceInfo, len(m.instances))
		for i := range m.instances {
			instances[i] = m.instances[i].Info
		}
		im := NewSetupModel(SetupRequest{
			Initial:           cfg,
			ExistingInstances: instances,
			Mode:              SetupRuntimeRepair,
			PreferredEngine:   result.ActionValue,
			Embedded:          true,
			WindowWidth:       m.width,
			WindowHeight:      m.height,
		})
		m.setupModel = im
		m.screen = ScreenSetup
		return m, im.Init()
	case DoctorActionStartInstance:
		if m.eng == nil {
			m.doctorMessage = "The container runtime must be ready before Omnideck can start."
			return m, nil
		}
		name := result.ActionValue
		eng := m.eng
		m.doctorStage = doctorStageActing
		m.doctorMessage = "Starting Omnideck…"
		return m, func() tea.Msg {
			_, err := workflow.EnsureStarted(eng, name)
			return doctorActionDoneMsg{err: err}
		}
	case DoctorActionSetupInstance:
		return m.startEmbeddedSetup()
	case DoctorActionRepairInstance:
		return m.startEmbeddedRepair()
	}
	return m, nil
}

// toggleContainer starts or stops the selected instance, then re-polls stats.
func (m DashboardModel) toggleContainer() tea.Cmd {
	inst := m.CurrentInstance()
	if inst == nil || m.eng == nil {
		return nil
	}
	name := inst.Info.Config.ContainerName
	status := inst.Status
	eng := m.eng
	idx := m.selected
	return func() tea.Msg {
		action := "start"
		var err error
		if status == "running" {
			action = "stop"
			_, err = workflow.EnsureStopped(eng, name)
		} else {
			_, err = workflow.EnsureStarted(eng, name)
		}
		if err != nil {
			return containerToggleDoneMsg{action: action, err: err}
		}
		return containerToggleDoneMsg{stats: fetchStats(eng, name, idx).(instanceStatsMsg), action: action}
	}
}

// openBrowserCmd launches the system browser for url.
func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			if os.Getenv("WSL_DISTRO_NAME") != "" {
				cmd = exec.Command("cmd.exe", "/c", "start", "", url)
			} else {
				cmd = exec.Command("xdg-open", url)
			}
		}
		_ = cmd.Start()
		return nil
	}
}

// copyToClipboard writes text to the system clipboard via pbcopy (macOS),
// clip (Windows), or xclip/xsel (Linux).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("clipboard tool not found — install xclip or xsel")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
