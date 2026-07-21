package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/tui"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the Omnideck container",
	RunE:  runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(_ *cobra.Command, _ []string) error {
	cfg, cfgPath, err := resolveUninstallTarget()
	if err != nil {
		if err.Error() == "aborted" {
			fmt.Println("Aborted.")
			return nil
		}
		return err
	}

	fmt.Printf("\nUninstalling instance %s (container: %s)\n\n",
		styles.Active.Render(instanceNameFromPath(cfgPath)),
		styles.Active.Render(cfg.ContainerName))
	prompts := bufio.NewScanner(os.Stdin)

	if !promptYN(prompts, "Are you sure? This will stop and remove the container. [y/N]: ") {
		fmt.Println("Aborted.")
		return nil
	}

	eng, err := engineFromConfig(cfg.Engine)
	if err != nil {
		return err
	}

	// Stop container.
	fmt.Printf("Stopping %s... ", cfg.ContainerName)
	if err := eng.StopContainer(cfg.ContainerName); err != nil {
		if isAlreadyStopped(err) {
			fmt.Println(styles.Warning.Render("(already stopped)"))
		} else {
			fmt.Println(styles.CrossMark)
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	} else {
		fmt.Println(styles.CheckMark)
	}

	// Remove container.
	fmt.Printf("Removing container %s... ", cfg.ContainerName)
	if err := eng.RemoveContainer(cfg.ContainerName); err != nil {
		if isNotFound(err) {
			fmt.Println(styles.Warning.Render("(already removed)"))
		} else {
			fmt.Println(styles.CrossMark)
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	} else {
		fmt.Println(styles.CheckMark)
	}

	homeVolume := cfg.HomeVolumeName()
	stateVolume := cfg.StateVolumeName()

	// Optionally delete data volumes (with optional backup).
	if promptYN(prompts, fmt.Sprintf("Delete data volumes (%s, %s)? [y/N]: ", homeVolume, stateVolume)) {
		if promptYN(prompts, "Back up data volumes before deleting? [y/N]: ") {
			fmt.Printf("Creating backup archive... ")
			archivePath, err := backupVolumes(eng, homeVolume, stateVolume, cfg.ContainerName)
			if err != nil {
				fmt.Println(styles.CrossMark)
				fmt.Fprintf(os.Stderr, "Warning: backup failed: %v\nData volumes were NOT deleted.\n", err)
				goto removeConfig
			}
			fmt.Println(styles.CheckMark)
			fmt.Printf("  Saved to: %s\n", styles.Active.Render(archivePath))
		}

		for _, volume := range []string{homeVolume, stateVolume} {
			if err := validateVolumeName(volume); err != nil {
				fmt.Printf("Skipping %s: %v\n", volume, err)
				continue
			}
			fmt.Printf("Removing volume %s... ", volume)
			if err := eng.RemoveVolume(volume); err != nil {
				fmt.Println(styles.CrossMark)
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			} else {
				fmt.Println(styles.CheckMark)
			}
		}
	}

removeConfig:

	// Remove config file.
	fmt.Printf("Removing config %s... ", cfgPath)
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		fmt.Println(styles.CrossMark)
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	} else {
		fmt.Println(styles.CheckMark)
	}

	fmt.Println(styles.Success.Render("\nOmnideck uninstalled."))
	return nil
}

// resolveUninstallTarget decides which instance to uninstall:
// - If --config or --name was given (LoadedConfig is set), use it.
// - If exactly one instance exists, use it.
// - If multiple exist, run the picker.
// - If none, error.
func resolveUninstallTarget() (*config.Config, string, error) {
	if LoadedConfig != nil {
		return LoadedConfig, ConfigPath, nil
	}

	instances, err := config.ListInstances()
	if err != nil {
		return nil, "", fmt.Errorf("listing instances: %w", err)
	}

	switch len(instances) {
	case 0:
		return nil, "", fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")
	case 1:
		return instances[0].Config, instances[0].Path, nil
	default:
		chosen, ok := tui.RunPicker(
			"Uninstall — Select an instance",
			"Which instance do you want to remove?",
			instances,
		)
		if !ok {
			return nil, "", fmt.Errorf("aborted")
		}
		return chosen.Config, chosen.Path, nil
	}
}

func instanceNameFromPath(path string) string {
	base := strings.TrimSuffix(path, ".yaml")
	base = strings.TrimPrefix(base, config.InstancesDir()+"/")
	return base
}

func promptYN(scanner *bufio.Scanner, prompt string) bool {
	fmt.Print(prompt)
	if scanner.Scan() {
		ans := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes")
	}
	return false
}

// backupVolumes creates a timestamped .tar.gz in the user's home directory
// containing native volume exports as home.tar and state.tar.
// Returns the path to the created archive.
func backupVolumes(eng interface {
	ExportVolume(string, io.Writer) error
}, homeVolume, stateVolume, containerName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	timestamp := time.Now().Format("20060102-150405")
	archivePath := filepath.Join(home, fmt.Sprintf("omnideck-backup-%s-%s.tar.gz", containerName, timestamp))

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("creating archive file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	tmpDir, err := os.MkdirTemp("", "omnideck-volume-backup-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, entry := range []struct {
		volume string
		name   string
	}{
		{homeVolume, "home.tar"},
		{stateVolume, "state.tar"},
	} {
		if err := validateVolumeName(entry.volume); err != nil {
			return "", err
		}
		tmpPath := filepath.Join(tmpDir, entry.name)
		f, err := os.Create(tmpPath)
		if err != nil {
			return "", fmt.Errorf("creating temporary export: %w", err)
		}
		if err := eng.ExportVolume(entry.volume, f); err != nil {
			f.Close()
			return "", fmt.Errorf("exporting volume %s: %w", entry.volume, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("closing temporary export: %w", err)
		}
		if err := addFileToTar(tw, tmpPath, entry.name); err != nil {
			return "", fmt.Errorf("archiving volume %s: %w", entry.volume, err)
		}
	}
	return archivePath, nil
}

func addFileToTar(tw *tar.Writer, path, archiveName string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	header.Name = archiveName
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(tw, src)
	return err
}

var volumeNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func validateVolumeName(name string) error {
	if !volumeNameRegex.MatchString(name) {
		return fmt.Errorf("invalid volume name %q", name)
	}
	return nil
}

func isAlreadyStopped(err error) bool {
	s := err.Error()
	return strings.Contains(s, "not running") ||
		strings.Contains(s, "is not running") ||
		strings.Contains(s, "No such container")
}

func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "No such container")
}
