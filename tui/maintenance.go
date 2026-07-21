package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
)

// MaintenanceMode identifies why Omnideck is replacing a container.
type MaintenanceMode int

const (
	MaintenanceUpdate MaintenanceMode = iota
	MaintenanceRepair
)

// MaintenanceStage represents one valid stage of a maintenance workflow. It is
// intentionally separate from SetupStage so invalid cross-workflow states
// cannot be represented.
type MaintenanceStage int

const (
	MaintenanceStageReview MaintenanceStage = iota
	MaintenanceStageApplying
	MaintenanceStageComplete
	MaintenanceStageFailed
)

// MaintenanceRequest contains every dependency and target needed by update or repair.
type MaintenanceRequest struct {
	Config     *config.Config
	ConfigPath string
	Engine     engine.Engine
	Embedded   bool
	Mode       MaintenanceMode
}

// MaintenanceModel is the Bubble Tea model shared by update and repair.
type MaintenanceModel struct {
	BaseModel
	Stage MaintenanceStage
	Mode  MaintenanceMode

	Embedded bool

	cfg          *config.Config
	configPath   string
	current      config.Config
	next         config.Config
	eng          engine.Engine
	spinnerModel SpinnerModel
	stepLabels   []string
	errorMsg     string
}

// NewMaintenanceModel creates a review-first maintenance workflow. No container or file
// changes happen until the user confirms the review screen.
func NewMaintenanceModel(req MaintenanceRequest) MaintenanceModel {
	current := *req.Config
	next := current
	next.MigrateImage()
	stepLabels := []string{
		"Download the latest Omnideck version",
		"Replace the container and save its settings",
	}
	if req.Mode == MaintenanceRepair {
		stepLabels = []string{
			"Download Omnideck",
			"Recreate the missing container from its saved settings",
		}
	}
	return MaintenanceModel{
		Stage:        MaintenanceStageReview,
		Mode:         req.Mode,
		Embedded:     req.Embedded,
		cfg:          req.Config,
		configPath:   req.ConfigPath,
		current:      current,
		next:         next,
		eng:          req.Engine,
		spinnerModel: NewSpinnerModel(stepLabels, defaultFlavorMessages),
		stepLabels:   stepLabels,
	}
}

func (m MaintenanceModel) Init() tea.Cmd { return nil }

func (m MaintenanceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
		return m, nil
	case tea.KeyMsg:
		switch m.Stage {
		case MaintenanceStageReview:
			switch msg.String() {
			case "enter":
				return m.begin()
			case "b", "esc", "q", "ctrl+c":
				return m.exit()
			}
		case MaintenanceStageApplying:
			// Engine operations are allowed to finish so the instance is never
			// abandoned halfway through a replacement.
			return m, nil
		case MaintenanceStageComplete:
			return m.exit()
		case MaintenanceStageFailed:
			switch msg.String() {
			case "r":
				return m.begin()
			case "b", "enter", "esc", "q", "ctrl+c":
				return m.exit()
			}
		}
	case StepDoneMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		nextStep := msg.Index + 1
		if nextStep < len(m.stepLabels) {
			return m, tea.Batch(cmd, m.startMaintenanceStep(nextStep))
		}
		*m.cfg = m.next
		m.Stage = MaintenanceStageComplete
		return m, cmd
	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Stage = MaintenanceStageFailed
		m.errorMsg = fmt.Sprintf("%s: %v", m.stepLabels[msg.Index], msg.Err)
		return m, cmd
	default:
		if m.Stage == MaintenanceStageApplying {
			var cmd tea.Cmd
			m.spinnerModel, cmd = m.spinnerModel.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m MaintenanceModel) begin() (tea.Model, tea.Cmd) {
	m.Stage = MaintenanceStageApplying
	m.errorMsg = ""
	m.spinnerModel = NewSpinnerModel(m.stepLabels, defaultFlavorMessages)
	return m, tea.Batch(m.spinnerModel.Init(), m.startMaintenanceStep(0))
}

func (m MaintenanceModel) exit() (tea.Model, tea.Cmd) {
	if m.Embedded {
		return m, func() tea.Msg { return WorkflowExitMsg{} }
	}
	return m, tea.Quit
}

func (m *MaintenanceModel) startMaintenanceStep(i int) tea.Cmd {
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }
	var workCmd tea.Cmd
	switch i {
	case 0:
		workCmd = StepCmd(i, func() (string, error) {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := m.eng.PullImage(m.next.Image, msgs)
			close(msgs)
			return "", err
		})
	case 1:
		workCmd = StepCmd(i, func() (string, error) {
			return "", workflow.RecreateAndSave(m.eng, &m.current, &m.next, m.configPath)
		})
	}
	return tea.Sequence(startCmd, workCmd)
}

func (m MaintenanceModel) reviewText() string {
	if m.Mode == MaintenanceRepair {
		return fmt.Sprintf("Repair %s\n\nOmnideck found this installation's saved settings, but its container is missing. It will recreate the container and reconnect the same saved file and app-data volumes. No saved data will be deleted.", m.next.ContainerName)
	}
	return fmt.Sprintf("Update %s\n\nOmnideck will download the latest version and restart this installation. Your files and agent data are kept in separate container volumes and will not be deleted.", m.next.ContainerName)
}

func (m MaintenanceModel) title() string {
	if m.Mode == MaintenanceRepair {
		return "Repair"
	}
	return "Update"
}

func (m MaintenanceModel) actionVerb() string {
	if m.Mode == MaintenanceRepair {
		return "repair"
	}
	return "update"
}

func (m MaintenanceModel) completeText() string {
	if m.Mode == MaintenanceRepair {
		return "Omnideck was repaired"
	}
	return "Omnideck is up to date"
}

func (m MaintenanceModel) failureText() string {
	if m.Mode == MaintenanceRepair {
		return "The repair could not be completed"
	}
	return "The update could not be completed"
}

// TNView renders maintenance inside the dashboard modal.
func (m MaintenanceModel) TNView(_ int) string {
	var sb strings.Builder
	switch m.Stage {
	case MaintenanceStageReview:
		sb.WriteString("\n  " + styles.TNTextBold.Render(m.reviewText()) + "\n\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press Enter to "+m.actionVerb()+", or b to go back without changing anything.") + "\n")
	case MaintenanceStageComplete:
		sb.WriteString("\n  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render(m.completeText()) + "\n\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press any key to return to the dashboard.") + "\n")
	case MaintenanceStageFailed:
		sb.WriteString("\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render(m.failureText()) + "\n\n")
		if m.errorMsg != "" {
			sb.WriteString("     " + styles.TNDimText.Render(m.errorMsg) + "\n\n")
		}
		sb.WriteString("  " + styles.TNDimText.Render("Press r to try again, or b to return to the dashboard.") + "\n")
	case MaintenanceStageApplying:
		sb.WriteString("\n")
		for _, step := range m.spinnerModel.Steps {
			sb.WriteString("  " + renderTNStep(step, m.spinnerModel) + "\n")
		}
	}
	return sb.String()
}

func (m MaintenanceModel) View() string {
	switch m.Stage {
	case MaintenanceStageReview:
		return styles.Header("OMNIDECK", m.title(), m.WindowWidth) + "\n  " + m.reviewText() + "\n\n" + styles.Dim.Render("  Press Enter to "+m.actionVerb()+", or b to cancel.")
	case MaintenanceStageComplete:
		return "\n" + styles.Success.Render("  ✓  "+m.completeText()) + "\n\n" + styles.Dim.Render("  Press any key to exit.")
	case MaintenanceStageFailed:
		return "\n" + styles.Error.Render("  ✗  "+m.failureText()) + "\n\n  " + m.errorMsg + "\n\n" + styles.Dim.Render("  Press r to try again, or b to exit.")
	default:
		return styles.Header("OMNIDECK", m.title()+" in progress", m.WindowWidth) + m.spinnerModel.View()
	}
}
