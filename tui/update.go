package tui

import (
	"fmt"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// UpdateModel is the Bubble Tea model for the update command.
type UpdateModel struct {
	BaseModel
	cfg          *config.Config
	eng          engine.Engine
	spinnerModel SpinnerModel
	errorMsg     string
}

var updateStepLabels = []string{
	"Pull latest image",
	"Stop container",
	"Remove container",
	"Run container",
}

// NewUpdateModel creates the update TUI model.
func NewUpdateModel(cfg *config.Config, eng engine.Engine) UpdateModel {
	sm := NewSpinnerModel(updateStepLabels, DefaultFlavorMessages)
	return UpdateModel{
		BaseModel:    BaseModel{Phase: PhaseInstall},
		cfg:          cfg,
		eng:          eng,
		spinnerModel: sm,
	}
}

func (m UpdateModel) Init() tea.Cmd {
	return tea.Batch(m.spinnerModel.Init(), m.startUpdateStep(0))
}

func (m UpdateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		if m.Phase == PhaseDone || m.Phase == PhaseError {
			return m, tea.Quit
		}
		_ = msg
	case StepDoneMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		next := msg.Index + 1
		if next < len(updateStepLabels) {
			return m, tea.Batch(cmd, m.startUpdateStep(next))
		}
		m.Phase = PhaseDone
		return m, cmd
	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Phase = PhaseError
		m.errorMsg = fmt.Sprintf("Step '%s' failed: %v", updateStepLabels[msg.Index], msg.Err)
		return m, cmd
	default:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *UpdateModel) startUpdateStep(i int) tea.Cmd {
	cfg := m.cfg
	eng := m.eng
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }

	var workCmd tea.Cmd
	switch i {
	case 0: // Pull latest image.
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
	case 1: // Stop container.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.StopContainer(cfg.ContainerName)
		})
	case 2: // Remove container.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.RemoveContainer(cfg.ContainerName)
		})
	case 3: // Run container.
		workCmd = StepCmd(i, func() (string, error) {
			opts := engine.RunOptions{
				Name:       cfg.ContainerName,
				Image:      cfg.Image,
				ShmSize:    cfg.ShmSize,
				SharedDir:  cfg.SharedDir,
				StateDir:   cfg.StateDir,
				Network:    "host",
				Restart:    "always",
				OllamaHost: checks.OllamaHost(),
				WebUIPort:  cfg.WebUIPortOrDefault(),
				Platform:   runtime.GOOS,
			}
			return "", eng.RunContainer(opts)
		})
	}
	return tea.Sequence(startCmd, workCmd)
}

func (m UpdateModel) View() string {
	switch m.Phase {
	case PhaseDone:
		out := "\n" + styles.Success.Render("  ✓  Omnideck updated successfully!") + "\n"
		out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
		out += "\n" + styles.Dim.Render("  Press any key to exit.")
		return out
	case PhaseError:
		out := "\n" + styles.Error.Render("  ✗  Update failed") + "\n"
		out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
		out += styles.Error.Render("  "+m.errorMsg) + "\n"
		out += "\n" + styles.Dim.Render("  Press any key to exit.")
		return out
	default:
		return styles.Header("OMNIDECK", "Updating", m.WindowWidth) + m.spinnerModel.View()
	}
}

