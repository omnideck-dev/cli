package tui

import (
	"errors"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// Kept as a seam so the TUI can be exercised without installing host
// software. Both the TUI and Desktop call this same engine workflow.
var ensureRuntimeForTUI = engine.EnsureRuntime

func (m *SetupModel) configureRuntimeSetup() {
	m.Stage = SetupStageRuntime
	m.runtimeSetupStage = runtimeSetupWorking
	m.runtimeStage = engine.SetupStageSoftware
	m.runtimeState = "start"
	m.runtimeActivity = engine.SetupActivitySoftware
	m.runtimeDetail = ""
	m.runtimeProgress = nil
	m.failureFromRuntime = false
	m.preferredEngine = "podman"
	m.spinnerModel = NewSpinnerModel(nil, nil)
	m.spinnerModel.spinner = m.quickCheckSpinner
}

func (m *SetupModel) startRuntimeSetup() tea.Cmd {
	m.runtimeEvents = make(chan tea.Msg, 64)
	events := m.runtimeEvents
	host := m.hostPlatform
	return func() tea.Msg {
		go func() {
			probe, err := ensureRuntimeForTUI(engine.RuntimeSetupOptions{
				Host:                   host,
				AllowTerminalElevation: true,
				OnEvent: func(event engine.RuntimeSetupEvent) {
					events <- runtimeSetupEventMsg{event: event}
				},
			})
			events <- runtimeSetupDoneMsg{probe: probe, err: err}
		}()
		return <-events
	}
}

func waitForRuntimeSetupEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (m SetupModel) updateRuntimeSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.quickCheckSpinner, cmd = m.quickCheckSpinner.Update(msg)
		return m, cmd
	case runtimeSetupEventMsg:
		event := msg.event
		m.runtimeStage = event.Stage
		m.runtimeState = event.State
		m.runtimeActivity = event.Activity
		m.runtimeDetail = event.Detail
		m.runtimeProgress = event.Progress
		return m, waitForRuntimeSetupEvent(m.runtimeEvents)
	case runtimeSetupDoneMsg:
		if msg.err != nil {
			var setupErr *engine.RuntimeSetupError
			if errors.As(msg.err, &setupErr) && setupErr.Failure == engine.RuntimeSetupRestart {
				m.runtimeSetupStage = runtimeSetupRestart
				m.runtimeDetail = setupErr.Hint
				return m, nil
			}
			m.Stage = SetupStageFailed
			m.failureFromRuntime = true
			m.errorMsg = "prepare this computer"
			m.errorDetail = msg.err.Error()
			if errors.As(msg.err, &setupErr) {
				m.errorMsg = setupErr.Message
				m.errorDetail = setupErr.Hint
				if setupErr.Err != nil {
					m.errorDetail += "\n\nTechnical detail: " + setupErr.Err.Error()
				}
			}
			m.errorShowDetails = false
			return m, nil
		}
		engines := engine.ReadyEngines([]engine.ProbeResult{msg.probe})
		if len(engines) == 0 {
			m.Stage = SetupStageFailed
			m.failureFromRuntime = true
			m.errorMsg = "open the secure workspace"
			m.errorDetail = "Podman setup finished, but it is not responding. Restart the computer and run omnideck again."
			return m, nil
		}
		m.eng = engines[0]
		m.engErr = nil
		m.availableEngines = engines
		m.permErr = nil
		return m.afterRuntimeReady()
	case restartRequestedMsg:
		if msg.err != nil {
			m.Stage = SetupStageFailed
			m.failureFromRuntime = true
			m.errorMsg = "restart Windows"
			m.errorDetail = "omnideck could not schedule setup to resume after restart. " + msg.err.Error()
			return m, nil
		}
		return m.exit(WorkflowCompleted)
	case tea.KeyMsg:
		if m.runtimeSetupStage == runtimeSetupRestart {
			switch msg.String() {
			case "enter", " ":
				return m, requestSetupRestart
			case "l", "esc", "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
			return m, nil
		}
		// Setup owns the host installer while it is running. Ignoring quit keys
		// here prevents a half-finished WSL or Podman install from being mistaken
		// for a clean cancellation.
		return m, nil
	}
	return m, nil
}

func requestSetupRestart() tea.Msg {
	return restartRequestedMsg{err: engine.RestartAndResumeSetup()}
}

func (m SetupModel) afterRuntimeReady() (tea.Model, tea.Cmd) {
	if m.setupMode == SetupRuntimeRepair {
		if m.eng == nil {
			m.Stage = SetupStageFailed
			m.errorMsg = "omnideck could not find a ready container runtime"
			return m, nil
		}
		if err := config.SaveRuntime(m.eng.Name()); err != nil {
			m.Stage = SetupStageFailed
			m.errorMsg = "omnideck could not remember the container runtime"
			m.errorDetail = err.Error()
			m.errorShowDetails = true
			return m, nil
		}
		return m.exit(WorkflowCompleted)
	}

	m.ensureRecommendedSettingsAvailable()
	if m.setupMode == SetupFirstRun {
		if !m.validateAllInputs() {
			m.Stage = SetupStageSettings
			m.settingsAdvanced = true
			return m, nil
		}
		return m.beginApplying()
	}
	m.Stage = SetupStageSettings
	return m, nil
}

func (m SetupModel) beginApplying() (tea.Model, tea.Cmd) {
	m.Stage = SetupStageApplying
	m.lastCompletedStep = -1
	m.errorMsg = ""
	m.errorDetail = ""
	m.errorShowDetails = false
	m.failureFromRuntime = false
	m.spinnerModel = NewSpinnerModel(setupStepLabels, defaultFlavorMessages)
	return m, tea.Batch(m.spinnerModel.Init(), m.startSetupStep(0))
}
