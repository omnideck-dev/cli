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
	Short: "Tail container logs",
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 50, "Number of lines to show from end of logs")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	// Handle Ctrl+C cleanly: exit 0 on interrupt rather than letting cobra
	// print an error about the terminated subprocess. Use a done channel so
	// the goroutine is not leaked when TailLogs returns normally (e.g. --follow=false).
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

	return eng.TailLogs(cfg.ContainerName, logsFollow, logsTail)
}
