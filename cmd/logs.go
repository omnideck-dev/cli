package cmd

import (
	"os"

	"github.com/omnideck-dev/cli/engine"
	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsTail   int
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show Omnideck logs",
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 50, "Number of lines to show from end of logs")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, _ []string) error {
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

	follow := resolveLogsFollow(cmd, jsonFlag, logsFollow)

	if jsonFlag && !follow {
		lines, fetchErr := eng.FetchLogs(cfg.ContainerName, logsTail)
		if fetchErr != nil {
			return writeJSONError(newJSONError(ErrCodeInternal, fetchErr.Error()))
		}
		writeJSON(logsPayload{Lines: lines})
		return nil
	}

	// A killed omnideck process kills the Podman log subprocess too
	// (see engine.SetCancelContext, wired in cmd/root.go's Execute()), so
	// Ctrl+C during --follow surfaces here as TailLogs returning an error for
	// the subprocess we just killed ourselves. Report that as a clean exit
	// rather than letting cobra print an error about a process we terminated
	// on purpose.
	if jsonFlag {
		w := newJSONLogWriter()
		tailErr := eng.TailLogs(cfg.ContainerName, follow, logsTail, w, w)
		if tailErr != nil {
			if engine.CancelRequested() {
				return nil
			}
			return writeJSONError(newJSONError(ErrCodeInternal, tailErr.Error()))
		}
		if w.enc.broken() {
			return writeJSONError(newJSONError(ErrCodeInternal, w.enc.err.Error()))
		}
		return nil
	}

	if tailErr := eng.TailLogs(cfg.ContainerName, logsFollow, logsTail, os.Stdout, os.Stderr); tailErr != nil {
		if engine.CancelRequested() {
			return nil
		}
		return tailErr
	}
	return nil
}

// resolveLogsFollow applies JSON_MODE_SPEC.md's default-follow safety
// valve: --follow/-f defaults to true for interactive use (tail -f-style),
// but a naive `logs --json` shouldn't silently block in an open-ended
// stream — so under --json, follow defaults to false unless the caller
// explicitly passed --follow (or -f). An explicit flag always wins, and
// non-`--json` behavior is untouched.
func resolveLogsFollow(cmd *cobra.Command, jsonMode bool, requested bool) bool {
	if jsonMode && !cmd.Flags().Changed("follow") {
		return false
	}
	return requested
}
