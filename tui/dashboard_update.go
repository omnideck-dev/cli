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
)

// Update dispatches messages to the appropriate screen handler.
func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// WizardExitMsg is handled at the dashboard level regardless of screen.
	if _, ok := msg.(WizardExitMsg); ok {
		m.screen = ScreenDashboard
		return m, reloadInstancesCmd()
	}

	// Global handlers first.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.screen == ScreenInstall {
			m.installModel.HandleWindowSize(msg)
		}
		if m.screen == ScreenUpdate {
			m.updateModel.HandleWindowSize(msg)
		}
		return m, nil

	case clockTickMsg:
		m.clock = time.Time(msg).Format("15:04:05")
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return clockTickMsg(t) })

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
		existing := map[string]InstanceState{}
		for _, inst := range m.instances {
			if inst.Info.Config == nil {
				continue
			}
			existing[inst.Info.Config.ContainerName] = inst
		}
		newStates := make([]InstanceState, 0, len(msg))
		for _, info := range msg {
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
		m.doctorResults = []CheckResult(msg)
		m.doctorDone = true
		return m, nil

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

	case cfgApplyDoneMsg:
		m.screen = ScreenDashboard
		m.cfgFields = nil
		if msg.err != nil {
			m.toast = "Apply failed: " + msg.err.Error()
			return m, clearToastCmd()
		}
		m.toast = "Config applied — " + msg.newName + " restarted"
		// Poll the specific instance with its new container name (already updated
		// in memory by saveCfgFields). A full reloadInstancesCmd is not needed
		// and can cause duplicate entries when instance metadata is in flux.
		return m, tea.Batch(m.pollStats(msg.idx), clearToastCmd())

	case containerToggleDoneMsg:
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
			action := "Stopped"
			if msg.status == "running" {
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
	case ScreenInstall:
		newModel, cmd := m.installModel.Update(msg)
		m.installModel = newModel.(InstallModel)
		if m.installModel.eng != nil {
			m.eng = m.installModel.eng
		}
		return m, cmd
	case ScreenUpdate:
		newModel, cmd := m.updateModel.Update(msg)
		m.updateModel = newModel.(UpdateModel)
		return m, cmd
	case ScreenDashboard:
		return m.updateDashboard(msg)
	case ScreenLogs:
		return m.updateLogs(msg)
	case ScreenConfig:
		return m.updateConfig(msg)
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
			m.screen = ScreenConfig
			m.buildCfgFields()
		}

	case "n":
		return m.startEmbeddedInstall()

	case "d":
		m.screen = ScreenDoctor
		m.doctorDone = false
		m.doctorResults = nil
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

func (m DashboardModel) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.cfgEditing {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			m.cfgFields[m.cfgFocus].Value = m.cfgBuf
			m.cfgFields[m.cfgFocus].Changed = (m.cfgBuf != m.cfgFields[m.cfgFocus].Orig)
			m.cfgEditing = false
			m.cfgBuf = ""
		case tea.KeyEsc:
			m.cfgEditing = false
			m.cfgBuf = ""
		case tea.KeyBackspace:
			if len(m.cfgBuf) > 0 {
				m.cfgBuf = m.cfgBuf[:len(m.cfgBuf)-1]
			}
		default:
			if key.Type == tea.KeyRunes {
				m.cfgBuf += string(key.Runes)
			}
		}
		return m, nil
	}

	switch key.String() {
	case "esc", "q", "backspace":
		m.screen = ScreenDashboard
		m.cfgFields = nil

	case "ctrl+s":
		inst := m.CurrentInstance()
		if inst == nil {
			break
		}
		// Capture old name before saveCfgFields mutates the in-memory config.
		oldName := inst.Info.Config.ContainerName
		oldPort := inst.Info.Config.WebUIPortOrDefault()
		needsRestart := false
		for _, f := range m.cfgFields {
			if !f.Changed {
				continue
			}
			switch f.Key {
			case "container_name", "home_volume", "state_volume", "memory", "shm_size", "web_ui_port", "image":
				needsRestart = true
			}
		}
		if err := m.validateCfgFields(); err != nil {
			m.toast = "Cannot save: " + err.Error()
			return m, clearToastCmd()
		}
		if err := m.saveCfgFields(); err != nil {
			m.toast = "Save failed: " + err.Error()
			return m, clearToastCmd()
		}
		for i := range m.cfgFields {
			m.cfgFields[i].Orig = m.cfgFields[i].Value
			m.cfgFields[i].Changed = false
		}
		if needsRestart {
			m.toast = "Applying config…"
			return m, applyConfigCmd(oldName, oldPort, inst.Info.Config, m.eng, m.selected)
		}
		m.toast = "Config saved"
		return m, clearToastCmd()

	case "down", "tab":
		if m.cfgFocus < len(m.cfgFields)-1 {
			m.cfgFocus++
		}

	case "up", "shift+tab":
		if m.cfgFocus > 0 {
			m.cfgFocus--
		}

	case "enter":
		if len(m.cfgFields) > 0 {
			m.cfgBuf = m.cfgFields[m.cfgFocus].Value
			m.cfgEditing = true
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
		if status == "running" {
			_ = eng.StopContainer(name)
		} else {
			_ = eng.StartContainer(name)
		}
		return containerToggleDoneMsg(fetchStats(eng, name, idx).(instanceStatsMsg))
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
