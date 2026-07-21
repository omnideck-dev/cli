package tui

import tea "github.com/charmbracelet/bubbletea"

// SetupStage represents one valid stage of the setup workflow.
type SetupStage int

const (
	SetupStageQuickCheck SetupStage = iota
	SetupStageRuntime
	SetupStageSettings
	SetupStageReview
	SetupStageApplying
	SetupStageComplete
	SetupStageFailed
)

// BaseModel holds window dimensions shared across TUI models.
type BaseModel struct {
	WindowWidth  int
	WindowHeight int
}

// HandleWindowSize updates the base model on a WindowSizeMsg.
func (m *BaseModel) HandleWindowSize(msg tea.WindowSizeMsg) {
	m.WindowWidth = msg.Width
	m.WindowHeight = msg.Height
}
