package cmd

import (
	"fmt"
	"runtime"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
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
	Use:          "setup",
	Aliases:      []string{"install"},
	Short:        "Set up an Omnideck instance",
	SilenceUsage: true,
	Long: `Walks through setting up one Omnideck instance.

Use --plain for non-interactive / CI-CD setup:
  omnideck setup --plain --name omnideck --port 2337`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installImageFlag, "image", "", "override container image")
	installCmd.Flags().BoolVar(&installPlainFlag, "plain", false, "non-interactive setup (no TUI) — for scripts and CI/CD")
	installCmd.Flags().StringVar(&installPortFlag, "port", "", "web UI host port (default: 2337)")
	installCmd.Flags().StringVar(&installMemoryFlag, "memory", "", "container memory limit (e.g. 2g)")
	installCmd.Flags().StringVar(&installShmFlag, "shm-size", "", "shared memory size (e.g. 1024m)")
	installCmd.Flags().StringVar(&installEngineFlag, "engine", "", "runtime for the first setup: docker or podman (later instances reuse it)")
}

func runInstall(_ *cobra.Command, _ []string) error {
	if installEngineFlag != "" && installEngineFlag != "docker" && installEngineFlag != "podman" {
		return fmt.Errorf("--engine must be docker or podman")
	}
	instances, _ := config.ListInstances()
	preferredEngine, err := setupRuntimePreference(RuntimeName, installEngineFlag, len(instances))
	if err != nil {
		return err
	}
	if installPlainFlag {
		return runInstallPlain(preferredEngine)
	}

	model := tui.NewDashboardModelForInstall(nil, instances, installImageFlag, preferredEngine)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// runRuntimeSetup repairs container support for existing installations without
// creating another Omnideck instance.
func runRuntimeSetup(instances []config.InstanceInfo) error {
	preferredEngine := configuredEngineName(LoadedConfig, instances)
	model := tui.NewDashboardModelForRuntimeSetup(instances, preferredEngine)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// runInstallPlain performs a non-interactive install suitable for CI/CD and scripts.
// All settings come from flags or sensible defaults.
func runInstallPlain(preferredEngine string) error {
	probes := engine.ProbeAll()
	available := engine.ReadyEngines(probes)
	var eng engine.Engine
	for _, candidate := range available {
		if preferredEngine == "" || candidate.Name() == preferredEngine {
			eng = candidate
			break
		}
	}
	if eng == nil {
		printRuntimeSetupGuidanceFromProbes(preferredEngine, probes)
		if preferredEngine != "" {
			return fmt.Errorf("%s is not ready; complete the setup option above", preferredEngine)
		}
		return fmt.Errorf("neither Podman nor Docker is ready; complete one of the setup options above")
	}
	fmt.Printf("Runtime: %s\n", eng.Name())
	for _, probe := range probes {
		if probe.Name == eng.Name() && probe.Warning != "" {
			fmt.Printf("Note: %s\n", probe.Warning)
		}
	}

	cfg := suggestNewInstanceDefaults()
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
	if nameFlag == "" {
		availableName, err := suggestAvailableRuntimeName(cfg.ContainerName, eng)
		if err != nil {
			return err
		}
		cfg.ContainerName = availableName
	}
	if err := validatePlainSetup(cfg, eng); err != nil {
		return err
	}

	savePath := config.InstancePath(cfg.ContainerName)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"Check that the name and browser address are available", func() error {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil {
				return fmt.Errorf("checking the name %q: %w", cfg.ContainerName, err)
			}
			if exists {
				return fmt.Errorf("another container already uses the name %q; choose a different --name", cfg.ContainerName)
			}
			if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
				return fmt.Errorf("another app is already using browser address number %s; choose a different --port", cfg.WebUIPortOrDefault())
			}
			return nil
		}},
		{"Prepare space for your files", func() error { return eng.CreateVolume(cfg.HomeVolumeName()) }},
		{"Prepare space for Omnideck data", func() error { return eng.CreateVolume(cfg.StateVolumeName()) }},
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
		{"Remember these settings", func() error { return saveInstalledConfig(savePath, cfg, eng.Name()) }},
	}

	for _, step := range steps {
		fmt.Printf("  → %s... ", step.label)
		if err := step.fn(); err != nil {
			fmt.Printf("FAILED\n    %v\n", err)
			return err
		}
		fmt.Println("ok")
	}

	fmt.Printf("\n✓  Omnideck is ready: http://localhost:%s\n", cfg.WebUIPortOrDefault())
	return nil
}

// setupRuntimePreference keeps one runtime shared by existing instances while
// allowing a genuinely fresh setup to choose again. A settings file can remain
// after the final instance is removed, so it must not hide the other option.
func setupRuntimePreference(saved, requested string, instanceCount int) (string, error) {
	if instanceCount > 0 {
		if saved != "" && requested != "" && requested != saved {
			return "", fmt.Errorf("Omnideck already uses %s for every installation on this computer; remove --engine %s", saved, requested)
		}
		if saved != "" {
			return saved, nil
		}
	}
	if requested != "" {
		return requested, nil
	}
	return "", nil
}

// saveInstalledConfig records the machine-wide runtime and the new instance.
func saveInstalledConfig(path string, cfg *config.Config, engineName string) error {
	if err := config.SaveRuntime(engineName); err != nil {
		return err
	}
	cfg.Engine = ""
	cfg.InstalledAt = time.Now()
	return config.Save(path, cfg)
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
	fmt.Println("\nFor guided setup, run `omnideck setup` without --plain.")
	fmt.Println("After manual setup, run this command again.")
}

// suggestNewInstanceDefaults inspects existing instances and returns a Config
// pre-filled with a unique container name and web UI port.
func suggestNewInstanceDefaults() *config.Config {
	instances, _ := config.ListInstances()

	// Collect names and ports already in use.
	takenNames := map[string]bool{}
	takenPorts := map[string]bool{}
	maxPort := 2336
	for _, inst := range instances {
		if inst.Config == nil {
			continue
		}
		takenNames[inst.Config.ContainerName] = true
		takenPorts[inst.Config.WebUIPortOrDefault()] = true
		if p, err := strconv.Atoi(inst.Config.WebUIPortOrDefault()); err == nil && p >= maxPort {
			maxPort = p
		}
	}

	name := "omnideck"
	if takenNames[name] {
		for i := 2; ; i++ {
			name = fmt.Sprintf("omnideck%d", i)
			if !takenNames[name] {
				break
			}
		}
	}

	d := config.DefaultConfig()
	d.ContainerName = name
	if port, ok := checks.NextAvailablePort(maxPort+1, takenPorts); ok {
		d.WebUIPort = port
	}
	return d
}

func suggestAvailableRuntimeName(initial string, eng engine.Engine) (string, error) {
	instances, err := config.ListInstances()
	if err != nil {
		return "", fmt.Errorf("checking existing installations: %w", err)
	}
	reserved := map[string]bool{}
	for _, instance := range instances {
		if instance.Config != nil {
			reserved[instance.Config.ContainerName] = true
		}
	}
	for suffix := 1; suffix < 10000; suffix++ {
		candidate := initial
		if suffix > 1 {
			candidate = fmt.Sprintf("omnideck%d", suffix)
		}
		if reserved[candidate] {
			continue
		}
		exists, err := eng.ContainerExists(candidate)
		if err != nil {
			return "", fmt.Errorf("checking the name %q: %w", candidate, err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find an available Omnideck name")
}

func validatePlainSetup(cfg *config.Config, eng engine.Engine) error {
	if !checks.ValidContainerName(cfg.ContainerName) {
		return fmt.Errorf("--name must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens")
	}
	if !checks.ValidPort(cfg.WebUIPortOrDefault()) {
		return fmt.Errorf("--port must be a number between 1 and 65535")
	}
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("checking existing installations: %w", err)
	}
	for _, instance := range instances {
		if instance.Config == nil {
			continue
		}
		if instance.Config.ContainerName == cfg.ContainerName {
			return fmt.Errorf("an Omnideck installation named %q already exists; choose a different --name", cfg.ContainerName)
		}
		if instance.Config.WebUIPortOrDefault() == cfg.WebUIPortOrDefault() {
			return fmt.Errorf("another Omnideck installation already uses port %s; choose a different --port", cfg.WebUIPortOrDefault())
		}
	}
	exists, err := eng.ContainerExists(cfg.ContainerName)
	if err != nil {
		return fmt.Errorf("checking the name %q: %w", cfg.ContainerName, err)
	}
	if exists {
		return fmt.Errorf("another container already uses the name %q; choose a different --name", cfg.ContainerName)
	}
	if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
		return fmt.Errorf("another app is already using port %s; choose a different --port", cfg.WebUIPortOrDefault())
	}
	return nil
}
