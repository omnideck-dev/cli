package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and edit saved configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the current configuration",
	RunE:  runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Update a single configuration value",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	RunE:  runConfigPath,
}

var validConfigKeys = []string{"home_volume", "state_volume", "shm_size"}

func init() {
	configCmd.AddCommand(configShowCmd, configSetCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig
	runtimeName := RuntimeName
	if runtimeName == "" {
		runtimeName = cfg.Engine
	}

	kv := lipgloss.NewStyle().Width(18)
	val := lipgloss.NewStyle()

	fmt.Println()
	fmt.Println(styles.Title.Render("  Omnideck Configuration"))
	fmt.Println("  " + styles.Dim.Render("─────────────────────────────"))
	fmt.Printf("  %s %s\n", kv.Render("container_name:"), val.Render(cfg.ContainerName))
	fmt.Printf("  %s %s\n", kv.Render("home_volume:"), val.Render(cfg.HomeVolumeName()))
	fmt.Printf("  %s %s\n", kv.Render("state_volume:"), val.Render(cfg.StateVolumeName()))
	fmt.Printf("  %s %s\n", kv.Render("shm_size:"), val.Render(cfg.ShmSize))
	fmt.Printf("  %s %s\n", kv.Render("runtime:"), val.Render(runtimeName))
	fmt.Printf("  %s %s\n", kv.Render("image:"), val.Render(cfg.Image))
	fmt.Printf("  %s %s\n", kv.Render("installed_at:"), val.Render(cfg.InstalledAt.Format("2006-01-02 15:04:05 UTC")))
	fmt.Println()
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	key, value := args[0], args[1]

	if key == "container_name" {
		return fmt.Errorf("an Omnideck name cannot be changed after setup; create another installation if you need a different name")
	}
	if !isValidConfigKey(key) {
		return fmt.Errorf("invalid key %q\nValid keys: %v", key, validConfigKeys)
	}
	if (key == "home_volume" || key == "state_volume") && value != "" && !checks.ValidContainerName(value) {
		return fmt.Errorf("%s must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens", key)
	}
	if key == "shm_size" && !checks.ValidMemorySize(value) {
		return fmt.Errorf("shm_size must be a number and unit, such as 512m or 2g")
	}

	cfg := LoadedConfig
	switch key {
	case "home_volume":
		cfg.HomeVolume = value
	case "state_volume":
		cfg.StateVolume = value
	case "shm_size":
		cfg.ShmSize = value
	}

	if err := config.Save(ConfigPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

func runConfigPath(_ *cobra.Command, _ []string) error {
	fmt.Println(ConfigPath)
	return nil
}

func isValidConfigKey(key string) bool {
	for _, k := range validConfigKeys {
		if k == key {
			return true
		}
	}
	return false
}
