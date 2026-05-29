package tui

import tea "github.com/charmbracelet/bubbletea"

// Phase represents the current wizard phase.
type Phase int

const (
	PhasePreflight Phase = iota
	PhaseConfig
	PhaseConfirm
	PhaseInstall
	PhaseDone
	PhaseError
)

// BaseModel holds window dimensions shared across TUI models.
type BaseModel struct {
	Phase       Phase
	WindowWidth int
	WindowHeight int
}

// HandleWindowSize updates the base model on a WindowSizeMsg.
func (m *BaseModel) HandleWindowSize(msg tea.WindowSizeMsg) {
	m.WindowWidth = msg.Width
	m.WindowHeight = msg.Height
}
