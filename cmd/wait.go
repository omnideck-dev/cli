package cmd

import (
	"errors"
	"fmt"

	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var waitCmd = &cobra.Command{
	Use: "wait", Short: "Wait for the application to become ready and report startup failures",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := requireConfigMulti(); err != nil {
			return err
		}
		eng, err := detectReadyEngine()
		if err != nil {
			if jsonFlag {
				return writeJSONError(newJSONError(ErrCodeEngineNotFound, err.Error()))
			}
			return err
		}
		err = workflow.WaitForApplication(cmd.Context(), eng, LoadedConfig)
		if err != nil {
			if jsonFlag {
				return writeJSONError(environmentJSONError(err))
			}
			return startupTextError(err)
		}
		if jsonFlag {
			writeJSON(gatherStatusPayload(LoadedConfig, eng))
		} else {
			fmt.Println("omnideck is ready.")
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(waitCmd) }

func startupTextError(err error) error {
	var failure *workflow.StartupFailure
	if errors.As(err, &failure) {
		return fmt.Errorf("%s\n\n%s\nRun `omnideck logs` for technical details. Retry after correcting the problem.", failure.Error(), failure.Detail())
	}
	return err
}
