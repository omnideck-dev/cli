package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	if !promptYN("Are you sure? This will stop and remove the container. [y/N]: ") {
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

	// Optionally delete data directories (with optional backup).
	if promptYN(fmt.Sprintf("Delete data directories (%s, %s)? [y/N]: ", cfg.SharedDir, cfg.StateDir)) {
		if promptYN("Back up data directories before deleting? [y/N]: ") {
			fmt.Printf("Creating backup archive... ")
			archivePath, err := backupDirs(cfg.SharedDir, cfg.StateDir, cfg.ContainerName)
			if err != nil {
				fmt.Println(styles.CrossMark)
				fmt.Fprintf(os.Stderr, "Warning: backup failed: %v\nData directories were NOT deleted.\n", err)
				goto removeConfig
			}
			fmt.Println(styles.CheckMark)
			fmt.Printf("  Saved to: %s\n", styles.Active.Render(archivePath))
		}

		for _, dir := range []string{cfg.SharedDir, cfg.StateDir} {
			if err := safeRemoveAll(dir); err != nil {
				fmt.Printf("Skipping %s: %v\n", dir, err)
				continue
			}
			fmt.Printf("Removing %s... ", dir)
			if err := os.RemoveAll(dir); err != nil {
				if os.IsPermission(err) {
					fmt.Println(styles.CrossMark)
					fmt.Fprintf(os.Stderr, "  Permission denied — some files are owned by the container user.\n")
					fmt.Fprintf(os.Stderr, "  Remove manually: sudo rm -rf %s\n", dir)
				} else if os.IsNotExist(err) {
					// Already deleted (e.g. StateDir is inside SharedDir which was just removed).
					fmt.Println(styles.Warning.Render("(already removed)"))
				} else {
					fmt.Println(styles.CrossMark)
					fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
				}
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
		return nil, "", fmt.Errorf("Omnideck is not installed.\nRun: omnideck install")
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

func promptYN(prompt string) bool {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		ans := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes")
	}
	return false
}

// backupDirs creates a timestamped .tar.gz in the user's home directory
// containing sharedDir (archived as "shared/") and stateDir (as "state/").
// Returns the path to the created archive.
func backupDirs(sharedDir, stateDir, containerName string) (string, error) {
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

	for _, entry := range []struct{ src, prefix string }{
		{sharedDir, "shared"},
		{stateDir, "state"},
	} {
		if _, err := os.Stat(entry.src); os.IsNotExist(err) {
			continue // dir doesn't exist, skip silently
		}
		if err := addDirToTar(tw, entry.src, entry.prefix); err != nil {
			return "", fmt.Errorf("archiving %s: %w", entry.src, err)
		}
	}
	return archivePath, nil
}

// addDirToTar walks srcDir and writes every file and directory into tw
// under the given prefix (e.g. "shared/").
func addDirToTar(tw *tar.Writer, srcDir, prefix string) error {
	return filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		// Build the in-archive name.
		archiveName := filepath.Join(prefix, rel)

		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			// Preserve symlink target.
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header := &tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     archiveName,
				Linkname: link,
				ModTime:  fi.ModTime(),
				Mode:     int64(fi.Mode()),
			}
			return tw.WriteHeader(header)

		case fi.IsDir():
			header := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     archiveName + "/",
				ModTime:  fi.ModTime(),
				Mode:     int64(fi.Mode()),
			}
			return tw.WriteHeader(header)

		case fi.Mode().IsRegular():
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

		default:
			// Skip devices, sockets, etc.
			return nil
		}
	})
}

// safeRemoveAll returns an error if path looks dangerous (too short, a root
// directory, or a known system path) to prevent accidental data loss from a
// corrupted config file.
func safeRemoveAll(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	clean := filepath.Clean(path)
	// Reject paths that are too short (fewer than 3 components deep).
	// e.g. "/" "/home" "/etc" are all dangerous.
	parts := strings.Split(strings.TrimPrefix(clean, "/"), string(filepath.Separator))
	if len(parts) < 3 {
		return fmt.Errorf("refusing to remove %q: path is too short (possible misconfiguration)", clean)
	}
	// Block well-known system roots.
	dangerousPrefixes := []string{
		"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64",
		"/proc", "/root", "/run", "/sbin", "/sys", "/tmp",
		"/usr", "/var",
	}
	for _, prefix := range dangerousPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return fmt.Errorf("refusing to remove %q: looks like a system directory", clean)
		}
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
