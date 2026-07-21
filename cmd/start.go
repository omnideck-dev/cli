package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Omnideck container",
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	exists, err := eng.ContainerExists(cfg.ContainerName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Container '%s' not found.\nRun: omnideck setup", cfg.ContainerName)
	}

	fmt.Printf("Starting %s... ", cfg.ContainerName)
	if err := eng.StartContainer(cfg.ContainerName); err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	fmt.Println(styles.CheckMark)
	return nil
}
