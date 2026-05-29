package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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

var validConfigKeys = []string{"container_name", "shared_dir", "state_dir", "shm_size"}

func init() {
	configCmd.AddCommand(configShowCmd, configSetCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	requireConfigMulti()
	cfg := LoadedConfig

	kv := lipgloss.NewStyle().Width(18)
	val := lipgloss.NewStyle()

	fmt.Println()
	fmt.Println(styles.Title.Render("  Omnideck Configuration"))
	fmt.Println("  " + styles.Dim.Render("─────────────────────────────"))
	fmt.Printf("  %s %s\n", kv.Render("container_name:"), val.Render(cfg.ContainerName))
	fmt.Printf("  %s %s\n", kv.Render("shared_dir:"), val.Render(cfg.SharedDir))
	fmt.Printf("  %s %s\n", kv.Render("state_dir:"), val.Render(cfg.StateDir))
	fmt.Printf("  %s %s\n", kv.Render("shm_size:"), val.Render(cfg.ShmSize))
	fmt.Printf("  %s %s\n", kv.Render("engine:"), val.Render(cfg.Engine))
	fmt.Printf("  %s %s\n", kv.Render("image:"), val.Render(cfg.Image))
	fmt.Printf("  %s %s\n", kv.Render("installed_at:"), val.Render(cfg.InstalledAt.Format("2006-01-02 15:04:05 UTC")))
	fmt.Println()
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	requireConfigMulti()
	key, value := args[0], args[1]

	if !isValidConfigKey(key) {
		return fmt.Errorf("invalid key %q\nValid keys: %v", key, validConfigKeys)
	}

	cfg := LoadedConfig
	switch key {
	case "container_name":
		cfg.ContainerName = value
	case "shared_dir":
		cfg.SharedDir = value
	case "state_dir":
		cfg.StateDir = value
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
