package cmd

import (
	"os"
	"os/signal"
	"syscall"

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
	eng, err := engineFromConfig(cfg.Engine)
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

	// Handle Ctrl+C cleanly: exit 0 on interrupt rather than letting cobra
	// print an error about the terminated subprocess (or, under --json,
	// emitting a spurious error for what is really just the caller closing
	// the stream). Use a done channel so the goroutine is not leaked when
	// TailLogs returns normally (e.g. --follow=false).
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			os.Exit(0)
		case <-done:
		}
	}()
	defer close(done)
	defer signal.Stop(sigCh)

	if jsonFlag {
		if tailErr := eng.TailLogs(cfg.ContainerName, follow, logsTail, newJSONLogWriter()); tailErr != nil {
			return writeJSONError(newJSONError(ErrCodeInternal, tailErr.Error()))
		}
		return nil
	}

	return eng.TailLogs(cfg.ContainerName, logsFollow, logsTail, os.Stdout)
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
