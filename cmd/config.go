package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and edit saved settings",
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

var validConfigKeys = workflow.EditableSettingKeys

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
	fmt.Println(styles.Title.Render("  Omnideck Settings"))
	fmt.Println("  " + styles.Dim.Render("─────────────────────────────"))
	fmt.Printf("  %s %s\n", kv.Render("container_name:"), val.Render(cfg.ContainerName))
	fmt.Printf("  %s %s\n", kv.Render("home_volume:"), val.Render(cfg.HomeVolumeName()))
	fmt.Printf("  %s %s\n", kv.Render("state_volume:"), val.Render(cfg.StateVolumeName()))
	fmt.Printf("  %s %s\n", kv.Render("memory:"), val.Render(cfg.Memory))
	fmt.Printf("  %s %s\n", kv.Render("shm_size:"), val.Render(cfg.ShmSize))
	fmt.Printf("  %s %s\n", kv.Render("web_ui_port:"), val.Render(cfg.WebUIPortOrDefault()))
	fmt.Printf("  %s %s\n", kv.Render("runtime:"), val.Render(runtimeDisplayName(runtimeName)))
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
	candidate := *LoadedConfig
	if err := workflow.ApplySetting(&candidate, key, value); err != nil {
		return err
	}

	if err := config.Save(ConfigPath, &candidate); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	*LoadedConfig = candidate
	fmt.Printf("Set %s = %s\n", key, value)
	fmt.Printf("Run `omnideck update --name %s` to restart Omnideck with this setting.\n", candidate.ContainerName)
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
