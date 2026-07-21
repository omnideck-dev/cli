package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check Omnideck and explain how to fix problems",
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	if LoadedConfig == nil && (len(instances) > 0 || nameFlag != "" || cfgPath != "") {
		if err := requireConfigMulti(); err != nil {
			return err
		}
	}
	engName := ""
	if LoadedConfig != nil {
		engName = LoadedConfig.Engine
	}
	detectedEng, _ := engineFromConfig(engName)

	results := tui.RunDoctorChecks(LoadedConfig, detectedEng)
	report, allPass := tui.RenderDoctorReport(results)
	fmt.Print(report)

	if !allPass {
		// Return errAborted (empty error) to exit 1 without printing a
		// redundant "Error: ..." line — the report already shows what failed.
		return errAborted
	}
	return nil
}
