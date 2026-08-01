package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start an Omnideck installation",
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
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeEngineNotFound, err.Error()))
		}
		return err
	}

	if jsonFlag {
		return runLifecycleJSON(cfg, eng, func() error {
			_, actionErr := workflow.EnsureStarted(eng, cfg.ContainerName)
			return actionErr
		})
	}

	fmt.Printf("Starting %s... ", cfg.ContainerName)
	changed, err := workflow.EnsureStarted(eng, cfg.ContainerName)
	if err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	if !changed {
		fmt.Println(styles.Warning.Render("already running"))
		return nil
	}
	fmt.Println(styles.CheckMark)
	return nil
}
