package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/tui"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var (
	setupImageFlag   string
	setupPlainFlag   bool
	setupPortFlag    string
	setupMemoryFlag  string
	setupShmFlag     string
	setupRuntimeFlag string
	setupEngineFlag  string // hidden compatibility alias for --runtime
)

var setupCmd = &cobra.Command{
	Use:          "setup",
	Aliases:      []string{"install"},
	Short:        "Set up an Omnideck instance",
	SilenceUsage: true,
	Long: `Walks through setting up one Omnideck instance.

Use --plain for non-interactive / CI-CD setup:
  omnideck setup --plain --name omnideck --port 2337`,
	RunE: runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)
	setupCmd.Flags().StringVar(&setupImageFlag, "image", "", "advanced: use a different Omnideck container image")
	setupCmd.Flags().BoolVar(&setupPlainFlag, "plain", false, "non-interactive setup (no TUI) — for scripts and CI/CD")
	setupCmd.Flags().StringVar(&setupPortFlag, "port", "", "web UI host port (default: 2337)")
	setupCmd.Flags().StringVar(&setupMemoryFlag, "memory", "", "container memory limit (e.g. 2g)")
	setupCmd.Flags().StringVar(&setupShmFlag, "shm-size", "", "shared memory size (e.g. 1024m)")
	setupCmd.Flags().StringVar(&setupRuntimeFlag, "runtime", "", "override automatic container runtime selection: docker or podman (first setup only)")
	setupCmd.Flags().StringVar(&setupEngineFlag, "engine", "", "deprecated alias for --runtime")
	_ = setupCmd.Flags().MarkHidden("engine")
}

func runSetup(_ *cobra.Command, _ []string) error {
	requestedRuntime, err := setupRuntimeOverride(setupRuntimeFlag, setupEngineFlag)
	if err != nil {
		return err
	}
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	preferredEngine, err := setupRuntimePreference(RuntimeName, requestedRuntime, len(instances))
	if err != nil {
		return err
	}
	if setupPlainFlag {
		return runSetupPlain(preferredEngine, instances)
	}

	model := tui.NewAppModelForSetup(nil, instances, setupImageFlag, preferredEngine)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func setupRuntimeOverride(runtimeFlag, legacyEngineFlag string) (string, error) {
	if runtimeFlag != "" && legacyEngineFlag != "" && runtimeFlag != legacyEngineFlag {
		return "", fmt.Errorf("--runtime and the older --engine option disagree; use only --runtime")
	}
	requested := runtimeFlag
	if requested == "" {
		requested = legacyEngineFlag
	}
	if requested != "" && requested != "docker" && requested != "podman" {
		return "", fmt.Errorf("--runtime must be docker or podman")
	}
	return requested, nil
}

// runRuntimeSetup repairs container support for existing installations without
// creating another Omnideck instance.
func runRuntimeSetup(instances []config.InstanceInfo) error {
	preferredEngine := configuredEngineName(LoadedConfig, instances)
	model := tui.NewAppModelForRuntimeSetup(instances, preferredEngine)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// runSetupPlain performs non-interactive setup suitable for CI/CD and scripts.
// All settings come from flags or sensible defaults.
func runSetupPlain(preferredEngine string, instances []config.InstanceInfo) error {
	probes := engine.ProbeAll()
	selectedRuntime := preferredEngine
	if selectedRuntime == "" {
		selectedRuntime = engine.DefaultRuntimeForSetup(probes, engine.DetectHostPlatform())
	}
	available := engine.ReadyEngines(probes)
	var eng engine.Engine
	for _, candidate := range available {
		if selectedRuntime == "" || candidate.Name() == selectedRuntime {
			eng = candidate
			break
		}
	}
	if eng == nil {
		printRuntimeSetupGuidanceFromProbes(selectedRuntime, probes)
		if selectedRuntime != "" {
			return fmt.Errorf("%s is not ready; complete the setup option above", selectedRuntime)
		}
		return fmt.Errorf("neither Podman nor Docker is ready; complete one of the setup options above")
	}
	fmt.Printf("Runtime: %s\n", eng.Name())
	for _, probe := range probes {
		if probe.Name == eng.Name() && probe.Warning != "" {
			fmt.Printf("Note: %s\n", probe.Warning)
		}
	}

	cfg := workflow.NewInstanceDefaults(instances)
	if nameFlag != "" {
		cfg.ContainerName = nameFlag
	}
	if setupPortFlag != "" {
		cfg.WebUIPort = setupPortFlag
	}
	if setupMemoryFlag != "" {
		cfg.Memory = setupMemoryFlag
	}
	if setupShmFlag != "" {
		cfg.ShmSize = setupShmFlag
	}
	if setupImageFlag != "" {
		cfg.Image = setupImageFlag
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
		{"Start Omnideck", func() error { return eng.RunContainer(workflow.RunOptions(cfg)) }},
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
			return "", fmt.Errorf("Omnideck already uses %s for every installation on this computer; remove --runtime %s", saved, requested)
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
	if !checks.ValidMemorySize(cfg.Memory) {
		return fmt.Errorf("--memory must be a positive number and unit, such as 2g")
	}
	if !checks.ValidMemorySize(cfg.ShmSize) {
		return fmt.Errorf("--shm-size must be a positive number and unit, such as 512m")
	}
	if strings.TrimSpace(cfg.Image) == "" {
		return fmt.Errorf("--image cannot be empty")
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
