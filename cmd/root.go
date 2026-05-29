package cmd

import (
	"fmt"
	"os"
	"strings"

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

	rootCmd = &cobra.Command{
		Use:   "omnideck",
		Short: "Manage the Omnideck container application",
		Long: `omnideck installs and manages the Omnideck containerized AI application.
It wraps Docker/Podman operations with a polished TUI interface.`,
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
		return cmd.Help()
	}
}

// engineFromConfig returns an Engine based on the config engine name.
func engineFromConfig(name string) (engine.Engine, error) {
	switch name {
	case "docker":
		return &engine.DockerEngine{}, nil
	case "podman":
		return &engine.PodmanEngine{}, nil
	default:
		return engine.Detect()
	}
}

// requireConfig returns an error if no config was loaded.
// Commands that require an installed instance should call this.
func requireConfig() error {
	if LoadedConfig == nil {
		return fmt.Errorf("Omnideck is not installed.\nRun: omnideck install")
	}
	return nil
}

// requireConfigMulti is like requireConfig but also handles the multiple-instance
// case: if multiple instances exist and no --name was given, it runs the
// interactive picker so the user can choose one. Returns errAborted if the
// user cancels the picker.
func requireConfigMulti() error {
	if LoadedConfig != nil {
		return nil
	}
	instances, err := config.ListInstances()
	if err == nil && len(instances) > 1 {
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
	return fmt.Errorf("Omnideck is not installed.\nRun: omnideck install")
}

// errAborted is returned when the user cancels an interactive prompt.
// Callers should return it as-is so Execute() can exit cleanly without
// printing a redundant error message.
var errAborted = fmt.Errorf("")

func persistentPreRun(cmd *cobra.Command, args []string) {
	styles.NoColor(noColor)
	debug.SetEnabled(debugFlag)

	// Priority: --config > --name > auto-detect from instances dir > legacy path
	switch {
	case cfgPath != "":
		ConfigPath = cfgPath
		if cfg, err := config.Load(cfgPath); err == nil {
			LoadedConfig = cfg
		}

	case nameFlag != "":
		path := config.InstancePath(nameFlag)
		ConfigPath = path
		if cfg, err := config.Load(path); err == nil {
			LoadedConfig = cfg
		}

	default:
		// Try instances dir.
		instances, _ := config.ListInstances()
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
			}
		default:
			// Multiple instances — leave LoadedConfig nil; commands that need
			// it will call requireConfigMulti() which prints a helpful error.
			ConfigPath = ""
		}
	}

	// For install, ConfigPath needs a sensible default even if no instance yet.
	if ConfigPath == "" {
		ConfigPath = config.InstancePath("omnideck")
	}
}

// instanceName returns the short instance name derived from ConfigPath.
func instanceName() string {
	if nameFlag != "" {
		return nameFlag
	}
	base := strings.TrimSuffix(fmt.Sprintf("%s", ConfigPath), ".yaml")
	return fmt.Sprintf("%s", strings.TrimPrefix(base, config.InstancesDir()+"/"))
}
