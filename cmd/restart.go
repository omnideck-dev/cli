package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Omnideck container",
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
	if err := eng.StopContainer(cfg.ContainerName); err != nil {
		if isAlreadyStopped(err) {
			fmt.Println(styles.Warning.Render("already stopped"))
		} else {
			fmt.Println(styles.CrossMark)
			return err
		}
	} else {
		fmt.Println(styles.CheckMark)
	}

	fmt.Printf("Starting %s... ", cfg.ContainerName)
	if err := eng.StartContainer(cfg.ContainerName); err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	fmt.Println(styles.CheckMark)
	return nil
}
