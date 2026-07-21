package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/omnideck-dev/cli/cmd/debug"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var (
	// version info set via SetVersion from main.go.
	versionStr = "dev"
	commitStr  = "none"
	dateStr    = "unknown"

	// Global flags.
	cfgPath   string
	nameFlag  string
	noColor   bool
	debugFlag bool

	// LoadedConfig is the config loaded in PersistentPreRun (may be nil).
	LoadedConfig *config.Config
	// ConfigPath is the resolved config file path used.
	ConfigPath string
	// RuntimeName is the runtime shared by every Omnideck instance. The Engine field on
	// older instance configs is used only as a migration fallback.
	RuntimeName string

	rootCmd = &cobra.Command{
		Use:   "omnideck",
		Short: "Set up and manage Omnideck",
		Long: `Omnideck runs as a container so the agent and its software stay isolated from the rest of your system.

Run omnideck without a command. It will open the right screen for first setup,
repair, or managing an existing installation.`,
		PersistentPreRun: persistentPreRun,
	}
)

// SetVersion is called from main.go with build-time values.
func SetVersion(v, c, d string) {
	versionStr = v
	commitStr = c
	dateStr = d
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "Explicit path to a config file")
	rootCmd.PersistentFlags().StringVarP(&nameFlag, "name", "n", "", "Instance name (e.g. omnideck, staging)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable color/style output")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print raw engine commands and output")
	rootCmd.Flags().Bool("version", false, "Print version and exit")

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			fmt.Printf("omnideck version %s (%s) built %s\n", versionStr, commitStr, dateStr)
			return nil
		}
		if isInteractive() {
			instances, listErr := config.ListInstances()
			instances = withLoadedInstance(instances, LoadedConfig, ConfigPath)
			probes := engine.ProbeAll()
			preferredEngine := configuredEngineName(LoadedConfig, instances)
			readyEngine := selectReadyEngine(engine.ReadyEngines(probes), preferredEngine)
			brokenIndex := firstBrokenInstance(readyEngine, instances)
			switch chooseInteractiveStart(LoadedConfig, instances, listErr, readyEngine != nil, brokenIndex >= 0) {
			case interactiveStartSetup:
				return runSetup(nil, nil)
			case interactiveStartRuntimeSetup:
				return runRuntimeSetup(instances)
			case interactiveStartDoctor:
				return runDashboardForDoctor(readyEngine, instances, brokenIndex)
			}
			return runDashboard(readyEngine, instances, LoadedConfig, ConfigPath)
		}
		return cmd.Help()
	}
}

type interactiveStart int

const (
	interactiveStartDashboard interactiveStart = iota
	interactiveStartSetup
	interactiveStartRuntimeSetup
	interactiveStartDoctor
)

// chooseInteractiveStart decides where a bare interactive command begins.
// Listing errors and legacy-only configs fall back to the dashboard so a
// filesystem or migration problem is not mistaken for a brand-new setup.
func chooseInteractiveStart(loaded *config.Config, instances []config.InstanceInfo, listErr error, runtimeReady, instanceBroken bool) interactiveStart {
	if listErr != nil {
		return interactiveStartDashboard
	}
	if loaded == nil && len(instances) == 0 {
		return interactiveStartSetup
	}
	if len(instances) > 0 && !runtimeReady {
		return interactiveStartRuntimeSetup
	}
	if instanceBroken {
		return interactiveStartDoctor
	}
	return interactiveStartDashboard
}

// containerLookup is the narrow runtime surface needed by entry routing.
type containerLookup interface {
	ContainerExists(name string) (bool, error)
}

// firstBrokenInstance returns the first saved instance whose container is
// missing or cannot be inspected. A stopped container still exists and is a
// normal dashboard state, not a repair state.
func firstBrokenInstance(eng containerLookup, instances []config.InstanceInfo) int {
	if eng == nil {
		return -1
	}
	for i, instance := range instances {
		if instance.Config == nil {
			continue
		}
		exists, err := eng.ContainerExists(instance.Config.ContainerName)
		if err != nil || !exists {
			return i
		}
	}
	return -1
}

func configuredEngineName(loaded *config.Config, instances []config.InstanceInfo) string {
	if RuntimeName != "" {
		return RuntimeName
	}
	if loaded != nil && loaded.Engine != "" {
		return loaded.Engine
	}
	for _, instance := range instances {
		if instance.Config != nil && instance.Config.Engine != "" {
			return instance.Config.Engine
		}
	}
	return ""
}

func selectReadyEngine(ready []engine.Engine, preferred string) engine.Engine {
	if preferred != "" {
		for _, candidate := range ready {
			if candidate.Name() == preferred {
				return candidate
			}
		}
		return nil
	}
	if len(ready) > 0 {
		return ready[0]
	}
	return nil
}

func runtimeDisplayName(name string) string {
	switch name {
	case "docker":
		return "Docker"
	case "podman":
		return "Podman"
	default:
		return name
	}
}

// engineFromConfig returns a ready engine based on the shared runtime choice.
func engineFromConfig(name string) (engine.Engine, error) {
	if RuntimeName != "" {
		name = RuntimeName
	}
	return readyEngineFromProbes(name, engine.ProbeAll())
}

func readyEngineFromProbes(name string, probes []engine.ProbeResult) (engine.Engine, error) {
	if candidate := selectReadyEngine(engine.ReadyEngines(probes), name); candidate != nil {
		return candidate, nil
	}
	if name != "" {
		for _, probe := range probes {
			if probe.Name == name {
				return nil, fmt.Errorf("%s: %s\nRun `omnideck` for guided setup", runtimeDisplayName(name), engine.RuntimeStateLabel(probe.State))
			}
		}
	}
	return nil, fmt.Errorf("Podman or Docker is not ready\nRun `omnideck` for guided setup")
}

// requireConfigMulti is like requireConfig but also handles the multiple-instance
// case: if multiple instances exist and no --name was given, it runs the
// interactive picker so the user can choose one. Returns errAborted if the
// user cancels the picker.
func requireConfigMulti() error {
	if LoadedConfig != nil {
		return nil
	}
	if cfgPath != "" {
		return fmt.Errorf("Omnideck could not load the config file %s; check that the path exists and that you can read it", cfgPath)
	}
	if nameFlag != "" {
		return fmt.Errorf("no Omnideck installation named %q was found\nRun `omnideck list` to see the available names", nameFlag)
	}
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	if len(instances) == 1 {
		LoadedConfig = instances[0].Config
		ConfigPath = instances[0].Path
		return nil
	}
	if len(instances) > 1 {
		if !isInteractive() {
			names := make([]string, 0, len(instances))
			for _, instance := range instances {
				names = append(names, instance.Name)
			}
			return fmt.Errorf("more than one Omnideck installation exists; choose one with --name\nAvailable names: %s", strings.Join(names, ", "))
		}
		chosen, ok := tui.RunPicker(
			"Select an instance",
			"Multiple instances found — which one?",
			instances,
		)
		if !ok {
			fmt.Println("Aborted.")
			return errAborted
		}
		LoadedConfig = chosen.Config
		ConfigPath = chosen.Path
		return nil
	}
	return fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")
}

// errAborted is returned when the user cancels an interactive prompt.
// Callers should return it as-is so Execute() can exit cleanly without
// printing a redundant error message.
var errAborted = fmt.Errorf("")

func persistentPreRun(_ *cobra.Command, _ []string) {
	styles.NoColor(noColor)
	debug.SetEnabled(debugFlag)
	LoadedConfig = nil
	ConfigPath = ""
	RuntimeName = ""
	if err := config.MigrateLegacyDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Omnideck could not copy settings from their previous location: %v\n", err)
	}
	if settings, err := config.LoadSettings(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Omnideck settings are unreadable: %v\n", err)
	} else {
		RuntimeName = settings.Runtime
	}
	instances, instanceListErr := config.ListInstances()
	if instanceListErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: Omnideck could not read its saved installations: %v\n", instanceListErr)
	}

	// Priority: --config > --name > auto-detect from instances dir > legacy path
	switch {
	case cfgPath != "":
		ConfigPath = cfgPath
		if cfg, err := config.Load(cfgPath); err == nil {
			LoadedConfig = cfg
		} else if _, statErr := os.Stat(cfgPath); statErr == nil {
			fmt.Fprintf(os.Stderr, "Warning: config file %s is unreadable: %v\n", cfgPath, err)
		}

	case nameFlag != "":
		path := config.InstancePath(nameFlag)
		ConfigPath = path
		if cfg, err := config.Load(path); err == nil {
			LoadedConfig = cfg
		} else if _, statErr := os.Stat(path); statErr == nil {
			fmt.Fprintf(os.Stderr, "Warning: config file %s is unreadable: %v\n", path, err)
		}

	default:
		// Try instances dir.
		switch len(instances) {
		case 1:
			LoadedConfig = instances[0].Config
			ConfigPath = instances[0].Path
		case 0:
			// Fall back to legacy single-file path.
			legacyPath := config.DefaultPath()
			ConfigPath = legacyPath
			if cfg, err := config.Load(legacyPath); err == nil {
				LoadedConfig = cfg
			} else if _, statErr := os.Stat(legacyPath); statErr == nil {
				fmt.Fprintf(os.Stderr, "Warning: config file %s is unreadable: %v\n", legacyPath, err)
			}
		default:
			// Multiple instances — leave LoadedConfig nil; commands that need
			// it will call requireConfigMulti() which prints a helpful error.
			ConfigPath = ""
		}
	}

	// Setup needs a sensible default path even if no instance exists yet.
	if ConfigPath == "" {
		ConfigPath = config.InstancePath("omnideck")
	}

	// Migrate the old per-instance runtime field when every existing config
	// agrees. Mixed legacy configs keep their old behavior until the user can
	// resolve them explicitly; Omnideck never silently changes their runtime.
	if RuntimeName == "" {
		if legacyRuntime, ok := oneLegacyRuntime(LoadedConfig, instances); ok {
			if err := config.SaveRuntime(legacyRuntime); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Omnideck could not save its runtime choice: %v\n", err)
			} else {
				RuntimeName = legacyRuntime
			}
		}
	}
}

func oneLegacyRuntime(loaded *config.Config, instances []config.InstanceInfo) (string, bool) {
	seen := map[string]bool{}
	if loaded != nil && config.ValidRuntime(loaded.Engine) {
		seen[loaded.Engine] = true
	}
	for _, instance := range instances {
		if instance.Config != nil && config.ValidRuntime(instance.Config.Engine) {
			seen[instance.Config.Engine] = true
		}
	}
	if len(seen) != 1 {
		return "", false
	}
	for name := range seen {
		return name, true
	}
	return "", false
}

// isInteractive reports whether stdin is an interactive terminal.
func isInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd())
}
