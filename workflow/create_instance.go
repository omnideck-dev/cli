package workflow

import (
	"context"
	"fmt"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// InstanceCreationEngine is the narrow container-runtime surface needed to
// create a new Omnideck instance.
type InstanceCreationEngine interface {
	ContainerExists(name string) (bool, error)
	CreateVolume(name string) error
	RemoveVolume(name string) error
	PullImage(image string, msgs chan<- string) error
	ImageExists(image string) (bool, error)
	RunContainer(opts engine.RunOptions) error
	RemoveContainer(name string) error
}

// CreateInstanceOptions controls progress reporting for CreateInstance.
// Every field is optional; a caller that doesn't need progress reporting can
// leave them all nil/zero.
type CreateInstanceOptions struct {
	// OnStage is called as each stage begins: "check_availability",
	// "create_home_volume", "create_state_volume", "pull_image",
	// "run_container", "save_config".
	OnStage func(stage string)
	// OnPullProgress is called with each raw line docker/podman pull emits.
	OnPullProgress func(line string)
}

func (o CreateInstanceOptions) reportStage(stage string) {
	if o.OnStage != nil {
		o.OnStage(stage)
	}
}

// CreateInstance runs the check-availability/create-volumes/pull/run/save
// sequence shared by `add --plain` and `add --json`, so the two
// non-interactive paths can never disagree about what creating an instance
// does or what happens when it fails partway through. save is called only
// once every prior stage has succeeded, and should persist cfg to disk.
//
// If any stage fails, every volume/container this call itself created is
// removed before returning — nothing else will ever discover an orphan
// otherwise, since list/doctor/remove are all keyed off the saved config
// file a failed run never gets to write. If the failure was the shared
// process context being cancelled (SIGINT/SIGTERM), the returned error
// satisfies errors.Is(err, context.Canceled) — checked and captured before
// the context is reset for the cleanup calls themselves to be able to run.
func CreateInstance(eng InstanceCreationEngine, cfg *config.Config, save func() error, opts CreateInstanceOptions) (err error) {
	var homeVolumeCreated, stateVolumeCreated, containerCreated bool
	defer func() {
		if err == nil {
			return
		}
		err = engine.WrapIfCancelled(err)
		engine.SetCancelContext(context.Background())
		if containerCreated {
			_ = eng.RemoveContainer(cfg.ContainerName)
		}
		if homeVolumeCreated {
			_ = eng.RemoveVolume(cfg.HomeVolumeName())
		}
		if stateVolumeCreated {
			_ = eng.RemoveVolume(cfg.StateVolumeName())
		}
	}()

	opts.reportStage("check_availability")
	exists, existsErr := eng.ContainerExists(cfg.ContainerName)
	if existsErr != nil {
		return fmt.Errorf("checking the name %q: %w", cfg.ContainerName, existsErr)
	}
	if exists {
		return fmt.Errorf("another container already uses the name %q; choose a different --name", cfg.ContainerName)
	}
	if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
		return fmt.Errorf("another app is already using browser address number %s; choose a different --port", cfg.WebUIPortOrDefault())
	}

	opts.reportStage("create_home_volume")
	if err := eng.CreateVolume(cfg.HomeVolumeName()); err != nil {
		return err
	}
	homeVolumeCreated = true

	opts.reportStage("create_state_volume")
	if err := eng.CreateVolume(cfg.StateVolumeName()); err != nil {
		return err
	}
	stateVolumeCreated = true

	opts.reportStage("pull_image")
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
	pullErr := eng.PullImage(cfg.Image, msgs)
	close(msgs)
	<-forwarded
	if pullErr != nil {
		// A registry that cannot be reached is only fatal when the image is not
		// already here. Setting up again offline, or repairing an installation
		// whose container is gone, needs nothing downloaded — and an image
		// named by digest cannot have changed since it was fetched.
		present, existsErr := eng.ImageExists(cfg.Image)
		if existsErr != nil || !present {
			return pullErr
		}
		if opts.OnPullProgress != nil {
			opts.OnPullProgress("Using the copy already on this computer")
		}
	}

	opts.reportStage("run_container")
	if err := eng.RunContainer(RunOptions(cfg)); err != nil {
		return err
	}
	containerCreated = true

	opts.reportStage("save_config")
	if err := save(); err != nil {
		return err
	}
	return nil
}
