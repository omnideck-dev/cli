package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)


var installImageFlag string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Omnideck via interactive TUI wizard",
	Long:  `Runs a full interactive TUI wizard to install and configure the Omnideck container.`,
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installImageFlag, "image", "", "override container image (for testing alternate images)")
}

func runInstall(_ *cobra.Command, _ []string) error {
	if LoadedConfig != nil {
		fmt.Printf("\nExisting installation %s detected.\n\n",
			styles.Active.Render(LoadedConfig.ContainerName))

		if !promptYN("Install a new instance instead? [y/N]: ") {
			// Update the existing install.
			eng, err := engineFromConfig(LoadedConfig.Engine)
			if err != nil {
				return err
			}
			model := tui.NewUpdateModel(LoadedConfig, eng)
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			return err
		}

		fmt.Println()
		// Fall through to fresh wizard, pre-seeded with non-conflicting defaults.
		suggested := suggestNewInstanceDefaults()
		model := tui.NewInstallModel(ConfigPath, suggested, installImageFlag)
		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}

	model := tui.NewInstallModel(ConfigPath, nil, installImageFlag)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// suggestNewInstanceDefaults inspects existing instances and returns a Config
// pre-filled with a unique container name, data directories, and web UI port.
func suggestNewInstanceDefaults() *config.Config {
	instances, _ := config.ListInstances()

	// Collect names and ports already in use.
	takenNames := map[string]bool{}
	maxPort := 2337
	for _, inst := range instances {
		if inst.Config == nil {
			continue
		}
		takenNames[inst.Config.ContainerName] = true
		if p, err := strconv.Atoi(inst.Config.WebUIPortOrDefault()); err == nil && p >= maxPort {
			maxPort = p
		}
	}

	// Pick the first unused name: omnideck2, omnideck3, …
	name := "omnideck2"
	for i := 2; takenNames[name]; i++ {
		name = fmt.Sprintf("omnideck%d", i)
	}

	// Derive directory names from the numeric suffix (omnideck2 → Omnideck2).
	suffix := name[len("omnideck"):]
	dirBase := "Omnideck" + suffix

	home, _ := os.UserHomeDir()
	sharedDir := filepath.Join(home, dirBase)

	d := config.DefaultConfig()
	d.ContainerName = name
	d.SharedDir = sharedDir
	d.StateDir = filepath.Join(sharedDir, ".state")
	d.WebUIPort = strconv.Itoa(maxPort + 1)
	return d
}
