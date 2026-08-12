package workflow

import (
	"fmt"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// InstanceCreationEngine is the narrow container-runtime surface needed to
// create a new Omnideck instance.
type InstanceCreationEngine interface {
	ContainerExists(name string) (bool, error)
	CreateVolume(name string) (bool, error)
	RemoveVolume(name string) error
	PullImage(image string, msgs chan<- string) error
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
	// OnPullProgress is called with each raw line Podman pull emits.
	OnPullProgress func(line string)
	// OnEvent receives typed start/completion events for every lifecycle stage.
	OnEvent func(InstanceCreationEvent)
	// Verify runs an optional, non-destructive post-start check before the
	// configuration is saved. A warning does not roll back a healthy instance.
	Verify func() (detail string, warning bool)
}

type InstanceCreationStage string

const (
	CreateStageAvailability InstanceCreationStage = "check_availability"
	CreateStageHomeVolume   InstanceCreationStage = "create_home_volume"
	CreateStageStateVolume  InstanceCreationStage = "create_state_volume"
	CreateStagePullImage    InstanceCreationStage = "pull_image"
	CreateStageRunContainer InstanceCreationStage = "run_container"
	CreateStageVerify       InstanceCreationStage = "verify_instance"
	CreateStageSaveConfig   InstanceCreationStage = "save_config"
)

type InstanceCreationEvent struct {
	Stage   InstanceCreationStage
	State   string
	Detail  string
	Warning bool
}

func (o CreateInstanceOptions) reportStage(stage string) {
	if o.OnStage != nil {
		o.OnStage(stage)
	}
	if o.OnEvent != nil {
		o.OnEvent(InstanceCreationEvent{Stage: InstanceCreationStage(stage), State: "start"})
	}
}

func (o CreateInstanceOptions) reportDone(stage InstanceCreationStage, detail string, warning bool) {
	if o.OnEvent != nil {
		o.OnEvent(InstanceCreationEvent{Stage: stage, State: "done", Detail: detail, Warning: warning})
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
	if err := ValidateInstanceConfig(cfg); err != nil {
		return err
	}
	var homeVolumeCreated, stateVolumeCreated, containerCreated bool
	defer func() {
		if err == nil {
			return
		}
		err = engine.WrapIfCancelled(err)
		restoreCancellation := engine.SuspendCancellation()
		defer restoreCancellation()
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

	opts.reportStage(string(CreateStageAvailability))
	exists, existsErr := eng.ContainerExists(cfg.ContainerName)
	if existsErr != nil {
		return fmt.Errorf("checking the name %q: %w", cfg.ContainerName, existsErr)
	}
	if exists {
		return classifyError(ErrContainerConflict, fmt.Errorf("another container already uses the name %q; choose a different --name", cfg.ContainerName))
	}
	if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
		return classifyError(ErrPortInUse, fmt.Errorf("another app is already using browser address number %s; choose a different --port", cfg.WebUIPortOrDefault()))
	}
	opts.reportDone(CreateStageAvailability, "available", false)

	opts.reportStage(string(CreateStageHomeVolume))
	homeVolumeCreated, err = eng.CreateVolume(cfg.HomeVolumeName())
	if err != nil {
		return err
	}
	opts.reportDone(CreateStageHomeVolume, creationDetail(homeVolumeCreated), false)

	opts.reportStage(string(CreateStageStateVolume))
	stateVolumeCreated, err = eng.CreateVolume(cfg.StateVolumeName())
	if err != nil {
		return err
	}
	opts.reportDone(CreateStageStateVolume, creationDetail(stateVolumeCreated), false)

	opts.reportStage(string(CreateStagePullImage))
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
		return classifyError(ErrImageDownload, pullErr)
	}
	opts.reportDone(CreateStagePullImage, "", false)

	opts.reportStage(string(CreateStageRunContainer))
	if err := eng.RunContainer(RunOptions(cfg)); err != nil {
		return classifyContainerRunError(err)
	}
	containerCreated = true
	opts.reportDone(CreateStageRunContainer, "", false)

	if opts.Verify != nil {
		opts.reportStage(string(CreateStageVerify))
		detail, warning := opts.Verify()
		opts.reportDone(CreateStageVerify, detail, warning)
	}

	opts.reportStage(string(CreateStageSaveConfig))
	if err := save(); err != nil {
		return err
	}
	opts.reportDone(CreateStageSaveConfig, "", false)
	return nil
}

func creationDetail(created bool) string {
	if created {
		return "created"
	}
	return "reusing existing"
}
