package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage Omnideck instances",
}

var instanceRemoveCmd = &cobra.Command{
	Use:          "remove NAME",
	Short:        "Remove one Omnideck instance",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runInstanceRemove,
}

func init() {
	rootCmd.AddCommand(instanceCmd)
	instanceCmd.AddCommand(instanceRemoveCmd)
}

func runInstanceRemove(_ *cobra.Command, args []string) error {
	instance, err := savedInstanceNamed(args[0])
	if err != nil {
		return err
	}
	eng, err := engineFromConfig(instance.Config.Engine)
	if err != nil {
		return fmt.Errorf("the container runtime must be running before this instance can be removed: %w", err)
	}

	fmt.Printf("\nRemove instance %s\n\n", styles.Active.Render(instance.Name))
	fmt.Println("Omnideck will stop and remove this instance and forget its saved settings.")
	fmt.Println("The Omnideck CLI and your container runtime will stay installed.")
	fmt.Println("Saved data is kept unless you explicitly choose to permanently delete it.")

	prompts := bufio.NewScanner(os.Stdin)
	if !promptYesNo(prompts, "\nRemove this instance? [y/N]: ", false) {
		fmt.Println("Nothing was changed.")
		return nil
	}

	deleteData := promptYesNo(prompts, "Also permanently delete this instance's saved data? [y/N]: ", false)
	backupData := false
	if deleteData {
		backupData = promptYesNo(prompts, "Create a backup before deleting the data? [Y/n]: ", true)
		fmt.Printf("Type %s to confirm permanent data deletion: ", instance.Name)
		if !prompts.Scan() || strings.TrimSpace(prompts.Text()) != instance.Name {
			fmt.Println("\nThe name did not match. Nothing was changed.")
			return nil
		}
	}

	fmt.Println("\nRemoving the instance safely…")
	result, err := workflow.RemoveInstance(eng, instance, workflow.RemoveInstanceOptions{
		DeleteData: deleteData,
		BackupData: backupData,
	})
	if err != nil {
		if result.BackupPath != "" {
			fmt.Printf("Backup created before the problem occurred: %s\n", styles.Active.Render(result.BackupPath))
		}
		return err
	}

	fmt.Println(styles.Success.Render("✓ Instance removed"))
	if deleteData {
		fmt.Println("  Its saved data was permanently deleted.")
		if result.BackupPath != "" {
			fmt.Printf("  Backup: %s\n", styles.Active.Render(result.BackupPath))
		}
	} else {
		fmt.Printf("  Saved data was kept in %s and %s.\n", instance.Config.HomeVolumeName(), instance.Config.StateVolumeName())
	}
	return nil
}

func savedInstanceNamed(name string) (config.InstanceInfo, error) {
	instances, err := config.ListInstances()
	if err != nil {
		return config.InstanceInfo{}, fmt.Errorf("reading saved Omnideck instances: %w", err)
	}
	for _, instance := range instances {
		if instance.Name == name {
			return instance, nil
		}
	}
	return config.InstanceInfo{}, fmt.Errorf("no Omnideck instance named %q was found; run `omnideck list` to see saved instances", name)
}

func promptYesNo(scanner *bufio.Scanner, prompt string, defaultYes bool) bool {
	fmt.Print(prompt)
	if !scanner.Scan() {
		return defaultYes
	}
	answer := strings.TrimSpace(scanner.Text())
	if answer == "" {
		return defaultYes
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

func instanceNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}
