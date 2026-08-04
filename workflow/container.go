// Package workflow contains application-level operations shared by the CLI
// commands and interactive screens. Container engines deliberately expose raw
// Docker/Podman behavior; this package turns that behavior into idempotent,
// user-facing Omnideck operations.
package workflow

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// ContainerStateEngine is the read-only engine surface used by lifecycle
// workflows.
type ContainerStateEngine interface {
	ContainerExists(name string) (bool, error)
	ContainerStatus(name string) (string, error)
}

// ContainerStartEngine can inspect and start a container.
type ContainerStartEngine interface {
	ContainerStateEngine
	StartContainer(name string) error
}

// ContainerStopEngine can inspect and stop a container.
type ContainerStopEngine interface {
	ContainerStateEngine
	StopContainer(name string) error
}

// ContainerRemoveEngine can find and remove a container.
type ContainerRemoveEngine interface {
	ContainerExists(name string) (bool, error)
	RemoveContainer(name string) error
}

// ContainerEngine is the full engine surface needed when replacing a
// container. Smaller workflows accept the narrower interfaces above.
type ContainerEngine interface {
	ContainerStartEngine
	ContainerStopEngine
	ContainerRemoveEngine
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
func EnsureStarted(eng ContainerStartEngine, name string) (changed bool, err error) {
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

// IsActiveContainerStatus reports whether status is one a running Omnideck
// instance can report while still doing meaningful work: "running" plainly,
// but also "paused" (frozen, not exited — Docker/Podman still return real,
// non-error CPU/memory stats for it) and "restarting" (mid-restart-policy
// cycle). Every caller that needs to know "is this container live" — stats
// fetching, EnsureStopped — shares this one definition instead of each
// re-deriving its own list and drifting out of sync.
func IsActiveContainerStatus(status string) bool {
	switch status {
	case "running", "paused", "restarting":
		return true
	default:
		return false
	}
}

// EnsureStopped stops name only when it exists and is in an active state.
// Missing and already-stopped containers are successful no-ops.
func EnsureStopped(eng ContainerStopEngine, name string) (changed bool, err error) {
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
	if !IsActiveContainerStatus(status) {
		return false, nil
	}
	if err := eng.StopContainer(name); err != nil {
		return false, fmt.Errorf("stopping container %q: %w", name, err)
	}
	return true, nil
}

// EnsureRemoved removes name when it exists. Missing containers are successful
// no-ops, which makes update retries and interrupted repairs safe.
func EnsureRemoved(eng ContainerRemoveEngine, name string) (changed bool, err error) {
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
//
// If the failure was the shared process context being cancelled
// (SIGINT/SIGTERM), the returned error satisfies
// errors.Is(err, context.Canceled) — see engine.WrapIfCancelled's doc for why
// a killed subprocess's own error can't be checked for that directly.
func Recreate(eng ContainerEngine, current, next *config.Config) (err error) {
	defer func() { err = engine.WrapIfCancelled(err) }()

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
	runErr := eng.RunContainer(RunOptions(next))
	if runErr != nil && isNameConflictError(runErr) {
		// Podman can leave a name reserved in its storage layer for a
		// container its own database no longer tracks (e.g. after an
		// interrupted removal), so ContainerExists/EnsureRemoved above never
		// saw anything to clean up: `podman ps -a` reads the database, not
		// storage. The engine's own error tells us exactly what to do —
		// remove that name directly — so force it once and retry.
		_ = eng.RemoveContainer(next.ContainerName)
		runErr = eng.RunContainer(RunOptions(next))
	}
	if err := runErr; err == nil {
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

// isNameConflictError reports whether err is a container-engine error saying
// a container/storage record with the requested name already exists. Both
// Docker ("Conflict. The container name ... is already in use by container
// ...") and Podman ("the container name ... is already in use by ... an
// external entity") phrase this the same way, so a single substring check
// covers both engines.
func isNameConflictError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already in use by")
}
