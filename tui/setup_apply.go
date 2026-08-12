package tui

import (
	"errors"
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
			return m, nil
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
			return m.beginApplying()
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
	"Check optional local AI connection",
	"Remember these settings",
}

const (
	setupStepAvailability = iota
	setupStepHomeVolume
	setupStepStateVolume
	setupStepImage
	setupStepContainer
	setupStepOllama
	setupStepSave
)

const (
	ollamaCheckConnected    = "connected"
	ollamaCheckNotConnected = "not connected (optional)"
	ollamaCheckNotInstalled = "not installed (optional)"
)

type setupInputError struct {
	field   int
	message string
}

func (e *setupInputError) Error() string { return e.message }

func (m SetupModel) updateApplying(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)

	case StepStartedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		if m.setupEvents != nil {
			return m, tea.Batch(cmd, waitForSetupEvent(m.setupEvents))
		}
		return m, cmd

	case StepDoneMsg:
		if msg.Index == setupStepHomeVolume {
			m.homeVolumeCreated = msg.Detail == "created"
		}
		if msg.Index == setupStepStateVolume {
			m.stateVolumeCreated = msg.Detail == "created"
		}
		if msg.Index == setupStepOllama {
			m.ollamaContainerChecked = m.ollamaOK
			m.ollamaContainerOK = msg.Detail == ollamaCheckConnected
		}
		return m.finishSetupStep(msg.Index, msg)

	case StepWarningMsg:
		if msg.Index == setupStepOllama {
			m.ollamaContainerChecked = true
			m.ollamaContainerOK = false
		}
		return m.finishSetupStep(msg.Index, msg)

	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Stage = SetupStageFailed
		m.errorMsg = setupStepLabels[msg.Index]
		m.errorDetail = msg.Err.Error()
		m.errorShowDetails = true
		var inputErr *setupInputError
		if msg.Index == setupStepAvailability && errors.Is(msg.Err, workflow.ErrContainerConflict) {
			inputErr = &setupInputError{field: inputContainerName, message: "another container already uses this name; choose a different name"}
		}
		if msg.Index == setupStepAvailability && errors.Is(msg.Err, workflow.ErrPortInUse) {
			inputErr = &setupInputError{field: inputWebUIPort, message: "another app is already using this browser address number"}
		}
		if inputErr == nil {
			_ = errors.As(msg.Err, &inputErr)
		}
		if msg.Index == setupStepAvailability && inputErr != nil {
			m.Stage = SetupStageSettings
			m.settingsAdvanced = true
			m.errorMsg = ""
			m.errorDetail = ""
			m.errorShowDetails = false
			m.inputFocus = inputErr.field
			m.inputErrs[inputErr.field] = inputErr.message
			for i := range m.inputs {
				m.inputs[i].Blur()
			}
			m.inputs[m.inputFocus].Focus()
			return m, cmd
		}
		// Roll back only resources proven to have been created by this setup.
		// Retained volumes from an earlier installation must survive a failed
		// reconnect attempt.
		if m.setupEvents == nil && (m.homeVolumeCreated || m.stateVolumeCreated || m.lastCompletedStep >= setupStepContainer) {
			cfg := m.buildConfig()
			eng := m.eng
			containerCreated := m.lastCompletedStep >= setupStepContainer
			homeVolumeCreated := m.homeVolumeCreated
			stateVolumeCreated := m.stateVolumeCreated
			rollbackCmd := func() tea.Msg {
				if containerCreated {
					_, _ = workflow.EnsureRemoved(eng, cfg.ContainerName)
				}
				if stateVolumeCreated {
					_ = eng.RemoveVolume(cfg.StateVolumeName())
				}
				if homeVolumeCreated {
					_ = eng.RemoveVolume(cfg.HomeVolumeName())
				}
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

func (m SetupModel) finishSetupStep(index int, msg tea.Msg) (tea.Model, tea.Cmd) {
	m.lastCompletedStep = index
	var cmd tea.Cmd
	m.spinnerModel, cmd = m.spinnerModel.Update(msg)
	next := index + 1
	if next < len(setupStepLabels) {
		if m.setupEvents != nil {
			return m, tea.Batch(cmd, waitForSetupEvent(m.setupEvents))
		}
		return m, tea.Batch(cmd, m.startSetupStep(next))
	}
	m.Stage = SetupStageComplete
	return m, cmd
}

func (m *SetupModel) startSetupWorkflow() tea.Cmd {
	m.setupEvents = make(chan tea.Msg, 32)
	events := m.setupEvents
	cfg := m.buildConfig()
	eng := m.eng
	ollamaAvailable := m.ollamaOK
	return func() tea.Msg {
		go func() {
			current := setupStepAvailability
			err := workflow.CreateInstance(eng, cfg, func() error {
				cfg.InstalledAt = time.Now()
				cfg.Engine = ""
				if err := config.SaveRuntime(eng.Name()); err != nil {
					return err
				}
				return config.Save(config.InstancePath(cfg.ContainerName), cfg)
			}, workflow.CreateInstanceOptions{
				OnEvent: func(event workflow.InstanceCreationEvent) {
					current = setupStepForCreationStage(event.Stage)
					if event.State == "start" {
						events <- StepStartedMsg{Index: current}
					} else if event.Warning {
						events <- StepWarningMsg{Index: current, Detail: event.Detail}
					} else {
						events <- StepDoneMsg{Index: current, Detail: event.Detail}
					}
				},
				Verify: func() (string, bool) {
					if !ollamaAvailable {
						return ollamaCheckNotInstalled, false
					}
					if err := eng.CheckOllamaConnection(cfg.ContainerName); err != nil {
						return ollamaCheckNotConnected, true
					}
					return ollamaCheckConnected, false
				},
			})
			if err != nil {
				events <- StepFailedMsg{Index: current, Err: err}
			}
		}()
		return <-events
	}
}

func waitForSetupEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func setupStepForCreationStage(stage workflow.InstanceCreationStage) int {
	switch stage {
	case workflow.CreateStageAvailability:
		return setupStepAvailability
	case workflow.CreateStageHomeVolume:
		return setupStepHomeVolume
	case workflow.CreateStageStateVolume:
		return setupStepStateVolume
	case workflow.CreateStagePullImage:
		return setupStepImage
	case workflow.CreateStageRunContainer:
		return setupStepContainer
	case workflow.CreateStageVerify:
		return setupStepOllama
	case workflow.CreateStageSaveConfig:
		return setupStepSave
	default:
		return setupStepAvailability
	}
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
			if m.failureFromRuntime {
				m.configureRuntimeSetup()
				return m, m.startRuntimeSetup()
			}
			m.Stage = SetupStageReview
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
	case setupStepAvailability: // Recheck name and port immediately before making changes.
		workCmd = StepCmd(i, func() (string, error) {
			return "available", checkSetupAvailability(eng, cfg)
		})
	case setupStepHomeVolume: // Create home volume.
		workCmd = StepCmd(i, func() (string, error) {
			created, err := eng.CreateVolume(cfg.HomeVolumeName())
			if created {
				return "created", err
			}
			return "reusing existing", err
		})
	case setupStepStateVolume: // Create state volume.
		workCmd = StepCmd(i, func() (string, error) {
			created, err := eng.CreateVolume(cfg.StateVolumeName())
			if created {
				return "created", err
			}
			return "reusing existing", err
		})
	case setupStepImage: // Pull image.
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
	case setupStepContainer: // Run container.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.RunContainer(workflow.RunOptions(cfg))
		})
	case setupStepOllama: // Verify the optional service from the real container network.
		workCmd = func() tea.Msg {
			if !m.ollamaOK {
				return StepDoneMsg{Index: i, Detail: ollamaCheckNotInstalled}
			}
			if err := eng.CheckOllamaConnection(cfg.ContainerName); err != nil {
				return StepWarningMsg{Index: i, Detail: ollamaCheckNotConnected}
			}
			return StepDoneMsg{Index: i, Detail: ollamaCheckConnected}
		}
	case setupStepSave: // Save settings.
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

func checkSetupAvailability(eng interface {
	ContainerExists(string) (bool, error)
}, cfg *config.Config) error {
	exists, err := eng.ContainerExists(cfg.ContainerName)
	if err != nil {
		return fmt.Errorf("checking the name %q: %w", cfg.ContainerName, err)
	}
	if exists {
		return &setupInputError{
			field:   inputContainerName,
			message: "another container already uses this name; choose a different name",
		}
	}
	if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
		return &setupInputError{
			field:   inputWebUIPort,
			message: "another app is already using this browser address number",
		}
	}
	return nil
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
