package cmd

import (
	"fmt"
	"runtime"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var (
	installImageFlag  string
	installPlainFlag  bool
	installPortFlag   string
	installMemoryFlag string
	installShmFlag    string
	installEngineFlag string
)

var installCmd = &cobra.Command{
	Use:          "install",
	Short:        "Install Omnideck via interactive TUI wizard",
	SilenceUsage: true,
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
	installCmd.Flags().StringVar(&installEngineFlag, "engine", "", "container engine to use: docker or podman (default: auto-detect, prefers podman)")
}

func runInstall(_ *cobra.Command, _ []string) error {
	if installEngineFlag != "" && installEngineFlag != "docker" && installEngineFlag != "podman" {
		return fmt.Errorf("--engine must be docker or podman")
	}
	if installPlainFlag {
		return runInstallPlain()
	}

	instances, _ := config.ListInstances()
	model := tui.NewDashboardModelForInstall(nil, instances, installImageFlag, installEngineFlag)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// runInstallPlain performs a non-interactive install suitable for CI/CD and scripts.
// All settings come from flags or sensible defaults.
func runInstallPlain() error {
	probes := engine.ProbeAll()
	available := engine.ReadyEngines(probes)
	var eng engine.Engine
	for _, candidate := range available {
		if installEngineFlag == "" || candidate.Name() == installEngineFlag {
			eng = candidate
			break
		}
	}
	if eng == nil {
		printRuntimeSetupGuidanceFromProbes(installEngineFlag, probes)
		if installEngineFlag != "" {
			return fmt.Errorf("--engine %s is not ready; complete the setup option above", installEngineFlag)
		}
		return fmt.Errorf("neither Podman nor Docker is ready; complete one of the setup options above")
	}
	fmt.Printf("Engine: %s\n", eng.Name())
	for _, probe := range probes {
		if probe.Name == eng.Name() && probe.Warning != "" {
			fmt.Printf("Note: %s\n", probe.Warning)
		}
	}

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
	if installImageFlag != "" {
		cfg.Image = installImageFlag
	}

	savePath := config.InstancePath(cfg.ContainerName)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"Prepare space for your files", func() error { return eng.CreateVolume(cfg.HomeVolumeName()) }},
		{"Prepare space for Omnideck data", func() error { return eng.CreateVolume(cfg.StateVolumeName()) }},
		{"Check for an earlier Omnideck installation", func() error {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil || !exists {
				return nil
			}
			return eng.RemoveContainer(cfg.ContainerName)
		}},
		{"Download Omnideck", func() error {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(cfg.Image, msgs)
			close(msgs)
			return err
		}},
		{"Start Omnideck", func() error {
			return eng.RunContainer(engine.RunOptions{
				Name:        cfg.ContainerName,
				Image:       cfg.Image,
				Memory:      cfg.Memory,
				ShmSize:     cfg.ShmSize,
				HomeVolume:  cfg.HomeVolumeName(),
				StateVolume: cfg.StateVolumeName(),
				Restart:     "always",
				WebUIPort:   cfg.WebUIPortOrDefault(),
				Platform:    runtime.GOOS,
			})
		}},
		{"Remember these settings", func() error { return config.Save(savePath, cfg) }},
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

func printRuntimeSetupGuidanceFromProbes(preferred string, probes []engine.ProbeResult) {
	plans := engine.BuildSetupPlans(probes, engine.DetectHostPlatform())
	fmt.Println("Omnideck runs inside a container.")
	fmt.Println("The container keeps the agent and its software isolated from the rest of your system.")
	fmt.Println("Podman or Docker is needed to run the container. You only need one of them.")
	fmt.Println()
	for _, probe := range probes {
		if preferred == "" || probe.Name == preferred {
			fmt.Printf("  %-7s %s\n", probe.Name+":", engine.RuntimeStateLabel(probe.State))
		}
	}
	for _, plan := range plans {
		if preferred != "" && plan.Runtime != preferred {
			continue
		}
		recommended := ""
		if plan.Recommended || plan.Runtime == preferred {
			recommended = " (recommended)"
		}
		fmt.Printf("\n%s%s\n", plan.Action, recommended)
		fmt.Printf("  %s\n", plan.Description)
		if plan.Recommendation != "" {
			fmt.Printf("  Why: %s\n", plan.Recommendation)
		}
		fmt.Println("  What will happen:")
		for i, step := range plan.Steps {
			fmt.Printf("    %d. %s\n", i+1, step)
		}
		if plan.PermissionNote != "" {
			fmt.Printf("  Password information: %s\n", plan.PermissionNote)
		}
		for _, command := range plan.Commands {
			fmt.Printf("  Command: %s\n", command.Display)
		}
		if plan.URL != "" {
			fmt.Printf("  %s\n", plan.URL)
		}
		if plan.SafetyNote != "" {
			fmt.Printf("  %s\n", plan.SafetyNote)
		}
	}
	fmt.Println("\nFor guided setup, run `omnideck install` without --plain.")
	fmt.Println("After manual setup, run this command again.")
}

// suggestNewInstanceDefaults inspects existing instances and returns a Config
// pre-filled with a unique container name and web UI port.
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

	d := config.DefaultConfig()
	d.ContainerName = name
	d.WebUIPort = strconv.Itoa(maxPort + 1)
	return d
}
