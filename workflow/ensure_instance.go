package workflow

import (
	"fmt"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// InstanceEnsureEngine is the runtime surface needed to make one saved
// Omnideck instance match a desired configuration.
type InstanceEnsureEngine interface {
	ContainerEngine
	CreateVolume(name string) (bool, error)
	RemoveVolume(name string) error
	VolumeExists(name string) (bool, error)
	PullImage(image string, msgs chan<- string) error
}

// EnsureInstanceOptions controls progress reporting for EnsureInstance.
type EnsureInstanceOptions struct {
	OnStage        func(stage string)
	OnPullProgress func(line string)
}

// EnsureInstanceResult describes the mutation performed by EnsureInstance.
type EnsureInstanceResult struct {
	Changed bool
	Action  string
}

func (o EnsureInstanceOptions) stage(stage string) {
	if o.OnStage != nil {
		o.OnStage(stage)
	}
}

// SameInstanceConfig compares every setting that affects the created
// container. InstalledAt and the legacy Engine field are metadata and do not
// require a rebuild.
func SameInstanceConfig(a, b *config.Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ContainerName == b.ContainerName &&
		a.LayoutVersion == b.LayoutVersion &&
		a.HomeVolumeName() == b.HomeVolumeName() &&
		a.StateVolumeName() == b.StateVolumeName() &&
		a.Memory == b.Memory &&
		a.ShmSize == b.ShmSize &&
		a.WebUIPortOrDefault() == b.WebUIPortOrDefault() &&
		a.Image == b.Image
}

// EnsureInstance is the single idempotent transaction used by GUI shells and
// automation. It creates a missing instance, starts an existing matching one,
// repairs a missing container, or safely replaces a container whose desired
// settings changed. Existing data volumes are never removed.
func EnsureInstance(eng InstanceEnsureEngine, current, desired *config.Config, save func() error, opts EnsureInstanceOptions) (result EnsureInstanceResult, err error) {
	if err := ValidateInstanceConfig(desired); err != nil {
		return result, err
	}
	defer func() { err = engine.WrapIfCancelled(err) }()
	if desired == nil {
		return result, fmt.Errorf("desired instance configuration is required")
	}

	opts.stage("check_environment")
	exists, err := eng.ContainerExists(desired.ContainerName)
	if err != nil {
		return result, fmt.Errorf("checking container %q: %w", desired.ContainerName, err)
	}
	matching := SameInstanceConfig(current, desired)
	if current == nil && exists {
		return result, classifyError(ErrContainerConflict, fmt.Errorf("another container already uses the name %q", desired.ContainerName))
	}
	if (!exists || current == nil || current.WebUIPortOrDefault() != desired.WebUIPortOrDefault()) && !checks.PortAvailable(desired.WebUIPortOrDefault()) {
		return result, classifyError(ErrPortInUse, fmt.Errorf("another app is already using port %s", desired.WebUIPortOrDefault()))
	}

	if matching && exists {
		opts.stage("start_container")
		changed, startErr := EnsureStarted(eng, desired.ContainerName)
		if startErr != nil {
			return result, startErr
		}
		if changed {
			return EnsureInstanceResult{Changed: true, Action: "started"}, nil
		}
		return EnsureInstanceResult{Changed: false, Action: "unchanged"}, nil
	}

	createdVolumes := make([]string, 0, 2)
	containerAttempted := false
	defer func() {
		if err == nil || current != nil {
			return
		}
		err = engine.WrapIfCancelled(err)
		restoreCancellation := engine.SuspendCancellation()
		defer restoreCancellation()
		if containerAttempted {
			_ = eng.RemoveContainer(desired.ContainerName)
		}
		for _, volume := range createdVolumes {
			_ = eng.RemoveVolume(volume)
		}
	}()

	opts.stage("prepare_storage")
	for _, volume := range []string{desired.HomeVolumeName(), desired.StateVolumeName()} {
		present, volumeErr := eng.VolumeExists(volume)
		if volumeErr != nil {
			return result, fmt.Errorf("checking storage %q: %w", volume, volumeErr)
		}
		if present {
			continue
		}
		if current != nil {
			return result, classifyError(
				ErrMissingStorage,
				fmt.Errorf("saved data volume %q is missing; automatic repair stopped to avoid creating an empty replacement", volume),
			)
		}
		created, volumeErr := eng.CreateVolume(volume)
		if volumeErr != nil {
			return result, fmt.Errorf("preparing storage %q: %w", volume, volumeErr)
		}
		if created {
			createdVolumes = append(createdVolumes, volume)
		}
	}

	opts.stage("pull_image")
	msgs := make(chan string, 32)
	forwarded := make(chan struct{})
	go func() {
		defer close(forwarded)
		for line := range msgs {
			if opts.OnPullProgress != nil {
				opts.OnPullProgress(line)
			}
		}
	}()
	pullErr := eng.PullImage(desired.Image, msgs)
	close(msgs)
	<-forwarded
	if pullErr != nil {
		return result, classifyError(ErrImageDownload, pullErr)
	}

	if current == nil {
		opts.stage("create_container")
		containerAttempted = true
		if runErr := eng.RunContainer(RunOptions(desired)); runErr != nil {
			return result, classifyContainerRunError(runErr)
		}
		opts.stage("save_config")
		if saveErr := save(); saveErr != nil {
			return result, saveErr
		}
		return EnsureInstanceResult{Changed: true, Action: "created"}, nil
	}

	if matching && !exists {
		opts.stage("create_container")
		if runErr := eng.RunContainer(RunOptions(desired)); runErr != nil {
			runErr = classifyContainerRunError(runErr)
			runErr = engine.WrapIfCancelled(runErr)
			restoreCancellation := engine.SuspendCancellation()
			defer restoreCancellation()
			_ = eng.RemoveContainer(desired.ContainerName)
			return result, runErr
		}
		return EnsureInstanceResult{Changed: true, Action: "repaired"}, nil
	}

	opts.stage("replace_container")
	if replaceErr := Recreate(eng, current, desired); replaceErr != nil {
		return result, replaceErr
	}
	opts.stage("save_config")
	if saveErr := save(); saveErr != nil {
		if rollbackErr := Recreate(eng, desired, current); rollbackErr != nil {
			return result, fmt.Errorf("saving settings: %v; restoring the previous container also failed: %w", saveErr, rollbackErr)
		}
		return result, fmt.Errorf("saving settings: %w (the previous container was restored)", saveErr)
	}
	return EnsureInstanceResult{Changed: true, Action: "recreated"}, nil
}
