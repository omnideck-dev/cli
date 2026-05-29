package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run a deep health check and display a report",
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
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
