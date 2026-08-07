package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop an Omnideck installation",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	eng, err := detectReadyEngine()
	if err != nil {
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeEngineNotFound, err.Error()))
		}
		return err
	}

	if jsonFlag {
		return runLifecycleJSON(cfg, eng, func() error {
			_, actionErr := workflow.EnsureStopped(eng, cfg.ContainerName)
			return actionErr
		})
	}

	fmt.Printf("Stopping %s... ", cfg.ContainerName)
	changed, err := workflow.EnsureStopped(eng, cfg.ContainerName)
	if err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	if !changed {
		fmt.Println(styles.Warning.Render("already stopped"))
		return nil
	}
	fmt.Println(styles.CheckMark)
	return nil
}
