package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
)

// reloadInstancesCmd fetches the current instance list and returns instancesRefreshedMsg.
func reloadInstancesCmd() tea.Cmd {
	return func() tea.Msg {
		instances, err := config.ListInstances()
		return instancesRefreshedMsg{instances: instances, err: err}
	}
}

// clearToastCmd fires toastClearMsg after ~1.7 seconds.
func clearToastCmd() tea.Cmd {
	return tea.Tick(1700*time.Millisecond, func(time.Time) tea.Msg { return toastClearMsg{} })
}
