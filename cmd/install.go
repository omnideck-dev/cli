package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var (
	installImageFlag   string
	installPlainFlag   bool
	installPortFlag    string
	installMemoryFlag  string
	installShmFlag     string
	installSharedFlag  string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Omnideck via interactive TUI wizard",
	Long: `Runs a full interactive TUI wizard to install and configure the Omnideck container.

Use --plain for non-interactive / CI-CD installs:
  omnideck install --plain --name omnideck --port 2337`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installImageFlag, "image", "", "override container image")
	installCmd.Flags().BoolVar(&installPlainFlag, "plain", false, "non-interactive install (no TUI) — for scripts and CI/CD")
	installCmd.Flags().StringVar(&installPortFlag, "port", "", "web UI host port (default: 2337)")
	installCmd.Flags().StringVar(&installMemoryFlag, "memory", "", "container memory limit (e.g. 2g)")
	installCmd.Flags().StringVar(&installShmFlag, "shm-size", "", "shared memory size (e.g. 1024m)")
	installCmd.Flags().StringVar(&installSharedFlag, "shared-dir", "", "host shared directory path")
}

func runInstall(_ *cobra.Command, _ []string) error {
	if installPlainFlag {
		return runInstallPlain()
	}

	instances, _ := config.ListInstances()

	if LoadedConfig != nil {
		fmt.Printf("\nExisting installation %s detected.\n\n",
			styles.Active.Render(LoadedConfig.ContainerName))

		if !promptYN("Install a new instance instead? [y/N]: ") {
			fmt.Println()
			eng, err := engineFromConfig(LoadedConfig.Engine)
			if err != nil {
				return err
			}
			selectedIdx := 0
			for i, inst := range instances {
				if inst.Config != nil && inst.Config.ContainerName == LoadedConfig.ContainerName {
					selectedIdx = i
					break
				}
			}
			model := tui.NewDashboardModelForUpdate(eng, instances, LoadedConfig, selectedIdx)
			p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = p.Run()
			return err
		}

		fmt.Println()
	}

	eng, _ := engine.Detect()
	model := tui.NewDashboardModelForInstall(eng, instances, installImageFlag)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// runInstallPlain performs a non-interactive install suitable for CI/CD and scripts.
// All settings come from flags or sensible defaults.
func runInstallPlain() error {
	eng, err := engine.Detect()
	if err != nil {
		return fmt.Errorf("no container engine found: %w", err)
	}
	fmt.Printf("Engine: %s\n", eng.Name())

	cfg := config.DefaultConfig()
	if nameFlag != "" {
		cfg.ContainerName = nameFlag
	}
	if installPortFlag != "" {
		cfg.WebUIPort = installPortFlag
	}
	if installMemoryFlag != "" {
		cfg.Memory = installMemoryFlag
	}
	if installShmFlag != "" {
		cfg.ShmSize = installShmFlag
	}
	if installSharedFlag != "" {
		cfg.SharedDir = installSharedFlag
		cfg.StateDir = installSharedFlag + "/.state"
	}
	if installImageFlag != "" {
		cfg.Image = installImageFlag
	}

	savePath := config.InstancePath(cfg.ContainerName)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"Create shared directory", func() error { return os.MkdirAll(cfg.SharedDir, 0o755) }},
		{"Create state directory", func() error { return os.MkdirAll(cfg.StateDir, 0o755) }},
		{"Remove existing container", func() error {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil || !exists {
				return nil
			}
			return eng.RemoveContainer(cfg.ContainerName)
		}},
		{"Pull image", func() error {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(cfg.Image, msgs)
			close(msgs)
			return err
		}},
		{"Run container", func() error {
			return eng.RunContainer(engine.RunOptions{
				Name:      cfg.ContainerName,
				Image:     cfg.Image,
				Memory:    cfg.Memory,
				ShmSize:   cfg.ShmSize,
				SharedDir: cfg.SharedDir,
				StateDir:  cfg.StateDir,
				Restart:   "always",
				WebUIPort: cfg.WebUIPortOrDefault(),
				Platform:  runtime.GOOS,
			})
		}},
		{"Save configuration", func() error { return config.Save(savePath, cfg) }},
	}

	for _, step := range steps {
		fmt.Printf("  → %s... ", step.label)
		if err := step.fn(); err != nil {
			fmt.Printf("FAILED\n    %v\n", err)
			return err
		}
		fmt.Println("ok")
	}

	fmt.Printf("\n✓  Omnideck installed: http://localhost:%s\n", cfg.WebUIPortOrDefault())
	return nil
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
