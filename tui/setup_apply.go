package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func (m SetupModel) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m.exit(WorkflowCanceled)
		case "b", "esc":
			m.Stage = SetupStageSettings
			m.settingsAdvanced = false
			m.reviewShowDetails = false
			return m, nil
		case "d":
			m.reviewShowDetails = !m.reviewShowDetails
		case "i", "enter", " ":
			if !m.validateAllInputs() {
				m.Stage = SetupStageSettings
				m.settingsAdvanced = true
				for i := range m.inputs {
					m.inputs[i].Blur()
				}
				m.inputs[m.inputFocus].Focus()
				return m, nil
			}
			m.Stage = SetupStageApplying
			m.lastCompletedStep = -1
			m.errorMsg = ""
			m.errorDetail = ""
			m.errorShowDetails = false
			m.spinnerModel = NewSpinnerModel(setupStepLabels, defaultFlavorMessages)
			return m, tea.Batch(m.spinnerModel.Init(), m.startSetupStep(0))
		}
	}
	return m, nil
}

var setupStepLabels = []string{
	"Check that the name and browser address are available",
	"Prepare space for your files",
	"Prepare space for Omnideck data",
	"Download Omnideck",
	"Start Omnideck",
	"Remember these settings",
}

func (m SetupModel) updateApplying(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)

	case StepDoneMsg:
		m.lastCompletedStep = msg.Index
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		next := msg.Index + 1
		if next < len(setupStepLabels) {
			return m, tea.Batch(cmd, m.startSetupStep(next))
		}
		// All steps done.
		m.Stage = SetupStageComplete
		return m, cmd

	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Stage = SetupStageFailed
		m.errorMsg = setupStepLabels[msg.Index]
		m.errorDetail = msg.Err.Error()
		m.errorShowDetails = false
		if msg.Index == 0 {
			m.Stage = SetupStageSettings
			m.settingsAdvanced = true
			_ = m.validateAllInputs()
			for i := range m.inputs {
				m.inputs[i].Blur()
			}
			m.inputs[m.inputFocus].Focus()
			return m, cmd
		}
		// Attempt rollback: if the container was run (step 4 completed) but
		// settings were not saved (step 5 failed), remove the orphaned container.
		if m.lastCompletedStep >= 4 {
			cfg := m.buildConfig()
			eng := m.eng
			rollbackCmd := func() tea.Msg {
				_, _ = workflow.EnsureRemoved(eng, cfg.ContainerName)
				return nil
			}
			return m, tea.Batch(cmd, rollbackCmd)
		}
		return m, cmd

	default:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m SetupModel) updateFailed(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "d":
			m.errorShowDetails = !m.errorShowDetails
		case "r":
			m.Stage = SetupStageReview
			m.reviewShowDetails = false
		case "b", "enter", "esc", "q", "ctrl+c":
			return m.exit(WorkflowCanceled)
		}
	}
	return m, nil
}

// startSetupStep returns the tea.Cmd for step i, sending StepStartedMsg
// immediately and running the work asynchronously.
func (m *SetupModel) startSetupStep(i int) tea.Cmd {
	cfg := m.buildConfig()
	eng := m.eng

	// Fire StepStartedMsg first.
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }

	var workCmd tea.Cmd
	switch i {
	case 0: // Recheck name and port immediately before making changes.
		workCmd = StepCmd(i, func() (string, error) {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil {
				return "", fmt.Errorf("checking the name %q: %w", cfg.ContainerName, err)
			}
			if exists {
				return "", fmt.Errorf("another container already uses the name %q; go back and choose a different name", cfg.ContainerName)
			}
			if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
				return "", fmt.Errorf("another app is already using browser address number %s; go back and choose a different number", cfg.WebUIPortOrDefault())
			}
			return "available", nil
		})
	case 1: // Create home volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.HomeVolumeName())
		})
	case 2: // Create state volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.StateVolumeName())
		})
	case 3: // Pull image.
		workCmd = StepCmd(i, func() (string, error) {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(cfg.Image, msgs)
			close(msgs)
			return "", err
		})
	case 4: // Run container.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.RunContainer(workflow.RunOptions(cfg))
		})
	case 5: // Save settings.
		workCmd = StepCmd(i, func() (string, error) {
			cfg.InstalledAt = time.Now()
			cfg.Engine = ""
			if err := config.SaveRuntime(eng.Name()); err != nil {
				return "", err
			}
			// Always save to the instances dir keyed by container name.
			savePath := config.InstancePath(cfg.ContainerName)
			return savePath, config.Save(savePath, cfg)
		})
	}

	return tea.Sequence(startCmd, workCmd)
}

func (m *SetupModel) buildConfig() *config.Config {
	image := config.DefaultConfig().Image
	if m.imageOverride != "" {
		image = m.imageOverride
	}
	return &config.Config{
		ContainerName: strings.TrimSpace(m.inputs[inputContainerName].Value()),
		Memory:        strings.TrimSpace(m.inputs[inputMemory].Value()),
		ShmSize:       strings.TrimSpace(m.inputs[inputShmSize].Value()),
		WebUIPort:     strings.TrimSpace(m.inputs[inputWebUIPort].Value()),
		Image:         image,
	}
}
