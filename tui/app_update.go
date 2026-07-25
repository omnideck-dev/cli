package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Update dispatches messages to the appropriate screen handler.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		if m.operationInProgress() {
			return m, nil
		}
		return m, tea.Quit
	}
	if result, ok := msg.(DialogResultMsg); ok {
		m.dialog = nil
		if result.Confirmed && result.Action == DialogDiscardSettings {
			m.settingFields = nil
			_, _ = m.router.Back()
		}
		return m, nil
	}
	if m.dialog != nil {
		dialog, cmd := m.dialog.Update(msg)
		m.dialog = &dialog
		return m, cmd
	}

	// WorkflowExitMsg returns to the screen that opened the workflow.
	if exit, ok := msg.(WorkflowExitMsg); ok {
		if exit.Outcome == WorkflowCanceled && !m.router.CanGoBack() {
			return m, tea.Quit
		}
		return m.finishWorkflow()
	}

	// Global handlers first.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.router.Current() == RouteSetup {
			m.setupModel.HandleWindowSize(msg)
		}
		if m.router.Current() == RouteMaintenance {
			m.maintenanceModel.HandleWindowSize(msg)
		}
		if m.router.Current() == RouteRemoval {
			m.removalModel.HandleWindowSize(msg)
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
		if m.router.Current() == RouteLogs {
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
			m.toast = ""
			m.settingsMessage = "The new settings could not be applied: " + msg.err.Error()
			return m, nil
		}
		_, _ = m.router.Back()
		if m.router.Current() == RouteSettings {
			m.router.Replace(RouteDashboard)
		}
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
		if m.router.Current() == RouteDoctor {
			var cmd tea.Cmd
			m.doctorSpinner, cmd = m.doctorSpinner.Update(msg)
			return m, cmd
		}
		// Setup, Maintenance, and Removal own their spinners. Continue to the active
		// route instead of consuming their animation message here.
	}

	// Per-screen handlers.
	switch m.router.Current() {
	case RouteSetup:
		newModel, cmd := m.setupModel.Update(msg)
		m.setupModel = newModel.(SetupModel)
		if m.setupModel.eng != nil {
			m.eng = m.setupModel.eng
		}
		return m, cmd
	case RouteMaintenance:
		newModel, cmd := m.maintenanceModel.Update(msg)
		m.maintenanceModel = newModel.(MaintenanceModel)
		return m, cmd
	case RouteRemoval:
		newModel, cmd := m.removalModel.Update(msg)
		m.removalModel = newModel.(RemovalModel)
		return m, cmd
	case RouteDashboard:
		return m.updateDashboard(msg)
	case RouteLogs:
		return m.updateLogs(msg)
	case RouteSettings:
		return m.updateSettings(msg)
	case RouteDoctor:
		return m.updateDoctor(msg)
	}
	return m, nil
}

func (m AppModel) operationInProgress() bool {
	switch m.router.Current() {
	case RouteSetup:
		return m.setupModel.Stage == SetupStageApplying ||
			m.setupModel.Stage == SetupStageRuntime && m.setupModel.runtimeSetupStage == runtimeSetupWorking
	case RouteSettings:
		return m.settingsStage == settingsStageApplying
	case RouteDoctor:
		return m.doctorStage == doctorStageActing
	case RouteMaintenance:
		return m.maintenanceModel.Stage == MaintenanceStageApplying
	case RouteRemoval:
		return m.removalModel.Stage == RemovalStageApplying
	default:
		return false
	}
}

func (m AppModel) finishWorkflow() (tea.Model, tea.Cmd) {
	previous, ok := m.router.Back()
	if !ok {
		m.router.Reset(RouteDashboard)
		previous = RouteDashboard
	}
	cmds := []tea.Cmd{reloadInstancesCmd()}
	if previous == RouteDoctor {
		m.doctorStage = doctorStageChecking
		m.doctorMessage = "Checking again…"
		cmds = append(cmds, m.doctorSpinner.Tick, m.runDoctorCmd())
	}
	return m, tea.Batch(cmds...)
}
