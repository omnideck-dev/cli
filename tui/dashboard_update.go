package tui

import (
	"fmt"
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
		// Forward size to any active sub-model.
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
		}
		return m, nil

	case instanceLogsMsg:
		if msg.idx >= 0 && msg.idx < len(m.instances) {
			m.instances[msg.idx].Logs = msg.lines
		}
		if m.screen == ScreenLogs {
			m.logScroll = 0 // reset scroll on fresh fetch
		}
		return m, nil

	case instancesRefreshedMsg:
		// Rebuild instance list, preserving existing runtime state where possible.
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
		}
		m.detailBusy = false
		return m, nil

	case spinner.TickMsg:
		var cmd1, cmd2 tea.Cmd
		m.doctorSpinner, cmd1 = m.doctorSpinner.Update(msg)
		if m.detailBusy {
			m.detailSpinner, cmd2 = m.detailSpinner.Update(msg)
		}
		return m, tea.Batch(cmd1, cmd2)
	}

	// Per-screen handlers.
	switch m.screen {
	case ScreenInstall:
		newModel, cmd := m.installModel.Update(msg)
		m.installModel = newModel.(InstallModel)
		return m, cmd
	case ScreenUpdate:
		newModel, cmd := m.updateModel.Update(msg)
		m.updateModel = newModel.(UpdateModel)
		return m, cmd
	case ScreenDashboard:
		return m.updateDashboard(msg)
	case ScreenDetail:
		return m.updateDetail(msg)
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
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "down", "tab":
		if m.selected < len(m.instances)-1 {
			m.selected++
		}

	case "up", "shift+tab":
		if m.selected > 0 {
			m.selected--
		}

	case "enter":
		if len(m.instances) > 0 {
			m.screen = ScreenDetail
			m.prevScreen = ScreenDashboard
			m.detailMenuIdx = 0
			return m, m.pollStats(m.selected)
		}

	case "n":
		return m.startEmbeddedInstall()

	case "d":
		m.screen = ScreenDoctor
		m.prevScreen = ScreenDashboard
		m.doctorDone = false
		m.doctorResults = nil
		return m, tea.Batch(m.doctorSpinner.Tick, m.runDoctorCmd())

	case "r":
		// Refresh stats immediately.
		var cmds []tea.Cmd
		for i := range m.instances {
			cmds = append(cmds, m.pollStats(i))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

const detailMenuCount = 4 // Open UI, Logs, Update, Start/Stop

func (m DashboardModel) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.detailBusy {
		return m, nil // block all input while start/stop is in flight
	}

	// Mouse click on an action item.
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress {
			if idx := m.menuItemAt(mouse.Y); idx >= 0 {
				m.detailMenuIdx = idx
				return m.execDetailMenu()
			}
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "backspace":
		m.screen = ScreenDashboard

	case "down", "tab":
		if m.detailMenuIdx < detailMenuCount-1 {
			m.detailMenuIdx++
		}

	case "up", "shift+tab":
		if m.detailMenuIdx > 0 {
			m.detailMenuIdx--
		}

	case "enter":
		return m.execDetailMenu()

	case "c":
		m.screen = ScreenConfig
		m.buildCfgFields()

	case "d":
		m.screen = ScreenDoctor
		m.prevScreen = ScreenDetail
		m.doctorDone = false
		m.doctorResults = nil
		return m, tea.Batch(m.doctorSpinner.Tick, m.runDoctorCmd())
	}
	return m, nil
}

// menuItemAt returns the 0-based index of the action item at terminal row y,
// or -1 if y does not land on an item.
//
// Layout (0-indexed terminal rows):
//   0           : header bar
//   1           : blank
//   2           : instance title
//   3           : blank
//   4–15        : side-by-side panels (panelH=10, +2 borders = 12 rows)
//   16          : actions panel top border
//   17          : titleRow  (inner line 0)
//   18          : blank     (inner line 1 — spacer before item 0)  ← zone for item 0 starts here
//   19          : item 0    (inner line 2)
//   20          : blank     (inner line 3 — spacer before item 1)  ← zone for item 1 starts here
//   21          : item 1    (inner line 4)
//   ...
//   itemZone(i) starts at 18 + i*2
func (m DashboardModel) menuItemAt(y int) int {
	// itemsStart points to the blank spacer before item 0, so each (blank+item)
	// pair maps cleanly to one idx via integer division.
	const itemsStart = headerLines + 1 + 1 + 1 + (10 + 2) + 1 + 1 // = 18
	rel := y - itemsStart
	if rel < 0 {
		return -1
	}
	idx := rel / 2
	if idx < detailMenuCount {
		return idx
	}
	return -1
}

// execDetailMenu runs the currently highlighted menu item on the Detail screen.
func (m DashboardModel) execDetailMenu() (DashboardModel, tea.Cmd) {
	switch m.detailMenuIdx {
	case 0: // Open UI
		if inst := m.CurrentInstance(); inst != nil {
			port := inst.Info.Config.WebUIPortOrDefault()
			return m, openBrowserCmd("http://localhost:" + port)
		}
	case 1: // Logs
		m.screen = ScreenLogs
		m.logScroll = 0
		return m, m.pollLogs(m.selected)
	case 2: // Update
		return m.startEmbeddedUpdate()
	case 3: // Start/Stop
		m.detailBusy = true
		if inst := m.CurrentInstance(); inst != nil && inst.Status == "running" {
			m.detailBusyAction = "Stopping"
		} else {
			m.detailBusyAction = "Starting"
		}
		return m, tea.Batch(m.toggleContainer(), m.detailSpinner.Tick)
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
	// Match viewLogs: h = contentHeight(); logH = h - panelH(10) - 5; contentLines = logH - 1.
	h := m.contentHeight()
	logH := h - 10 - 5
	if m.logSearchQuery != "" || m.logSearchMode {
		logH -= 3
	}
	if logH < 4 {
		logH = 4
	}
	visibleLines := logH - 1
	if visibleLines < 1 {
		visibleLines = 1
	}

	switch key.String() {
	case "esc", "backspace":
		if m.logSearchQuery != "" {
			m.logSearchQuery = ""
			m.logScroll = 0
		} else {
			m.screen = ScreenDetail
		}
	case "q":
		if m.logSearchQuery == "" {
			m.screen = ScreenDetail
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

// copyToClipboard writes text to the system clipboard via pbcopy (macOS),
// xclip, or xsel (Linux).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
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

func (m DashboardModel) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.cfgEditing {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			// Commit the edit.
			m.cfgFields[m.cfgFocus].Value = m.cfgBuf
			m.cfgFields[m.cfgFocus].Changed = (m.cfgBuf != m.cfgFields[m.cfgFocus].Orig)
			m.cfgEditing = false
			m.cfgBuf = ""
		case tea.KeyEsc:
			// Cancel the edit.
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
		m.screen = ScreenDetail
		m.cfgFields = nil

	case "ctrl+s":
		if err := m.saveCfgFields(); err == nil {
			// Reload instance data.
			if inst := m.CurrentInstance(); inst != nil {
				cfg := inst.Info.Config
				// Update local state to reflect saved values.
				for _, f := range m.cfgFields {
					switch f.Key {
					case "container_name":
						cfg.ContainerName = f.Value
					case "memory":
						cfg.Memory = f.Value
					case "shm_size":
						cfg.ShmSize = f.Value
					case "web_ui_port":
						cfg.WebUIPort = f.Value
					case "image":
						cfg.Image = f.Value
					}
				}
				for i := range m.cfgFields {
					m.cfgFields[i].Orig = m.cfgFields[i].Value
					m.cfgFields[i].Changed = false
				}
			}
		}

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
		m.screen = m.prevScreen
		if m.screen == ScreenDoctor {
			m.screen = ScreenDashboard
		}
	}
	return m, nil
}

// toggleContainer starts or stops the selected instance, then re-polls stats.
// Returns containerToggleDoneMsg so the handler can clear the busy flag.
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
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", url)
		} else {
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}

