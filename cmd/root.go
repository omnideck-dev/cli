package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

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
	cfgPath         string
	nameFlag        string
	noColor         bool
	debugFlag       bool
	jsonFlag        bool
	resumeSetupFlag bool

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

// Execute runs the root command. It wires a context tied to SIGINT/SIGTERM
// that every engine-invoked subprocess is built with (see
// engine.SetCancelContext), so a killed omnideck process also kills any
// Podman subprocess it was waiting on instead of orphaning it.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine.SetCancelContext(ctx)
	jsonRequested := argsRequestJSON(os.Args[1:])
	if jsonRequested {
		// Flag parsing, command lookup, and positional-argument validation can
		// fail before PersistentPreRun. Silence Cobra up front so those failures
		// can use the same JSON-only process boundary as command errors.
		rootCmd.SilenceErrors = true
		rootCmd.SilenceUsage = true
	}
	if err := rootCmd.Execute(); err != nil {
		if jsonRequested && !errors.Is(err, errAborted) {
			_ = writeJSONError(newJSONError(ErrCodeInternal, err.Error()))
		}
		os.Exit(1)
	}
}

// argsRequestJSON detects the persistent JSON flag before Cobra parses the
// command line. It deliberately stops at "--" so a positional argument named
// --json cannot change error rendering.
func argsRequestJSON(args []string) bool {
	enabled := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			enabled = true
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			parsed, err := strconv.ParseBool(value)
			if err == nil {
				enabled = parsed
			}
		}
	}
	return enabled
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "Explicit path to a config file")
	rootCmd.PersistentFlags().StringVarP(&nameFlag, "name", "n", "", "Instance name (e.g. omnideck, staging)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable color/style output")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print raw container runtime commands and output")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Emit machine-readable JSON/NDJSON instead of styled output; never prompts or launches a TUI (see JSON_MODE_SPEC.md)")
	rootCmd.Flags().Bool("version", false, "Print version and exit")
	rootCmd.Flags().BoolVar(&resumeSetupFlag, "resume-setup", false, "continue setup after a Windows restart")
	_ = rootCmd.Flags().MarkHidden("resume-setup")

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			if jsonFlag {
				writeJSON(versionPayload{
					Version:      versionStr,
					Commit:       commitStr,
					Date:         dateStr,
					JSONContract: jsonContract,
				})
				return nil
			}
			fmt.Printf("omnideck version %s (%s) built %s\n", versionStr, commitStr, dateStr)
			return nil
		}
		if jsonFlag {
			// --json must never fall through to a TUI or plain-text help,
			// regardless of what stdin looks like — see JSON_MODE_SPEC.md's
			// "Bare omnideck --json (no subcommand)".
			return writeJSONError(newJSONError(ErrCodeMissingSubcommand, "omnideck requires a subcommand in --json mode; run --help for the list"))
		}
		// RunOnce may start the CLI without an inherited terminal handle. The
		// private resume flag still routes through the normal bare-command
		// detection so first setup and runtime repair continue in the right mode.
		if isInteractive() || resumeSetupFlag {
			instances, listErr := config.ListInstances()
			instances = withLoadedInstance(instances, LoadedConfig, ConfigPath)
			probes := engine.ProbeAll()
			readyEngine := selectReadyEngine(engine.ReadyEngines(probes))
			brokenIndex := firstBrokenInstance(readyEngine, instances)
			switch chooseInteractiveStart(LoadedConfig, instances, listErr, readyEngine != nil, brokenIndex >= 0) {
			case interactiveStartSetup:
				return runSetup(nil, nil)
			case interactiveStartRuntimeSetup:
				return runRuntimeSetup(instances)
			case interactiveStartDoctor:
				return runAppForDoctor(readyEngine, instances, brokenIndex)
			}
			return runApp(readyEngine, instances, LoadedConfig, ConfigPath)
		}
		return cmd.Help()
	}
}

// versionPayload is the --version --json shape.
type versionPayload struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Date         string `json:"date"`
	JSONContract int    `json:"jsonContract"`
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

func selectReadyEngine(ready []engine.Engine) engine.Engine {
	if len(ready) > 0 {
		return ready[0]
	}
	return nil
}

func runtimeDisplayName(name string) string {
	if name == "podman" {
		return "Podman"
	}
	return name
}

// detectReadyEngine returns Podman when the shared runtime is ready. Legacy
// runtime names in saved configuration are intentionally ignored.
func detectReadyEngine() (engine.Engine, error) {
	return readyEngineFromProbes(engine.ProbeAll())
}

func readyEngineFromProbes(probes []engine.ProbeResult) (engine.Engine, error) {
	if candidate := selectReadyEngine(engine.ReadyEngines(probes)); candidate != nil {
		return candidate, nil
	}
	for _, probe := range probes {
		if probe.Name == "podman" {
			return nil, fmt.Errorf("Podman: %s\nRun `omnideck` for guided setup", engine.RuntimeStateLabel(probe.State))
		}
	}
	return nil, fmt.Errorf("Podman is not ready\nRun `omnideck` for guided setup")
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
		err := fmt.Errorf("Omnideck could not load the config file %s; check that the path exists and that you can read it", cfgPath)
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeNotInstalled, err.Error()))
		}
		return err
	}
	if nameFlag != "" {
		err := fmt.Errorf("no Omnideck installation named %q was found\nRun `omnideck list` to see the available names", nameFlag)
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeNotInstalled, err.Error()))
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
	if len(instances) == 1 {
		LoadedConfig = instances[0].Config
		ConfigPath = instances[0].Path
		return nil
	}
	if len(instances) > 1 {
		names := make([]string, 0, len(instances))
		for _, instance := range instances {
			names = append(names, instance.Name)
		}
		// --json never opens the interactive picker, regardless of whether
		// stdin looks like a terminal — see JSON_MODE_SPEC.md's
		// requireConfigMulti() bypass.
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeAmbiguousInstance, "Multiple instances found — specify --name").withInstances(names))
		}
		if !isInteractive() {
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
	if jsonFlag {
		return writeJSONError(newJSONError(ErrCodeNotInstalled, "Omnideck is not set up. Run: omnideck add"))
	}
	return fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")
}

// errAborted is returned when the user cancels an interactive prompt.
// Callers should return it as-is so Execute() can exit cleanly without
// printing a redundant error message.
var errAborted = fmt.Errorf("")

func persistentPreRun(cmd *cobra.Command, _ []string) {
	// JSON is a complete process contract on stdout. Cobra's default error
	// handling would otherwise append an empty "Error:" and, for some commands,
	// the full usage text to stderr after a structured JSON error was emitted.
	// Reset both fields on every invocation because command objects are reused by
	// tests in this package.
	cmd.Root().SilenceErrors = jsonFlag
	cmd.Root().SilenceUsage = jsonFlag
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

	// Migrate the old per-instance Podman field when it is present. Unsupported
	// legacy runtime names are ignored rather than influencing runtime choice.
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
	if loaded != nil && config.ValidRuntime(loaded.Engine) {
		return loaded.Engine, true
	}
	for _, instance := range instances {
		if instance.Config != nil && config.ValidRuntime(instance.Config.Engine) {
			return instance.Config.Engine, true
		}
	}
	return "", false
}

// isInteractive reports whether stdin is an interactive terminal.
func isInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd())
}
