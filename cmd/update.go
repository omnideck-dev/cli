package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Omnideck while keeping its saved data",
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

	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	instances = withLoadedInstance(instances, cfg, ConfigPath)
	selectedIdx := 0
	for i, inst := range instances {
		if inst.Config != nil && inst.Config.ContainerName == cfg.ContainerName {
			selectedIdx = i
			break
		}
	}

	model := tui.NewDashboardModelForUpdate(eng, instances, cfg, selectedIdx)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
