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

	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var unsetupCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove an Omnideck installation",
	RunE:  runUninstall,
}

func init() {
	rootCmd.AddCommand(unsetupCmd)
}

func runUninstall(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg, cfgPath := LoadedConfig, ConfigPath

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
	stopped, err := workflow.EnsureStopped(eng, cfg.ContainerName)
	if err != nil {
		fmt.Println(styles.CrossMark)
		return err
	}
	if stopped {
		fmt.Println(styles.CheckMark)
	} else {
		fmt.Println(styles.Warning.Render("(already stopped or removed)"))
	}

	// Remove container.
	fmt.Printf("Removing container %s... ", cfg.ContainerName)
	removed, err := workflow.EnsureRemoved(eng, cfg.ContainerName)
	if err != nil {
		fmt.Println(styles.CrossMark)
		return fmt.Errorf("the container could not be removed, so its saved settings were kept: %w", err)
	}
	if removed {
		fmt.Println(styles.CheckMark)
	} else {
		fmt.Println(styles.Warning.Render("(already removed)"))
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
				return fmt.Errorf("backup failed; data volumes and saved settings were kept: %w", err)
			}
			fmt.Println(styles.CheckMark)
			fmt.Printf("  Saved to: %s\n", styles.Active.Render(archivePath))
		}

		for _, volume := range []string{homeVolume, stateVolume} {
			if err := validateVolumeName(volume); err != nil {
				return fmt.Errorf("data volumes and saved settings were kept: %w", err)
			}
			fmt.Printf("Removing volume %s... ", volume)
			if err := eng.RemoveVolume(volume); err != nil {
				fmt.Println(styles.CrossMark)
				return fmt.Errorf("volume %s could not be removed, so saved settings were kept: %w", volume, err)
			} else {
				fmt.Println(styles.CheckMark)
			}
		}
	}

	// Remove config file.
	fmt.Printf("Removing config %s... ", cfgPath)
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		fmt.Println(styles.CrossMark)
		return fmt.Errorf("removing saved settings: %w", err)
	} else {
		fmt.Println(styles.CheckMark)
	}

	fmt.Println(styles.Success.Render("\nOmnideck uninstalled."))
	return nil
}

func instanceNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
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
}, homeVolume, stateVolume, containerName string) (archivePath string, retErr error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	timestamp := time.Now().Format("20060102-150405")
	archivePath = filepath.Join(home, fmt.Sprintf("omnideck-backup-%s-%s.tar.gz", containerName, timestamp))

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("creating archive file: %w", err)
	}
	defer func() {
		_ = f.Close()
		if retErr != nil {
			_ = os.Remove(archivePath)
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

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
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("finishing backup archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("compressing backup archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing backup archive: %w", err)
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
