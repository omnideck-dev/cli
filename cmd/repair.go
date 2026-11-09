package cmd

import (
	"context"
	"fmt"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var repairCmd = &cobra.Command{
	Use:          "repair",
	Short:        "Recreate a missing or misconfigured container from its saved settings",
	SilenceUsage: true,
	RunE:         runRepair,
}

func init() {
	rootCmd.AddCommand(repairCmd)
}

// repairWaitForApplication is a seam so tests can repair against a fake
// engine without a real application to answer readiness checks.
var repairWaitForApplication = workflow.WaitForApplication

// runRepair runs workflow.RepairInstance for the selected instance: the
// command Doctor's repair_instance action names.
func runRepair(cmd *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	current := LoadedConfig
	eng, err := detectReadyEngine()
	if err != nil {
		structured := newJSONError(ErrCodeEngineNotFound, err.Error()).withAction(workflow.DoctorActionRuntimeSetup, "podman")
		if jsonFlag {
			return writeJSONError(structured)
		}
		return structured
	}
	return repairInstance(cmd.Context(), eng, current)
}

func repairInstance(ctx context.Context, eng engine.Engine, current *config.Config) error {
	stream := newEnsureStream(jsonFlag)
	if stream.nd == nil {
		fmt.Printf("Repairing %s...\n", current.ContainerName)
	}
	result, repaired, err := workflow.RepairInstance(eng, current, func(cfg *config.Config) error {
		return workflow.SaveInstanceConfig(eng.Name(), ConfigPath, cfg)
	}, stream.options())
	if err != nil {
		return stream.fail(err)
	}
	*current = repaired
	stream.startup()
	if err := repairWaitForApplication(ctx, eng, &repaired); err != nil {
		return stream.failStartup(err)
	}
	if stream.complete(result, gatherStatusPayload(loadedInstanceName(), &repaired, eng)) {
		return nil
	}
	fmt.Printf("✓  Omnideck is ready at %s (%s).\n", repaired.BrowserURL(), result.Action)
	return nil
}
