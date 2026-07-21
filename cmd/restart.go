package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart an Omnideck installation",
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
}

func runRestart(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	fmt.Printf("Stopping %s... ", cfg.ContainerName)
	changed, err := workflow.EnsureStopped(eng, cfg.ContainerName)
	if err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	if changed {
		fmt.Println(styles.CheckMark)
	} else {
		fmt.Println(styles.Warning.Render("already stopped"))
	}

	fmt.Printf("Starting %s... ", cfg.ContainerName)
	if _, err := workflow.EnsureStarted(eng, cfg.ContainerName); err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	fmt.Println(styles.CheckMark)
	return nil
}
