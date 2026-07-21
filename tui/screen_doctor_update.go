package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func (m AppModel) updateDoctor(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.doctorStage == doctorStageActing {
		// Starting or repairing an instance must finish before navigation can
		// continue, otherwise its result would arrive on an unrelated screen.
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "b", "q", "backspace":
		_, _ = m.router.Back()
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

func (m AppModel) runDoctorAction(result CheckResult) (tea.Model, tea.Cmd) {
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
		m.router.Push(RouteSetup)
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
