package cmd

import (
	"fmt"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/tui"
	"github.com/omnideck-dev/cli/workflow"
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
	inventory, err := config.LoadInventory()
	if err != nil {
		wrapped := fmt.Errorf("reading saved Omnideck installations: %w", err)
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeInternal, wrapped.Error()))
		}
		return wrapped
	}
	if LoadedConfig == nil && (len(inventory.Instances) > 0 || nameFlag != "" || cfgPath != "") {
		if err := requireConfigMulti(); err != nil {
			return err
		}
	}
	detectedEng, _ := detectReadyEngine()

	results := workflow.Diagnose(LoadedConfig, detectedEng)
	results = append(results, workflow.DiagnoseInventoryIssues(inventory.Issues)...)

	if jsonFlag {
		allPass := allChecksPass(results)
		writeJSON(doctorPayload{Checks: jsonCheckResults(results), AllPass: allPass})
		if !allPass {
			return errAborted
		}
		return nil
	}

	report, allPass := tui.RenderDoctorReport(results)
	fmt.Print(report)

	if !allPass {
		// Return errAborted (empty error) to exit 1 without printing a
		// redundant "Error: ..." line — the report already shows what failed.
		return errAborted
	}
	return nil
}
