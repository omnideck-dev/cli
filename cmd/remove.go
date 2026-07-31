package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var (
	removeYesFlag           bool
	removeKeepVolumesFlag   bool
	removeDeleteVolumesFlag bool
	removeBackupFlag        bool
	removeNoBackupFlag      bool
	removePlainFlag         bool
)

var removeCmd = &cobra.Command{
	Use:          "remove NAME",
	Aliases:      []string{"uninstall"},
	Short:        "Remove one Omnideck instance",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runInstanceRemove,
}

func init() {
	removeCmd.Flags().BoolVar(&removeYesFlag, "yes", false, "skip the confirmation prompt (required for --plain/--json)")
	removeCmd.Flags().BoolVar(&removeKeepVolumesFlag, "keep-volumes", false, "keep this instance's saved data volumes (required for --plain/--json; mutually exclusive with --delete-volumes)")
	removeCmd.Flags().BoolVar(&removeDeleteVolumesFlag, "delete-volumes", false, "permanently delete this instance's saved data volumes (mutually exclusive with --keep-volumes)")
	removeCmd.Flags().BoolVar(&removeBackupFlag, "backup", false, "back up data volumes before deleting them (required with --delete-volumes; mutually exclusive with --no-backup)")
	removeCmd.Flags().BoolVar(&removeNoBackupFlag, "no-backup", false, "skip the backup when deleting data volumes (mutually exclusive with --backup)")
	removeCmd.Flags().BoolVar(&removePlainFlag, "plain", false, "non-interactive removal (no prompts) — for scripts and CI/CD")
	rootCmd.AddCommand(removeCmd)
}

func runInstanceRemove(_ *cobra.Command, args []string) error {
	instance, err := savedInstanceNamed(args[0])
	if err != nil {
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeNotInstalled, err.Error()))
		}
		return err
	}
	// --json implies the same non-interactive path as --plain — a caller
	// never has to remember to pass both. --plain remains independently
	// useful (plain text, no JSON) and may still be combined with --json.
	if removePlainFlag || jsonFlag {
		// Validate the explicit flags before requiring a container engine —
		// a missing required flag is a pure, local, deterministic failure
		// and must not depend on (or be masked by) engine availability.
		opts, optErr := resolveRemoveOptions()
		if optErr != nil {
			if jsonFlag {
				return writeJSONError(optErr)
			}
			return optErr
		}
		eng, err := engineFromConfig(instance.Config.Engine)
		if err != nil {
			wrapped := fmt.Errorf("the container runtime must be running before this instance can be removed: %w", err)
			if jsonFlag {
				return writeJSONError(newJSONError(ErrCodeEngineNotFound, wrapped.Error()))
			}
			return wrapped
		}
		if jsonFlag {
			return runRemoveJSON(eng, instance, opts)
		}
		return runRemovePlain(eng, instance, opts)
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

// resolveRemoveOptions turns --plain/--json's explicit flags into
// workflow.RemoveInstanceOptions, replacing every prompt the interactive
// flow above uses with a hard requirement — no silent default for a
// destructive choice. The type-the-instance-name confirmation the
// interactive flow uses for extra safety has no headless equivalent:
// --yes together with --delete-volumes is treated as the caller's explicit
// confirmation instead of requiring a --confirm-name flag that would just
// re-add a prompt-shaped requirement.
func resolveRemoveOptions() (workflow.RemoveInstanceOptions, error) {
	if !removeYesFlag {
		return workflow.RemoveInstanceOptions{}, newJSONError(ErrCodeMissingRequiredFlag, "--yes is required for non-interactive removal")
	}
	if removeKeepVolumesFlag == removeDeleteVolumesFlag {
		return workflow.RemoveInstanceOptions{}, newJSONError(ErrCodeMissingRequiredFlag, "specify exactly one of --keep-volumes or --delete-volumes")
	}
	opts := workflow.RemoveInstanceOptions{DeleteData: removeDeleteVolumesFlag}
	if removeDeleteVolumesFlag {
		if removeBackupFlag == removeNoBackupFlag {
			return workflow.RemoveInstanceOptions{}, newJSONError(ErrCodeMissingRequiredFlag, "specify exactly one of --backup or --no-backup when using --delete-volumes")
		}
		opts.BackupData = removeBackupFlag
	}
	return opts, nil
}

// removeStageLabel gives each workflow.RemoveInstance stage the same
// plain-language phrasing runSetupPlain/runUpdatePlain use for their steps.
func removeStageLabel(stage string) string {
	switch stage {
	case "prepare":
		return "Check that the instance can be removed"
	case "stop_container":
		return "Stop the container"
	case "backup":
		return "Back up saved data"
	case "remove_container":
		return "Remove the container"
	case "delete_volumes":
		return "Delete saved data volumes"
	default:
		return stage
	}
}

// runRemovePlain performs non-interactive removal suitable for CI/CD and
// scripts, mirroring runSetupPlain/runUpdatePlain's step-printing style.
func runRemovePlain(eng engine.Engine, instance config.InstanceInfo, opts workflow.RemoveInstanceOptions) error {
	fmt.Printf("Removing instance %s...\n", instance.Name)

	lastStage := "prepare"
	fmt.Printf("  → %s... ", removeStageLabel(lastStage))
	opts.OnStage = func(stage string) {
		fmt.Println("ok")
		fmt.Printf("  → %s... ", removeStageLabel(stage))
		lastStage = stage
	}

	result, err := workflow.RemoveInstance(eng, instance, opts)
	if err != nil {
		fmt.Printf("FAILED\n    %v\n", err)
		if result.BackupPath != "" {
			fmt.Printf("Backup created before the problem occurred: %s\n", result.BackupPath)
		}
		return err
	}
	fmt.Println("ok")

	fmt.Println(styles.Success.Render("✓ Instance removed"))
	if opts.DeleteData {
		fmt.Println("  Its saved data was permanently deleted.")
		if result.BackupPath != "" {
			fmt.Printf("  Backup: %s\n", result.BackupPath)
		}
	} else {
		fmt.Printf("  Saved data was kept in %s and %s.\n", instance.Config.HomeVolumeName(), instance.Config.StateVolumeName())
	}
	return nil
}

// runRemoveJSON performs the same non-interactive removal as runRemovePlain
// but emits NDJSON progress instead of printing text, using the same
// {"stage","state"} convention as add/update. Unlike add, no cleanup-on-
// cancel logic is needed: a partially-removed instance (e.g. container
// removed but volumes not yet deleted) is exactly the state doctor/a
// retried remove already handles. See JSON_MODE_SPEC.md §8.
func runRemoveJSON(eng engine.Engine, instance config.InstanceInfo, opts workflow.RemoveInstanceOptions) error {
	nd := newNDJSONEncoder()

	lastStage := "prepare"
	nd.start(lastStage)
	opts.OnStage = func(stage string) {
		nd.done(lastStage)
		nd.start(stage)
		lastStage = stage
	}

	result, err := workflow.RemoveInstance(eng, instance, opts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nd.fail(lastStage, newJSONError(ErrCodeCancelled, err.Error()))
		}
		return nd.fail(lastStage, err)
	}
	nd.done(lastStage)

	removedVolumes := result.RemovedVolumes
	if removedVolumes == nil {
		removedVolumes = []string{}
	}
	nd.complete(removeResultPayload{
		ContainerStopped: result.ContainerStopped,
		ContainerRemoved: result.ContainerRemoved,
		RemovedVolumes:   removedVolumes,
		BackupPath:       result.BackupPath,
	})
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
