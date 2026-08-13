package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
)

// RemovalStage represents one valid screen in the remove-instance journey.
type RemovalStage int

const (
	RemovalStageDataChoice RemovalStage = iota
	RemovalStageReview
	RemovalStageBackupChoice
	RemovalStageDeleteConfirm
	RemovalStageApplying
	RemovalStageComplete
	RemovalStageFailed
)

type RemovalRequest struct {
	Instance config.InstanceInfo
	Engine   engine.Engine
	Embedded bool
}

type removalDoneMsg struct {
	result workflow.RemoveInstanceResult
	err    error
}

// RemovalModel owns the full-screen remove-instance workflow. Permanent data
// deletion requires both an explicit choice and typing the instance name.
type RemovalModel struct {
	BaseModel
	Stage RemovalStage

	Embedded bool
	instance config.InstanceInfo
	eng      engine.Engine

	dataChoice   int
	backupChoice int
	deleteData   bool
	backupData   bool
	confirmation textinput.Model
	inputError   string
	spinner      spinner.Model
	result       workflow.RemoveInstanceResult
	errorMsg     string
}

func NewRemovalModel(req RemovalRequest) RemovalModel {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 128
	input.Width = 40
	// The TUI paints its own dark canvas, so give the confirmation field an
	// explicit light surface instead of inheriting either terminal color.
	input.PromptStyle = styles.TUITextInput
	input.TextStyle = styles.TUITextInput
	input.Cursor.Style = styles.TUITextInputCursor
	input.Cursor.TextStyle = styles.TUITextInput

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.TUIAccentText

	return RemovalModel{
		Stage:        RemovalStageDataChoice,
		Embedded:     req.Embedded,
		instance:     req.Instance,
		eng:          req.Engine,
		confirmation: input,
		spinner:      sp,
	}
}

func (m RemovalModel) Init() tea.Cmd { return nil }

func (m RemovalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
		return m, nil
	case spinner.TickMsg:
		if m.Stage == RemovalStageApplying {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case removalDoneMsg:
		m.result = msg.result
		if msg.err != nil {
			m.Stage = RemovalStageFailed
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		m.Stage = RemovalStageComplete
		return m, nil
	case tea.KeyMsg:
		if m.Stage == RemovalStageApplying {
			return m, nil
		}
		switch m.Stage {
		case RemovalStageDataChoice:
			switch msg.String() {
			case "up", "down", "left", "right", "tab", "shift+tab":
				m.dataChoice = (m.dataChoice + 1) % 2
			case "enter", " ":
				m.deleteData = m.dataChoice == 1
				if m.deleteData {
					m.Stage = RemovalStageBackupChoice
				} else {
					m.backupData = false
					m.Stage = RemovalStageReview
				}
			case "b", "esc", "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		case RemovalStageReview:
			switch msg.String() {
			case "enter", " ":
				return m.begin()
			case "b", "esc":
				m.Stage = RemovalStageDataChoice
			case "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		case RemovalStageBackupChoice:
			switch msg.String() {
			case "up", "down", "left", "right", "tab", "shift+tab":
				m.backupChoice = (m.backupChoice + 1) % 2
			case "enter", " ":
				m.backupData = m.backupChoice == 0
				m.Stage = RemovalStageDeleteConfirm
				m.inputError = ""
				m.confirmation.SetValue("")
				return m, m.confirmation.Focus()
			case "b", "esc":
				m.Stage = RemovalStageDataChoice
			case "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		case RemovalStageDeleteConfirm:
			switch msg.String() {
			case "enter":
				if m.confirmation.Value() != m.instance.Name {
					m.inputError = "The name does not match. Nothing has been deleted."
					return m, nil
				}
				m.confirmation.Blur()
				return m.begin()
			case "esc":
				m.confirmation.Blur()
				m.Stage = RemovalStageBackupChoice
				m.inputError = ""
				return m, nil
			case "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
			var cmd tea.Cmd
			m.confirmation, cmd = m.confirmation.Update(msg)
			m.inputError = ""
			return m, cmd
		case RemovalStageComplete:
			return m.exit(WorkflowCompleted)
		case RemovalStageFailed:
			switch msg.String() {
			case "r":
				return m.begin()
			case "b", "enter", "esc", "q", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		}
	}
	return m, nil
}

func (m RemovalModel) begin() (tea.Model, tea.Cmd) {
	m.Stage = RemovalStageApplying
	m.errorMsg = ""
	m.result = workflow.RemoveInstanceResult{}
	eng := m.eng
	instance := m.instance
	opts := workflow.RemoveInstanceOptions{DeleteData: m.deleteData, BackupData: m.backupData}
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		result, err := workflow.RemoveInstance(eng, instance, opts)
		return removalDoneMsg{result: result, err: err}
	})
}

func (m RemovalModel) exit(outcome WorkflowOutcome) (tea.Model, tea.Cmd) {
	if m.Embedded {
		return m, func() tea.Msg { return WorkflowExitMsg{Outcome: outcome} }
	}
	return m, tea.Quit
}

func (m RemovalModel) instanceName() string {
	if m.instance.Name != "" {
		return m.instance.Name
	}
	if m.instance.Config != nil {
		return m.instance.Config.ContainerName
	}
	return "Omnideck"
}

func (m RemovalModel) TUIView(width int) string {
	var sb strings.Builder
	name := m.instanceName()
	sb.WriteString("\n")
	switch m.Stage {
	case RemovalStageDataChoice:
		writeTUIWrapped(&sb, width, "  ", "  ", "Remove "+name, styles.TUIPrimaryBold)
		writeTUIWrapped(&sb, width, "  ", "  ", "Omnideck will stop and remove this instance and forget its settings. The Omnideck CLI and your container runtime will stay installed.", styles.TUISecondaryText)
		sb.WriteString("\n  " + styles.TUIPrimaryText.Render("What should happen to its saved data?") + "\n")
		m.writeChoice(&sb, width, 0, "Keep saved data — Recommended", "You can reconnect it later by setting up an instance with the same name.", false)
		m.writeChoice(&sb, width, 1, "Permanently delete saved data", "Files and agent data in this instance's two data volumes will be deleted.", true)
	case RemovalStageReview:
		writeTUIWrapped(&sb, width, "  ", "  ", "Ready to remove "+name, styles.TUIPrimaryBold)
		sb.WriteString("\n")
		writeTUIWrapped(&sb, width, "  ✓ ", "    ", "Stop and remove the instance container.", styles.TUISecondaryText)
		writeTUIWrapped(&sb, width, "  ✓ ", "    ", "Remove this instance from the Decks screen.", styles.TUISecondaryText)
		writeTUIWrapped(&sb, width, "  ✓ ", "    ", "Keep its saved files and agent data.", styles.TUISuccessText)
		sb.WriteString("\n")
		writeTUIWrapped(&sb, width, "  ", "  ", "Press Enter to remove the instance, or Esc to go back without changing anything.", styles.TUIPrimaryText)
	case RemovalStageBackupChoice:
		writeTUIWrapped(&sb, width, "  ", "  ", "Protect your data before deleting it", styles.TUIPrimaryBold)
		writeTUIWrapped(&sb, width, "  ", "  ", "Permanent deletion cannot be undone. A backup gives you one last copy outside the container runtime.", styles.TUISecondaryText)
		sb.WriteString("\n  " + styles.TUIPrimaryText.Render("Create a backup first?") + "\n")
		m.writeBackupChoice(&sb, width, 0, "Create a backup — Recommended", "Save one backup file in your home folder, then delete the data.", false)
		m.writeBackupChoice(&sb, width, 1, "Delete without a backup", "Permanently delete the data without making a copy.", true)
	case RemovalStageDeleteConfirm:
		writeTUIWrapped(&sb, width, "  ", "  ", "Confirm permanent data deletion", styles.TUIDangerText)
		writeTUIWrapped(&sb, width, "  ", "  ", "This removes the instance and permanently deletes its saved files and agent data.", styles.TUISecondaryText)
		if m.backupData {
			writeTUIWrapped(&sb, width, "  ", "  ", "Omnideck will create a backup in your home folder before deleting the data.", styles.TUISuccessText)
		} else {
			writeTUIWrapped(&sb, width, "  ", "  ", "No backup will be created.", styles.TUIWarningText)
		}
		sb.WriteString("\n")
		writeTUIWrapped(&sb, width, "  ", "  ", fmt.Sprintf("Type %s below to confirm. This prevents accidental deletion.", name), styles.TUIPrimaryText)
		sb.WriteString("  " + m.confirmation.View() + "\n")
		if m.inputError != "" {
			writeTUIWrapped(&sb, width, "  ", "  ", m.inputError, styles.TUIDangerText)
		}
	case RemovalStageApplying:
		message := "Removing the instance safely…"
		if m.deleteData && m.backupData {
			message = "Backing up the data, then removing the instance…"
		} else if m.deleteData {
			message = "Removing the instance and permanently deleting its data…"
		}
		sb.WriteString("  " + m.spinner.View() + " " + styles.TUIPrimaryBold.Render(message) + "\n\n")
		writeTUIWrapped(&sb, width, "  ", "  ", "Keep this window open until the operation finishes.", styles.TUISecondaryText)
	case RemovalStageComplete:
		sb.WriteString("  " + styles.TUISuccessText.Render("✓") + "  " + styles.TUIPrimaryBold.Render(name+" was removed") + "\n\n")
		if m.deleteData {
			writeTUIWrapped(&sb, width, "  ", "  ", "Its saved data was permanently deleted.", styles.TUISecondaryText)
			if m.result.BackupPath != "" {
				writeTUIWrapped(&sb, width, "  ", "  ", "Backup: "+m.result.BackupPath, styles.TUISuccessText)
			}
		} else {
			writeTUIWrapped(&sb, width, "  ", "  ", "Its saved files and agent data were kept.", styles.TUISuccessText)
		}
		writeTUIWrapped(&sb, width, "  ", "  ", "The Omnideck CLI and container runtime are still installed. Press any key to return to Decks.", styles.TUISecondaryText)
	case RemovalStageFailed:
		sb.WriteString("  " + styles.TUIDangerText.Render("✗") + "  " + styles.TUIPrimaryBold.Render("The instance could not be fully removed") + "\n\n")
		writeTUIWrapped(&sb, width, "  ", "  ", "Omnideck kept the saved instance settings so the operation remains visible and can be retried. Some requested steps may already have finished.", styles.TUISecondaryText)
		if m.result.BackupPath != "" {
			writeTUIWrapped(&sb, width, "  ", "  ", "Backup created before the problem occurred: "+m.result.BackupPath, styles.TUISuccessText)
		}
		if m.errorMsg != "" {
			writeTUIWrapped(&sb, width, "  ", "  ", m.errorMsg, styles.TUIDangerText)
		}
		writeTUIWrapped(&sb, width, "  ", "  ", "Press r to try again, or Esc to return to Decks.", styles.TUIPrimaryText)
	}
	return sb.String()
}

func (m RemovalModel) writeChoice(sb *strings.Builder, width, index int, label, detail string, dangerous bool) {
	selected := m.dataChoice == index
	writeRemovalChoice(sb, width, selected, label, detail, dangerous)
}

func (m RemovalModel) writeBackupChoice(sb *strings.Builder, width, index int, label, detail string, dangerous bool) {
	selected := m.backupChoice == index
	writeRemovalChoice(sb, width, selected, label, detail, dangerous)
}

func writeRemovalChoice(sb *strings.Builder, width int, selected bool, label, detail string, dangerous bool) {
	prefix := "    "
	labelStyle := styles.TUISecondaryText
	if selected {
		prefix = "  " + styles.TUIAccentText.Render("▸ ")
		labelStyle = styles.TUIPrimaryBold
		if dangerous {
			labelStyle = styles.TUIDangerText
		}
	}
	writeTUIWrapped(sb, width, prefix, "    ", label, labelStyle)
	if selected {
		writeTUIWrapped(sb, width, "      ", "      ", detail, styles.TUISecondaryText)
	}
}

func (m RemovalModel) View() string { return m.TUIView(m.WindowWidth) }
