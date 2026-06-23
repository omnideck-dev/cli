package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive dashboard TUI",
	Long: `Opens the full Omnideck dashboard — a keyboard-driven terminal UI for managing
all installed instances. Shows live CPU/memory stats, logs, config, and health checks.

Key bindings (Dashboard):
  ↑↓      move selection
  enter   inspect instance
  n       install new instance
  u       update selected instance
  d       run doctor checks
  r       refresh stats
  q       quit`,
	RunE: runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// runTUI launches the interactive dashboard.
func runTUI(_ *cobra.Command, _ []string) error {
	eng, err := engine.Detect()
	if err != nil {
		return fmt.Errorf("no container engine available: %w", err)
	}

	instances, _ := config.ListInstances()
	model := tui.NewDashboardModel(eng, instances)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
