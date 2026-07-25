package workflow

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/omnideck-dev/cli/config"
)

// InstanceRemovalEngine is the narrow container-runtime surface needed to
// remove an Omnideck instance and, when explicitly requested, its data.
type InstanceRemovalEngine interface {
	ContainerExists(name string) (bool, error)
	ContainerStatus(name string) (string, error)
	StopContainer(name string) error
	RemoveContainer(name string) error
	VolumeExists(name string) (bool, error)
	RemoveVolume(name string) error
	ExportVolume(name string, w io.Writer) error
}

// RemoveInstanceOptions controls the data choice made by the user. Keeping
// data is represented by the zero value and is therefore the safe default.
type RemoveInstanceOptions struct {
	DeleteData bool
	BackupData bool
	BackupDir  string
	Now        func() time.Time
}

// RemoveInstanceResult reports the parts of removal that changed state.
type RemoveInstanceResult struct {
	ContainerStopped bool
	ContainerRemoved bool
	RemovedVolumes   []string
	BackupPath       string
}

var removalVolumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// RemoveInstance removes one container and its saved instance configuration.
// Data volumes are retained unless DeleteData is explicitly true. The config
// file is removed last so an interrupted operation remains visible and can be
// retried instead of silently disappearing from Omnideck.
func RemoveInstance(eng InstanceRemovalEngine, instance config.InstanceInfo, opts RemoveInstanceOptions) (RemoveInstanceResult, error) {
	var result RemoveInstanceResult
	if eng == nil {
		return result, fmt.Errorf("container runtime is not ready")
	}
	if instance.Config == nil {
		return result, fmt.Errorf("instance %q has no readable saved settings", instance.Name)
	}
	if instance.Path == "" {
		return result, fmt.Errorf("instance %q has no saved settings path", instance.Name)
	}
	if opts.BackupData && !opts.DeleteData {
		return result, fmt.Errorf("a backup is only needed when saved data will be deleted")
	}
	if err := validateRemovalConfigPath(instance.Path); err != nil {
		return result, err
	}

	cfg := instance.Config
	volumes := []string{cfg.HomeVolumeName(), cfg.StateVolumeName()}
	for _, volume := range volumes {
		if !removalVolumeName.MatchString(volume) {
			return result, fmt.Errorf("invalid data volume name %q", volume)
		}
	}

	existingVolumes := make([]string, 0, len(volumes))
	if opts.DeleteData {
		for _, volume := range volumes {
			exists, err := eng.VolumeExists(volume)
			if err != nil {
				return result, fmt.Errorf("checking data volume %q: %w", volume, err)
			}
			if exists {
				existingVolumes = append(existingVolumes, volume)
			}
		}
	}

	stopped, err := EnsureStopped(eng, cfg.ContainerName)
	if err != nil {
		return result, err
	}
	result.ContainerStopped = stopped

	if opts.DeleteData && opts.BackupData && len(existingVolumes) > 0 {
		backupPath, err := backupInstanceVolumes(eng, existingVolumes, cfg.ContainerName, opts)
		if err != nil {
			return result, fmt.Errorf("creating data backup: %w", err)
		}
		result.BackupPath = backupPath
	}

	removed, err := EnsureRemoved(eng, cfg.ContainerName)
	if err != nil {
		return result, fmt.Errorf("the container could not be removed, so its saved settings were kept: %w", err)
	}
	result.ContainerRemoved = removed

	for _, volume := range existingVolumes {
		if err := eng.RemoveVolume(volume); err != nil {
			return result, fmt.Errorf("removing data volume %q: %w; saved instance settings were kept", volume, err)
		}
		result.RemovedVolumes = append(result.RemovedVolumes, volume)
	}

	if err := os.Remove(instance.Path); err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("removing saved instance settings: %w", err)
	}
	return result, nil
}

func validateRemovalConfigPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking saved instance settings: %w", err)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("saved instance settings path is not a file: %s", path)
	}
	return nil
}

func backupInstanceVolumes(eng InstanceRemovalEngine, volumes []string, containerName string, opts RemoveInstanceOptions) (archivePath string, retErr error) {
	backupDir := opts.BackupDir
	if backupDir == "" {
		var err error
		backupDir, err = os.UserHomeDir()
		if err != nil || backupDir == "" {
			backupDir = "."
		}
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	archiveName := fmt.Sprintf("omnideck-backup-%s-%s.tar.gz", containerName, now().Format("20060102-150405.000000000"))
	archivePath = filepath.Join(backupDir, archiveName)

	archive, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating backup file: %w", err)
	}
	complete := false
	defer func() {
		_ = archive.Close()
		if !complete {
			_ = os.Remove(archivePath)
		}
	}()

	gz := gzip.NewWriter(archive)
	tw := tar.NewWriter(gz)
	tempDir, err := os.MkdirTemp("", "omnideck-volume-backup-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary backup space: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	entryNames := []string{"home.tar", "state.tar"}
	for i, volume := range volumes {
		entryName := fmt.Sprintf("volume-%d.tar", i+1)
		if i < len(entryNames) {
			entryName = entryNames[i]
		}
		tempPath := filepath.Join(tempDir, entryName)
		temp, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", fmt.Errorf("preparing backup for %q: %w", volume, err)
		}
		if err := eng.ExportVolume(volume, temp); err != nil {
			_ = temp.Close()
			return "", fmt.Errorf("exporting data volume %q: %w", volume, err)
		}
		if err := temp.Close(); err != nil {
			return "", fmt.Errorf("finishing data volume %q export: %w", volume, err)
		}
		if err := addRemovalBackupFile(tw, tempPath, entryName); err != nil {
			return "", fmt.Errorf("adding data volume %q to backup: %w", volume, err)
		}
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("finishing backup contents: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("compressing backup: %w", err)
	}
	if err := archive.Close(); err != nil {
		return "", fmt.Errorf("finishing backup file: %w", err)
	}
	complete = true
	return archivePath, nil
}

func addRemovalBackupFile(tw *tar.Writer, path, archiveName string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = archiveName
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(tw, source)
	return err
}
