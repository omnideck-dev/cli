package cmd

import (
	"context"
	"errors"
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
	setupImageFlag           string
	setupPlainFlag           bool
	setupPortFlag            string
	setupMemoryFlag          string
	setupShmFlag             string
	setupRuntimeFlag         string
	setupEngineFlag          string // hidden compatibility alias for --runtime
	setupSuggestDefaultsFlag bool
)

var setupCmd = &cobra.Command{
	Use:          "add",
	Aliases:      []string{"install", "setup"},
	Short:        "Add an Omnideck instance",
	SilenceUsage: true,
	Long: `Walks through setting up one Omnideck instance.

Use --plain for non-interactive / CI-CD setup:
  omnideck add --plain --name omnideck --port 2337`,
	RunE: runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)
	setupCmd.Flags().StringVar(&setupImageFlag, "image", "", "advanced: use a different Omnideck container image")
	setupCmd.Flags().BoolVar(&setupPlainFlag, "plain", false, "non-interactive setup (no TUI) — for scripts and CI/CD")
	setupCmd.Flags().StringVar(&setupPortFlag, "port", "", "web UI host port (default: 2337)")
	setupCmd.Flags().StringVar(&setupMemoryFlag, "memory", "", "container memory limit (e.g. 2g)")
	setupCmd.Flags().StringVar(&setupShmFlag, "shm-size", "", "shared memory size (e.g. 1024m)")
	setupCmd.Flags().StringVar(&setupRuntimeFlag, "runtime", "", "container runtime (Omnideck uses podman)")
	setupCmd.Flags().StringVar(&setupEngineFlag, "engine", "", "deprecated alias for --runtime")
	_ = setupCmd.Flags().MarkHidden("engine")
	setupCmd.Flags().BoolVar(&setupSuggestDefaultsFlag, "suggest-defaults", false, "print the next available name/port without creating anything (for --json)")
}

func runSetup(_ *cobra.Command, _ []string) error {
	if err := validateSetupRuntimeFlags(setupRuntimeFlag, setupEngineFlag); err != nil {
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeInternal, err.Error()))
		}
		return err
	}
	instances, err := config.ListInstances()
	if err != nil {
		wrapped := fmt.Errorf("reading saved Omnideck installations: %w", err)
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeInternal, wrapped.Error()))
		}
		return wrapped
	}

	if setupSuggestDefaultsFlag {
		defaults := workflow.NewInstanceDefaults(instances)
		if jsonFlag {
			writeJSON(suggestDefaultsPayload{Name: defaults.ContainerName, WebUIPort: defaults.WebUIPortOrDefault()})
			return nil
		}
		fmt.Printf("Next available name: %s\n", defaults.ContainerName)
		fmt.Printf("Next available port: %s\n", defaults.WebUIPortOrDefault())
		return nil
	}

	// --json implies the same non-interactive path as --plain — a caller
	// never has to remember to pass both. --plain remains independently
	// useful (plain text, no JSON) and may still be combined with --json.
	if setupPlainFlag || jsonFlag {
		if jsonFlag {
			return runSetupJSON(instances)
		}
		return runSetupPlain(instances)
	}

	model := tui.NewAppModelForSetup(nil, instances, setupImageFlag, resumeSetupFlag)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func validateSetupRuntimeFlags(runtimeFlag, legacyEngineFlag string) error {
	if runtimeFlag != "" && legacyEngineFlag != "" && runtimeFlag != legacyEngineFlag {
		return fmt.Errorf("--runtime and the older --engine option disagree; use only --runtime")
	}
	requested := runtimeFlag
	if requested == "" {
		requested = legacyEngineFlag
	}
	if requested == "docker" {
		return fmt.Errorf("--runtime docker is ignored; Omnideck uses podman")
	}
	if requested != "" && requested != "podman" {
		return fmt.Errorf("--runtime must be podman")
	}
	return nil
}

// runRuntimeSetup repairs container support for existing installations without
// creating another Omnideck instance.
func runRuntimeSetup(instances []config.InstanceInfo) error {
	model := tui.NewAppModelForRuntimeSetup(instances)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// selectSetupEngine returns the ready Podman engine used by both
// non-interactive setup paths.
func selectSetupEngine() (eng engine.Engine, probes []engine.ProbeResult, err error) {
	probes = engine.ProbeAll()
	for _, candidate := range engine.ReadyEngines(probes) {
		if candidate.Name() == "podman" {
			eng = candidate
			break
		}
	}
	if eng == nil {
		return nil, probes, fmt.Errorf("Podman is not ready; complete the setup option above")
	}
	return eng, probes, nil
}

// resolveSetupConfig builds the new instance's config from flags/defaults
// and validates it, matching runSetupPlain's original behavior exactly.
// Shared with runSetupJSON.
func resolveSetupConfig(eng engine.Engine, instances []config.InstanceInfo) (*config.Config, error) {
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
			return nil, err
		}
		cfg.ContainerName = availableName
	}
	if err := validatePlainSetup(cfg, eng); err != nil {
		return nil, err
	}
	return cfg, nil
}

// createStageLabel gives each workflow.CreateInstance stage the same
// plain-language phrasing runRemovePlain/runUpdatePlain use for their steps.
func createStageLabel(stage string) string {
	switch stage {
	case "check_availability":
		return "Check that the name and browser address are available"
	case "create_home_volume":
		return "Prepare space for your files"
	case "create_state_volume":
		return "Prepare space for Omnideck data"
	case "pull_image":
		return "Download Omnideck"
	case "run_container":
		return "Start Omnideck"
	case "save_config":
		return "Remember these settings"
	default:
		return stage
	}
}

// runSetupPlain performs non-interactive setup suitable for CI/CD and
// scripts. All settings come from flags or sensible defaults. It shares
// workflow.CreateInstance with runSetupStepsJSON so the two non-interactive
// paths can never disagree about what creating an instance does or what
// happens when it fails partway through.
func runSetupPlain(instances []config.InstanceInfo) error {
	eng, probes, err := selectSetupEngine()
	if err != nil {
		printRuntimeSetupGuidanceFromProbes(probes)
		return err
	}
	fmt.Printf("Runtime: %s\n", eng.Name())
	for _, probe := range probes {
		if probe.Name == eng.Name() && probe.Warning != "" {
			fmt.Printf("Note: %s\n", probe.Warning)
		}
	}

	cfg, err := resolveSetupConfig(eng, instances)
	if err != nil {
		return err
	}

	savePath := config.InstancePath(cfg.ContainerName)

	lastStage := ""
	opts := workflow.CreateInstanceOptions{
		OnStage: func(stage string) {
			if lastStage != "" {
				fmt.Println("ok")
			}
			fmt.Printf("  → %s... ", createStageLabel(stage))
			lastStage = stage
		},
	}
	if err := workflow.CreateInstance(eng, cfg, func() error {
		return saveInstalledConfig(savePath, cfg, eng.Name())
	}, opts); err != nil {
		fmt.Printf("FAILED\n    %v\n", err)
		return err
	}
	fmt.Println("ok")

	fmt.Printf("\n✓  Omnideck is ready: http://localhost:%s\n", cfg.WebUIPortOrDefault())
	return nil
}

// suggestDefaultsPayload is "add --suggest-defaults --json"'s shape.
type suggestDefaultsPayload struct {
	Name      string `json:"name"`
	WebUIPort string `json:"webUiPort"`
}

// runSetupJSON performs the same non-interactive setup as runSetupPlain but
// emits NDJSON progress instead of printing text. The final "complete" event
// carries a status --json-shaped result.
//
// On cancellation (SIGINT/SIGTERM) before the new instance's config has been
// saved, workflow.CreateInstance best-effort removes whatever
// container/volumes this run already created — nothing else will ever
// discover them otherwise, since list/doctor/remove are all keyed off the
// saved config file that a cancelled run never gets to write. See
// JSON_MODE_SPEC.md's "Cancellation and teardown".
func runSetupJSON(instances []config.InstanceInfo) error {
	eng, _, err := selectSetupEngine()
	if err != nil {
		return writeJSONError(newJSONError(ErrCodeEngineNotFound, err.Error()))
	}

	cfg, err := resolveSetupConfig(eng, instances)
	if err != nil {
		return writeJSONError(newJSONError(ErrCodeInternal, err.Error()))
	}

	return runSetupStepsJSON(eng, cfg, config.InstancePath(cfg.ContainerName))
}

// runSetupStepsJSON runs the actual create-volumes/pull/run/save sequence
// once the engine and instance config are already resolved, sharing
// workflow.CreateInstance with runSetupPlain. Split out from runSetupJSON so
// the NDJSON/cancellation-cleanup logic is unit testable against a mock
// engine without needing a real container runtime for engine selection.
func runSetupStepsJSON(eng engine.Engine, cfg *config.Config, savePath string) error {
	nd := newNDJSONEncoder()

	lastStage := ""
	opts := workflow.CreateInstanceOptions{
		OnStage: func(stage string) {
			if lastStage != "" {
				nd.done(lastStage)
			}
			nd.start(stage)
			lastStage = stage
		},
		OnPullProgress: func(line string) {
			// PullImage already streams every raw Podman pull line;
			// forward it as progress detail instead of discarding it the way
			// runSetupPlain's text path does — a multi-minute pull otherwise
			// looks identical to a hang.
			nd.progress("pull_image", line)
		},
	}

	err := workflow.CreateInstance(eng, cfg, func() error {
		return saveInstalledConfig(savePath, cfg, eng.Name())
	}, opts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nd.fail(lastStage, newJSONError(ErrCodeCancelled, err.Error()))
		}
		return nd.fail(lastStage, environmentStageJSONError(lastStage, err))
	}
	nd.done(lastStage)

	if nd.broken() {
		return errAborted
	}
	nd.complete(gatherStatusPayload(cfg, eng))
	if nd.broken() {
		return errAborted
	}
	return nil
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

func printRuntimeSetupGuidanceFromProbes(probes []engine.ProbeResult) {
	plans := engine.BuildSetupPlans(probes, engine.DetectHostPlatform())
	fmt.Println("Omnideck runs inside a container.")
	fmt.Println("The container keeps the agent and its software isolated from the rest of your system.")
	fmt.Println("Podman runs that container and is the only runtime Omnideck uses.")
	fmt.Println()
	for _, probe := range probes {
		if probe.Name == "podman" {
			fmt.Printf("  %-7s %s\n", probe.Name+":", engine.RuntimeStateLabel(probe.State))
		}
	}
	for _, plan := range plans {
		recommended := ""
		if plan.Recommended {
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
