package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Pull the latest image and recreate the container",
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	model := tui.NewUpdateModel(cfg, eng)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
