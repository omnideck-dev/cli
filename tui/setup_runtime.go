package tui

import (
	"fmt"
	"os/exec"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

func (m *SetupModel) configureRuntimeSetup() {
	m.Stage = SetupStageRuntime
	m.runtimeSetupStage = runtimeSetupChoose
	m.runtimeShowDetails = false
	m.runtimeLastError = ""
	host := m.hostPlatform
	if host.OS == "" {
		host = engine.DetectHostPlatform()
	}
	m.runtimePlans = engine.BuildSetupPlans(m.runtimeProbes, host)
	m.releaseMissingSavedRuntime()
	selectedRuntime := m.preferredEngine
	automaticSelection := selectedRuntime == ""
	installed := engine.InstalledRuntimeNames(m.runtimeProbes)
	if automaticSelection {
		selectedRuntime = engine.DefaultRuntimeForSetup(m.runtimeProbes, host)
		m.preferredEngine = selectedRuntime
	}
	if selectedRuntime != "" {
		filtered := m.runtimePlans[:0]
		for _, plan := range m.runtimePlans {
			if plan.Runtime == selectedRuntime {
				plan.Recommended = true
				switch {
				case automaticSelection && len(installed) == 0:
					plan.Recommendation = plan.Title + " is the easiest option for this computer."
				case automaticSelection:
					plan.Recommendation = plan.Title + " is already installed, so Omnideck will use it."
				default:
					plan.Recommendation = "Omnideck will use " + plan.Title + " for all your installations on this computer."
				}
				filtered = append(filtered, plan)
			}
		}
		m.runtimePlans = filtered
	}
	m.runtimeChoice = 0
	for i, plan := range m.runtimePlans {
		if plan.Recommended {
			m.runtimeChoice = i
			break
		}
	}
}

func (m SetupModel) updateRuntimeSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.quickCheckSpinner, cmd = m.quickCheckSpinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			if m.runtimeSetupStage == runtimeSetupWorking {
				return m, nil
			}
			return m.exit(WorkflowCanceled)
		}
		if m.runtimeSetupStage == runtimeSetupWorking {
			return m, nil
		}
		if len(m.runtimePlans) == 0 {
			switch msg.String() {
			case "r", "enter", " ":
				m.runtimeSetupStage = runtimeSetupWorking
				m.runtimeMessage = "Checking again…"
				return m, runEngineCheckFor(m.preferredEngine)
			case "b", "esc":
				if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
					m.runtimeSetupEntry = runtimeSetupFromCheck
					m.preferredEngine = ""
					m.quickCheckAlternative = ""
					m.Stage = SetupStageQuickCheck
					m.quickCheckReady = true
					m.runtimeMessage = ""
					return m, nil
				}
				return m.exit(WorkflowCanceled)
			}
			return m, nil
		}
		if msg.String() == "esc" {
			if m.runtimeSetupStage == runtimeSetupReview || m.runtimeSetupStage == runtimeSetupWaiting {
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeMessage = ""
				return m, nil
			}
			if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
				m.runtimeSetupEntry = runtimeSetupFromCheck
				m.preferredEngine = ""
				m.quickCheckAlternative = ""
				m.Stage = SetupStageQuickCheck
				m.quickCheckReady = true
				m.runtimeMessage = ""
				m.runtimeLastError = ""
				return m, nil
			}
			return m.exit(WorkflowCanceled)
		}
		if m.runtimeSetupStage == runtimeSetupWaiting {
			switch msg.String() {
			case "r", "enter", " ":
				m.runtimeCheckFromWaiting = true
				m.runtimeSetupStage = runtimeSetupWorking
				m.runtimeMessage = "Checking whether Podman or Docker is ready…"
				return m, runEngineCheckFor(m.preferredEngine)
			case "b":
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeMessage = ""
			case "o":
				plan := m.runtimePlans[m.runtimeChoice]
				if plan.URL != "" {
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		if m.runtimeSetupStage == runtimeSetupReview {
			switch msg.String() {
			case "b":
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeShowDetails = false
			case "d":
				if m.runtimeDetailsAvailable() {
					m.runtimeShowDetails = !m.runtimeShowDetails
				}
			case "enter", " ":
				plan := m.runtimePlans[m.runtimeChoice]
				if len(plan.Commands) > 0 {
					m.runtimeSetupStage = runtimeSetupWorking
					m.runtimeCommandIndex = 0
					m.runtimeMessage = "Starting the first step…"
					return m, execRuntimeCommand(plan.Commands[0])
				}
				if plan.URL != "" {
					m.runtimeSetupStage = runtimeSetupWaiting
					m.runtimeMessage = runtimeWaitingMessage(plan)
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "up", "left", "shift+tab":
			m.runtimeChoice = (m.runtimeChoice - 1 + len(m.runtimePlans)) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "down", "right", "tab":
			m.runtimeChoice = (m.runtimeChoice + 1) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "d":
			if m.runtimeDetailsAvailable() {
				m.runtimeShowDetails = !m.runtimeShowDetails
			}
		case "b", "esc":
			if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
				m.runtimeSetupEntry = runtimeSetupFromCheck
				m.preferredEngine = ""
				m.quickCheckAlternative = ""
				m.Stage = SetupStageQuickCheck
				m.quickCheckReady = true
				m.runtimeMessage = ""
				m.runtimeLastError = ""
			}
		case "r":
			m.runtimeSetupStage = runtimeSetupWorking
			m.runtimeMessage = "Checking whether Podman or Docker is ready…"
			return m, runEngineCheckFor(m.preferredEngine)
		case "enter", " ":
			m.runtimeSetupStage = runtimeSetupReview
			m.runtimeShowDetails = false
			m.runtimeMessage = ""
		}
	case runtimeCommandDone:
		if msg.err != nil {
			m.runtimeSetupStage = runtimeSetupChoose
			m.runtimeLastError = msg.err.Error()
			m.runtimeMessage = "That step did not finish. You can review it and try again. Press d if you want to see the technical error."
			if len(m.runtimePlans) > 1 {
				m.runtimeMessage = "That step did not finish. You can try again or choose the other option. Press d if you want to see the technical error."
			}
			return m, nil
		}
		plan := m.runtimePlans[m.runtimeChoice]
		m.runtimeCommandIndex++
		if m.runtimeCommandIndex < len(plan.Commands) {
			command := plan.Commands[m.runtimeCommandIndex]
			m.runtimeMessage = fmt.Sprintf("Starting step %d of %d…", m.runtimeCommandIndex+1, len(plan.Commands))
			return m, execRuntimeCommand(command)
		}
		if plan.RequiresRestart {
			m.runtimeSetupStage = runtimeSetupWaiting
			m.runtimeMessage = plan.Title + " is starting. Wait until it says it is ready, then return here and press Enter."
			return m, nil
		}
		m.runtimeMessage = "That step finished. Checking that everything is ready…"
		return m, runEngineCheckFor(m.preferredEngine)
	case engineCheckResult:
		checkedFromWaiting := m.runtimeCheckFromWaiting
		m.runtimeCheckFromWaiting = false
		previousPlan, hadPreviousPlan := m.selectedRuntimePlan()
		m.runtimeSetupStage = runtimeSetupChoose
		m.runtimeProbes = msg.probes
		if msg.eng != nil {
			m.eng = msg.eng
			m.engErr = nil
			m.availableEngines = msg.all
			m.permErr = nil
			return m.afterRuntimeReady()
		}
		m.engErr = msg.err
		m.configureRuntimeSetup()
		if checkedFromWaiting && hadPreviousPlan {
			if currentPlan, ok := m.selectedRuntimePlan(); ok && sameRuntimeSetupStep(previousPlan, currentPlan) {
				m.runtimeSetupStage = runtimeSetupWaiting
				m.runtimeMessage = runtimeStillWaitingMessage(currentPlan)
				return m, nil
			}
		}
		m.runtimeMessage = runtimeNotReadyMessage(m.runtimePlans, m.preferredEngine)
	}
	return m, nil
}

func (m SetupModel) selectedRuntimePlan() (engine.SetupPlan, bool) {
	if m.runtimeChoice < 0 || m.runtimeChoice >= len(m.runtimePlans) {
		return engine.SetupPlan{}, false
	}
	return m.runtimePlans[m.runtimeChoice], true
}

func sameRuntimeSetupStep(before, after engine.SetupPlan) bool {
	return before.Runtime == after.Runtime && before.State == after.State && before.URL == after.URL
}

func (m SetupModel) runtimeDetailsAvailable() bool {
	if m.runtimeChoice < 0 || m.runtimeChoice >= len(m.runtimePlans) {
		return false
	}
	return len(m.runtimePlans[m.runtimeChoice].Commands) > 0 || m.runtimeLastError != ""
}

func (m SetupModel) runtimeDetailsLabel() string {
	if m.runtimeLastError != "" {
		return "error details"
	}
	return "commands"
}

func runtimeWaitingMessage(plan engine.SetupPlan) string {
	name := plan.Title
	if plan.DirectDownload {
		return "The official " + name + " installer should now be downloading. Open it from Downloads, finish the installer, then return here."
	}
	switch plan.State {
	case engine.RuntimePermissionDenied, engine.RuntimeBroken:
		return "The official " + name + " help page should now be open. Follow the guidance there, then return here."
	case engine.RuntimeUnsupportedVersion:
		return "The official Docker update instructions should now be open. Update Docker and make sure it is running, then return here."
	case engine.RuntimeStopped, engine.RuntimeMachineStopped:
		return "The official " + name + " help page should now be open. Follow its instructions to start " + name + ", then return here."
	default:
		return "The official " + name + " installation page should now be open. Finish the installation and make sure " + name + " is running, then return here."
	}
}

func runtimeStillWaitingMessage(plan engine.SetupPlan) string {
	if plan.DirectDownload {
		return "Omnideck still cannot find " + plan.Title + ". If the installer is still open, finish it. Then return here and press Enter to check again."
	}
	return plan.Title + " is not ready yet. Finish the step on the other screen, wait until " + plan.Title + " is running, then return here and press Enter to check again."
}

func runtimeNotReadyMessage(plans []engine.SetupPlan, preferred string) string {
	if len(plans) == 0 {
		name := "Podman or Docker"
		if preferred != "" {
			name = runtimeNameForPeople(preferred)
		}
		return "Omnideck still cannot use " + name + ". Make sure it is installed and running, then check again."
	}
	if len(plans) > 1 {
		return "Neither Podman nor Docker is ready yet. Choose one of the setup options below, or check again if you just finished a step."
	}
	plan := plans[0]
	switch plan.State {
	case engine.RuntimeMissing:
		return "Omnideck still cannot find " + plan.Title + ". Make sure the installation finished, then open " + plan.Title + " and wait until it is running. You can review the installation step below or check again."
	case engine.RuntimeStopped, engine.RuntimeMachineStopped:
		return plan.Title + " is installed, but it is not running yet. Review the start step below or check again after you start it."
	case engine.RuntimeMachineMissing:
		return "Podman is installed, but its one-time setup is not finished. Review the step below to finish it."
	case engine.RuntimePermissionDenied:
		return plan.Title + " is installed, but your account cannot use it yet. Review the help step below."
	case engine.RuntimeUnsupportedVersion:
		return "Docker is installed, but it must be updated before Omnideck can use it. Review the update step below."
	default:
		return plan.Title + " is installed, but it still needs attention. Review the help step below or check again."
	}
}

func (m SetupModel) afterRuntimeReady() (tea.Model, tea.Cmd) {
	if m.setupMode == SetupRuntimeRepair {
		if m.eng == nil {
			m.Stage = SetupStageFailed
			m.errorMsg = "Omnideck could not find a ready container runtime"
			return m, nil
		}
		if err := config.SaveRuntime(m.eng.Name()); err != nil {
			m.Stage = SetupStageFailed
			m.errorMsg = "Omnideck could not remember the container runtime"
			m.errorDetail = err.Error()
			return m, nil
		}
		return m.exit(WorkflowCompleted)
	}
	m.ensureRecommendedSettingsAvailable()
	m.Stage = SetupStageSettings
	return m, nil
}

// ensureRecommendedSettingsAvailable quietly advances default names and ports
// when another container or app already uses the first suggestion. The user
// still sees and confirms the final values before setup starts.
func execRuntimeCommand(command engine.SetupCommand) tea.Cmd {
	cmd := exec.Command(command.Name, command.Args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return runtimeCommandDone{err: err}
	})
}

// maybeAdvanceQuickCheck returns a Cmd that fires allQuickCheckDone once all
// 4 checks (engine, permission counted together, ollama, memory) are done.
// Engine check fires permission check as a follow-up, so we wait for 4 total.
