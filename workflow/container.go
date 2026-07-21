// Package workflow contains application-level operations shared by the CLI
// commands and interactive screens. Container engines deliberately expose raw
// Docker/Podman behavior; this package turns that behavior into idempotent,
// user-facing Omnideck operations.
package workflow

import (
	"fmt"
	"runtime"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// ContainerEngine is the part of engine.Engine needed for lifecycle changes.
// Keeping the interface narrow makes workflow behavior straightforward to test.
type ContainerEngine interface {
	ContainerExists(name string) (bool, error)
	ContainerStatus(name string) (string, error)
	StartContainer(name string) error
	StopContainer(name string) error
	RemoveContainer(name string) error
	RunContainer(opts engine.RunOptions) error
}

// RecreateAndSave applies next and persists it as one application transaction.
// A save failure restores the previous container so the runtime and YAML file
// continue to describe the same installation.
func RecreateAndSave(eng ContainerEngine, current, next *config.Config, path string) error {
	if err := Recreate(eng, current, next); err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := config.Save(path, next); err != nil {
		if rollbackErr := Recreate(eng, next, current); rollbackErr != nil {
			return fmt.Errorf("saving settings: %v; restoring the previous container also failed: %w", err, rollbackErr)
		}
		return fmt.Errorf("saving settings: %w (the previous container was restored)", err)
	}
	return nil
}

// RunOptions is the canonical mapping from persisted Omnideck configuration to
// a container-engine request. OllamaHost is intentionally left empty so each
// engine can choose the correct container-facing hostname for the host OS.
func RunOptions(cfg *config.Config) engine.RunOptions {
	return engine.RunOptions{
		Name:        cfg.ContainerName,
		Image:       cfg.Image,
		Memory:      cfg.Memory,
		ShmSize:     cfg.ShmSize,
		HomeVolume:  cfg.HomeVolumeName(),
		StateVolume: cfg.StateVolumeName(),
		Restart:     "always",
		WebUIPort:   cfg.WebUIPortOrDefault(),
		Platform:    runtime.GOOS,
	}
}

// EnsureStarted starts name only when it exists and is not already running.
// changed reports whether an engine mutation was needed.
func EnsureStarted(eng ContainerEngine, name string) (changed bool, err error) {
	exists, err := eng.ContainerExists(name)
	if err != nil {
		return false, fmt.Errorf("checking container %q: %w", name, err)
	}
	if !exists {
		return false, fmt.Errorf("container %q was not found; run `omnideck doctor` for guided repair", name)
	}
	status, err := eng.ContainerStatus(name)
	if err != nil {
		return false, fmt.Errorf("checking container %q status: %w", name, err)
	}
	if status == "running" {
		return false, nil
	}
	if err := eng.StartContainer(name); err != nil {
		return false, fmt.Errorf("starting container %q: %w", name, err)
	}
	return true, nil
}

// EnsureStopped stops name only when it exists and is in an active state.
// Missing and already-stopped containers are successful no-ops.
func EnsureStopped(eng ContainerEngine, name string) (changed bool, err error) {
	exists, err := eng.ContainerExists(name)
	if err != nil {
		return false, fmt.Errorf("checking container %q: %w", name, err)
	}
	if !exists {
		return false, nil
	}
	status, err := eng.ContainerStatus(name)
	if err != nil {
		return false, fmt.Errorf("checking container %q status: %w", name, err)
	}
	switch status {
	case "running", "paused", "restarting":
		if err := eng.StopContainer(name); err != nil {
			return false, fmt.Errorf("stopping container %q: %w", name, err)
		}
		return true, nil
	default:
		return false, nil
	}
}

// EnsureRemoved removes name when it exists. Missing containers are successful
// no-ops, which makes update retries and interrupted repairs safe.
func EnsureRemoved(eng ContainerEngine, name string) (changed bool, err error) {
	exists, err := eng.ContainerExists(name)
	if err != nil {
		return false, fmt.Errorf("checking container %q: %w", name, err)
	}
	if !exists {
		return false, nil
	}
	if err := eng.RemoveContainer(name); err != nil {
		return false, fmt.Errorf("removing container %q: %w", name, err)
	}
	return true, nil
}

// Recreate replaces the current container with next. If the replacement fails
// after an existing container was removed, Omnideck attempts to restore the
// previous configuration before returning the error.
func Recreate(eng ContainerEngine, current, next *config.Config) error {
	hadCurrent, err := eng.ContainerExists(current.ContainerName)
	if err != nil {
		return fmt.Errorf("checking the current container: %w", err)
	}
	if _, err := EnsureStopped(eng, current.ContainerName); err != nil {
		return err
	}
	if _, err := EnsureRemoved(eng, current.ContainerName); err != nil {
		return err
	}
	if err := eng.RunContainer(RunOptions(next)); err == nil {
		return nil
	} else if !hadCurrent {
		return fmt.Errorf("starting the replacement container: %w", err)
	} else {
		applyErr := err
		// A failed run can still leave a partially created container behind.
		_, _ = EnsureRemoved(eng, next.ContainerName)
		if rollbackErr := eng.RunContainer(RunOptions(current)); rollbackErr != nil {
			return fmt.Errorf("starting the replacement container: %v; restoring the previous container also failed: %w", applyErr, rollbackErr)
		}
		return fmt.Errorf("starting the replacement container: %w (the previous settings were restored)", applyErr)
	}
}
