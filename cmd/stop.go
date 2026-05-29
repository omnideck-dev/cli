package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Omnideck container",
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
	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	fmt.Printf("Stopping %s... ", cfg.ContainerName)
	if err := eng.StopContainer(cfg.ContainerName); err != nil {
		// Already stopped is a warning, not an error.
		if isAlreadyStopped(err) {
			fmt.Println(styles.Warning.Render("already stopped"))
			return nil
		}
		fmt.Println(styles.CrossMark)
		return err
	}
	fmt.Println(styles.CheckMark)
	return nil
}
