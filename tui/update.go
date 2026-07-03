package tui

import (
	"fmt"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// UpdateModel is the Bubble Tea model for the update command.
type UpdateModel struct {
	BaseModel

	// Embedded, when true, means this model runs inside DashboardModel.
	// On done/error it emits WizardExitMsg instead of tea.Quit.
	Embedded bool

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
	sm := NewSpinnerModel(updateStepLabels, defaultFlavorMessages)
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
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
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
				Name:        cfg.ContainerName,
				Image:       cfg.Image,
				Memory:      cfg.Memory,
				ShmSize:     cfg.ShmSize,
				HomeVolume:  cfg.HomeVolumeName(),
				StateVolume: cfg.StateVolumeName(),
				Restart:     "always",
				WebUIPort:   cfg.WebUIPortOrDefault(),
				Platform:    runtime.GOOS,
			}
			return "", eng.RunContainer(opts)
		})
	}
	return tea.Sequence(startCmd, workCmd)
}

// TNView renders the update progress in Tokyo Night style.
// Called by DashboardModel.viewUpdate() to render in the log-panel area.
func (m UpdateModel) TNView(_ int) string {
	var sb strings.Builder
	switch m.Phase {
	case PhaseDone:
		sb.WriteString("\n  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render("Updated successfully!") + "\n\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press any key to return to dashboard.") + "\n")
	case PhaseError:
		sb.WriteString("\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render("Update failed") + "\n\n")
		if m.errorMsg != "" {
			sb.WriteString("     " + styles.TNDimText.Render(m.errorMsg) + "\n\n")
		}
		sb.WriteString("  " + styles.TNDimText.Render("Press any key to return.") + "\n")
	default:
		sb.WriteString("\n")
		for _, step := range m.spinnerModel.Steps {
			sb.WriteString("  " + renderTNStep(step, m.spinnerModel) + "\n")
		}
	}
	return sb.String()
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
