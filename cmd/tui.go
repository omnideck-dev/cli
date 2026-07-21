package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive Omnideck app",
	Long: `Opens Omnideck's keyboard-driven terminal app for managing all installed
instances. Shows live CPU/memory stats, logs, settings, and health checks.

Key bindings (Dashboard):
  ↑↓      move selection
  enter   inspect instance
  n       set up another instance
  u       update selected instance
  d       run doctor checks
  r       refresh stats
  q       quit`,
	RunE: runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// runTUI launches the interactive dashboard.
func runTUI(_ *cobra.Command, _ []string) error {
	legacyRuntime := ""
	if LoadedConfig != nil {
		legacyRuntime = LoadedConfig.Engine
	}
	eng, err := engineFromConfig(legacyRuntime)
	if err != nil {
		return fmt.Errorf("no container runtime is ready: %w", err)
	}

	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	return runApp(eng, instances, LoadedConfig, ConfigPath)
}

// runApp is the single interactive shell for returning users. A legacy
// single-file configuration is included until the user next saves it in the
// conventional instances directory.
func runApp(eng engine.Engine, instances []config.InstanceInfo, loaded *config.Config, loadedPath string) error {
	if eng == nil {
		return fmt.Errorf("Podman or Docker is not ready\nRun `omnideck` for guided setup")
	}
	instances = withLoadedInstance(instances, loaded, loadedPath)
	model := tui.NewAppModel(eng, instances)
	return runAppModel(model)
}

func runAppForDoctor(eng engine.Engine, instances []config.InstanceInfo, selected int) error {
	model := tui.NewAppModelForDoctor(eng, instances, selected)
	return runAppModel(model)
}

func runAppModel(model tui.AppModel) error {
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func withLoadedInstance(instances []config.InstanceInfo, loaded *config.Config, loadedPath string) []config.InstanceInfo {
	if loaded == nil || containsInstanceConfig(instances, loadedPath, loaded.ContainerName) {
		return instances
	}
	return append(instances, config.InstanceInfo{
		Name:   instanceNameFromPath(loadedPath),
		Path:   loadedPath,
		Config: loaded,
	})
}

func containsInstanceConfig(instances []config.InstanceInfo, path, containerName string) bool {
	for _, instance := range instances {
		if instance.Path == path || instance.Config != nil && instance.Config.ContainerName == containerName {
			return true
		}
	}
	return false
}
