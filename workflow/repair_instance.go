package workflow

import (
	"fmt"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// RepairInstance makes a saved instance run again without a reviewed update.
// It is the one repair transaction shared by `omnideck repair` and the TUI.
//
// A missing container is recreated from the saved settings with the current
// container layout and a legacy image name migrated to the current one,
// reusing that image when it is stored locally and downloading it only when
// it is not. If saving those settings then fails, the new container is
// removed again, so the runtime and the saved settings stay aligned.
//
// An existing container is started, or replaced from the image it runs when
// its MCP sign-in address has drifted; its saved settings are kept, because
// modernizing a healthy container is update's job. Saved data volumes are reused and never created empty: a
// missing one stops the repair with ErrMissingStorage before anything changes.
//
// It returns the configuration the instance now runs with; save is called
// with it only when it differs from current.
//
// SaveInstanceConfig is the save every caller should pass.
func RepairInstance(eng InstanceEnsureEngine, current *config.Config, save func(*config.Config) error, opts EnsureInstanceOptions) (result EnsureInstanceResult, repaired config.Config, err error) {
	defer func() { err = engine.WrapIfCancelled(err) }()
	desired := *current
	desired.Engine = ""
	opts.stage("check_environment")
	exists, err := eng.ContainerExists(current.ContainerName)
	if err != nil {
		return EnsureInstanceResult{}, desired, fmt.Errorf("checking container %q: %w", current.ContainerName, err)
	}
	if !exists {
		desired.LayoutVersion = config.CurrentContainerLayout
		desired.MigrateImage()
	}
	opts.ReuseLocalImage = true

	// Both sides carry the target layout, so a missing container takes
	// EnsureInstance's repair path, which cleans up a partly created
	// container on failure, rather than the replacement path.
	matched := desired
	result, err = EnsureInstance(eng, &matched, &desired, func() error { return nil }, opts)
	if err != nil {
		return result, desired, err
	}
	if desired.LayoutVersion != current.LayoutVersion || desired.Image != current.Image {
		opts.stage("save_config")
		if saveErr := save(&desired); saveErr != nil {
			if removeErr := removeRepairedContainer(eng, desired.ContainerName); removeErr != nil {
				return result, *current, fmt.Errorf("saving the repaired settings failed: %v; removing the new container also failed: %w", saveErr, removeErr)
			}
			return result, *current, fmt.Errorf("saving the repaired settings failed, so the new container was removed and nothing changed: %w", saveErr)
		}
	}
	return result, desired, nil
}

func removeRepairedContainer(eng InstanceEnsureEngine, name string) error {
	restoreCancellation := engine.SuspendCancellation()
	defer restoreCancellation()
	if _, err := EnsureStopped(eng, name); err != nil {
		return err
	}
	_, err := EnsureRemoved(eng, name)
	return err
}

// SaveInstanceConfig records the shared runtime and saves cfg to path: the
// persistence step of a repair or ensure, shared by the CLI and the TUI. An
// empty path saves nothing (a configuration not backed by a file).
func SaveInstanceConfig(runtimeName, path string, cfg *config.Config) error {
	if path == "" {
		return nil
	}
	if err := config.SaveRuntime(runtimeName); err != nil {
		return err
	}
	return config.Save(path, cfg)
}
