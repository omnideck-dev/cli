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

	// Migrate configs that still point at a superseded image so update pulls
	// and recreates with the current default. Persist before recreating so the
	// stored config matches the container we're about to run.
	if cfg.MigrateImage() {
		if err := config.Save(ConfigPath, cfg); err != nil {
			return fmt.Errorf("saving migrated config: %w", err)
		}
	}

	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	instances, _ := config.ListInstances()
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
