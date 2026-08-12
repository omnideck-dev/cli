package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

// runDoctorCmd runs doctor checks for the current instance.
func (m AppModel) runDoctorCmd() tea.Cmd {
	var cfg *config.Config
	if inst := m.CurrentInstance(); inst != nil {
		cfg = inst.Info.Config
	}
	eng := m.eng
	issues := append([]config.InstanceIssue(nil), m.inventoryIssues...)
	return func() tea.Msg {
		results, usableEngine := workflow.DiagnoseWithProbes(cfg, eng, engine.ProbeAll())
		results = append(results, workflow.DiagnoseInventoryIssues(issues)...)
		return doctorResultsMsg{results: results, eng: usableEngine}
	}
}
