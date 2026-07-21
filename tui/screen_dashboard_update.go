package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/workflow"
)

func (m AppModel) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.router.Push(RouteLogs)
			m.logScroll = 0
			return m, m.pollLogs(m.selected)
		}

	case "c":
		if len(m.instances) > 0 {
			m.router.Push(RouteSettings)
			m.buildSettingFields()
		}

	case "n":
		return m.startEmbeddedSetup()

	case "u":
		if len(m.instances) > 0 {
			return m.startEmbeddedUpdate()
		}

	case "d":
		m.router.Push(RouteDoctor)
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
func (m AppModel) isExpanded() bool {
	if m.selected < 0 || m.selected >= len(m.instances) {
		return false
	}
	return m.expanded[m.instances[m.selected].Info.Name]
}

// doToggleContainer initiates a stop/start for the selected instance with a toast.
func (m AppModel) doToggleContainer() (AppModel, tea.Cmd) {
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
func (m AppModel) execChip() (AppModel, tea.Cmd) {
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
		m.router.Push(RouteLogs)
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

// toggleContainer starts or stops the selected instance, then re-polls stats.
func (m AppModel) toggleContainer() tea.Cmd {
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
