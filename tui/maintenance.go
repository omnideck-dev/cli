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
	// externalURL carries the pre-replacement sign-in address from the
	// replace step to the verify step. It is a pointer because steps run in
	// tea.Cmd goroutines against copies of the model; Bubble Tea sequences the
	// steps, so the write happens before the read.
	externalURL *workflow.ExternalURLCheck
	// externalURLSummary is the verified outcome shown on completion.
	externalURLSummary string
	// repaired receives the configuration a repair leaves the instance
	// running with, for the same goroutine reason as externalURL.
	repaired *config.Config
}

// NewMaintenanceModel creates a review-first maintenance workflow. No container or file
// changes happen until the user confirms the review screen.
func NewMaintenanceModel(req MaintenanceRequest) MaintenanceModel {
	current, next := workflow.UpdateTarget(req.Config)
	stepLabels := []string{
		"Download the latest Omnideck version",
		"Replace the container and save its settings",
		"Check the MCP sign-in address",
	}
	if req.Mode == MaintenanceRepair {
		stepLabels = []string{
			"Recreate the container from its saved settings",
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
				return m.exit(WorkflowCanceled)
			}
		case MaintenanceStageApplying:
			// Engine operations are allowed to finish so the instance is never
			// abandoned halfway through a replacement.
			return m, nil
		case MaintenanceStageComplete:
			return m.exit(WorkflowCompleted)
		case MaintenanceStageFailed:
			switch msg.String() {
			case "r":
				return m.begin()
			case "b", "enter", "esc", "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		}
	case StepDoneMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		if m.Mode == MaintenanceUpdate && msg.Index == 1 {
			// The container is replaced and its settings saved. Keep the
			// in-memory config aligned with the YAML even if the sign-in
			// address check fails, and make a retry start from what is saved.
			*m.cfg = m.next
			m.current = m.next
		}
		nextStep := msg.Index + 1
		if nextStep < len(m.stepLabels) {
			return m, tea.Batch(cmd, m.startMaintenanceStep(nextStep))
		}
		if m.Mode == MaintenanceUpdate {
			m.externalURLSummary = msg.Detail
		}
		if m.Mode == MaintenanceRepair && m.repaired != nil && m.repaired.ContainerName != "" {
			m.next = *m.repaired
			m.current = m.next
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
	m.externalURLSummary = ""
	m.externalURL = &workflow.ExternalURLCheck{}
	m.repaired = &config.Config{}
	m.spinnerModel = NewSpinnerModel(m.stepLabels, defaultFlavorMessages)
	return m, tea.Batch(m.spinnerModel.Init(), m.startMaintenanceStep(0))
}

func (m MaintenanceModel) exit(outcome WorkflowOutcome) (tea.Model, tea.Cmd) {
	if m.Embedded {
		return m, func() tea.Msg { return WorkflowExitMsg{Outcome: outcome} }
	}
	return m, tea.Quit
}

func (m *MaintenanceModel) startMaintenanceStep(i int) tea.Cmd {
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }
	var workCmd tea.Cmd
	if m.Mode == MaintenanceRepair {
		// Repair is one shared transaction: it reuses the stored version,
		// downloads only when it is missing, and stops rather than create
		// empty saved data.
		repaired := m.repaired
		current, path := m.current, m.configPath
		workCmd = StepCmd(i, func() (string, error) {
			_, result, err := workflow.RepairInstance(m.eng, &current, func(cfg *config.Config) error {
				return workflow.SaveInstanceConfig(m.eng.Name(), path, cfg)
			}, workflow.EnsureInstanceOptions{})
			if err == nil && repaired != nil {
				*repaired = result
			}
			return "", err
		})
		return tea.Sequence(startCmd, workCmd)
	}
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
		before := m.externalURL
		workCmd = StepCmd(i, func() (string, error) {
			if m.Mode == MaintenanceUpdate && before != nil {
				*before = workflow.ExternalURLBeforeReplace(m.eng, &m.next)
			}
			return "", workflow.RecreateAndSave(m.eng, &m.current, &m.next, m.configPath)
		})
	case 2:
		before := m.externalURL
		workCmd = StepCmd(i, func() (string, error) {
			var check workflow.ExternalURLCheck
			if before != nil {
				check = *before
			}
			report, err := workflow.VerifyExternalURL(m.eng, &m.next, check)
			if err != nil {
				return "", err
			}
			return "MCP sign-in address " + report.Summary(), nil
		})
	}
	return tea.Sequence(startCmd, workCmd)
}

func (m MaintenanceModel) reviewText() string {
	if m.Mode == MaintenanceRepair {
		return fmt.Sprintf("Repair %s\n\nOmnideck found this installation's saved settings, but its container is missing. It will recreate the container, reusing the Omnideck version already downloaded (or downloading it if it is missing), and reconnect the same saved file and app-data volumes. If saved data is missing, it stops without changing anything. No saved data will be deleted.", m.next.ContainerName)
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

// TUIView renders maintenance inside the application screen.
func (m MaintenanceModel) TUIView(_ int) string {
	var sb strings.Builder
	switch m.Stage {
	case MaintenanceStageReview:
		sb.WriteString("\n  " + styles.TUIPrimaryBold.Render(m.reviewText()) + "\n\n")
		sb.WriteString("  " + styles.TUISecondaryText.Render("Press Enter to "+m.actionVerb()+", or Esc to go back without changing anything.") + "\n")
	case MaintenanceStageComplete:
		sb.WriteString("\n  " + styles.TUISuccessText.Render("✓") + "  " + styles.TUIPrimaryBold.Render(m.completeText()) + "\n\n")
		if m.externalURLSummary != "" {
			sb.WriteString("  " + styles.TUISecondaryText.Render(m.externalURLSummary) + "\n\n")
		}
		sb.WriteString("  " + styles.TUISecondaryText.Render("Press any key to return to the previous screen.") + "\n")
	case MaintenanceStageFailed:
		sb.WriteString("\n  " + styles.TUIDangerText.Render("✗") + "  " + styles.TUIDangerText.Render(m.failureText()) + "\n\n")
		if m.errorMsg != "" {
			sb.WriteString("     " + styles.TUISecondaryText.Render(m.errorMsg) + "\n\n")
		}
		sb.WriteString("  " + styles.TUISecondaryText.Render("Press r to try again, or Esc to return to the previous screen.") + "\n")
	case MaintenanceStageApplying:
		sb.WriteString("\n")
		for _, step := range m.spinnerModel.Steps {
			sb.WriteString("  " + renderTUIStep(step, m.spinnerModel) + "\n")
		}
	}
	return sb.String()
}

func (m MaintenanceModel) View() string {
	switch m.Stage {
	case MaintenanceStageReview:
		return styles.Header("OMNIDECK", m.title(), m.WindowWidth) + "\n  " + m.reviewText() + "\n\n" + styles.Dim.Render("  Press Enter to "+m.actionVerb()+", or Esc to cancel.")
	case MaintenanceStageComplete:
		summary := ""
		if m.externalURLSummary != "" {
			summary = "  " + m.externalURLSummary + "\n\n"
		}
		return "\n" + styles.Success.Render("  ✓  "+m.completeText()) + "\n\n" + summary + styles.Dim.Render("  Press any key to exit.")
	case MaintenanceStageFailed:
		return "\n" + styles.Error.Render("  ✗  "+m.failureText()) + "\n\n  " + m.errorMsg + "\n\n" + styles.Dim.Render("  Press r to try again, or Esc to exit.")
	default:
		return styles.Header("OMNIDECK", m.title()+" in progress", m.WindowWidth) + m.spinnerModel.View()
	}
}
