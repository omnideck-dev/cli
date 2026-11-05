package workflow

import (
	"errors"
	"slices"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestRepairInstanceRecreatesMissingContainerAndSavesTheLayout(t *testing.T) {
	saved := desiredTestConfig(t)
	saved.LayoutVersion = 1
	eng := &fakeEnsureEngine{
		volumes:     map[string]bool{saved.HomeVolumeName(): true, saved.StateVolumeName(): true},
		localImages: map[string]bool{saved.Image: true},
	}
	var stages []string
	var savedWith *config.Config
	result, repaired, err := RepairInstance(eng, saved, func(cfg *config.Config) error {
		savedWith = cfg
		return nil
	}, EnsureInstanceOptions{OnStage: func(stage string) { stages = append(stages, stage) }})
	if err != nil || result.Action != "repaired" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	// Repair announces check_environment before EnsureInstance repeats it;
	// stream consumers treat the repeat as the same running stage.
	if want := []string{"check_environment", "prepare_storage", "create_container", "save_config"}; !slices.Equal(slices.Compact(stages), want) {
		t.Fatalf("stages = %v, want %v (stored image, no download)", stages, want)
	}
	if repaired.LayoutVersion != config.CurrentContainerLayout || savedWith == nil || savedWith.LayoutVersion != config.CurrentContainerLayout {
		t.Fatalf("repaired layout = %d, saved = %+v", repaired.LayoutVersion, savedWith)
	}
}

// A healthy container is not replaced just to modernize its layout.
func TestRepairInstanceStartsAnExistingContainerWithoutChangingIt(t *testing.T) {
	saved := desiredTestConfig(t)
	saved.LayoutVersion = 1
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "exited", env: []string{"OMNIDECK_EXTERNAL_URL=" + saved.BrowserURL()}},
	}
	result, repaired, err := RepairInstance(eng, saved, func(*config.Config) error {
		t.Fatal("an unchanged repair must not rewrite settings")
		return nil
	}, EnsureInstanceOptions{})
	if err != nil || result.Action != "started" || eng.removed != 0 || repaired.LayoutVersion != 1 {
		t.Fatalf("result=%#v err=%v removed=%d layout=%d", result, err, eng.removed, repaired.LayoutVersion)
	}
}

// A failed create cleans up the partly created container, so a retry
// recreates it rather than starting a broken one.
func TestRepairInstanceCleansUpAFailedCreate(t *testing.T) {
	saved := desiredTestConfig(t)
	saved.LayoutVersion = 1
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{runErrors: []error{errors.New("podman run: interrupted")}},
		volumes:             map[string]bool{saved.HomeVolumeName(): true, saved.StateVolumeName(): true},
		localImages:         map[string]bool{saved.Image: true},
	}
	_, _, err := RepairInstance(eng, saved, func(*config.Config) error {
		t.Fatal("a failed repair must not save")
		return nil
	}, EnsureInstanceOptions{})
	if err == nil || eng.removed == 0 {
		t.Fatalf("err=%v removed=%d", err, eng.removed)
	}
}

func TestRepairInstanceMigratesALegacyImageForAMissingContainer(t *testing.T) {
	saved := desiredTestConfig(t)
	saved.Image = "ghcr.io/omnideck-dev/omnideck:main"
	eng := &fakeEnsureEngine{
		volumes:     map[string]bool{saved.HomeVolumeName(): true, saved.StateVolumeName(): true},
		localImages: map[string]bool{config.DefaultImage: true},
	}
	var savedWith *config.Config
	_, repaired, err := RepairInstance(eng, saved, func(cfg *config.Config) error {
		savedWith = cfg
		return nil
	}, EnsureInstanceOptions{})
	if err != nil || repaired.Image != config.DefaultImage || savedWith == nil || savedWith.Image != config.DefaultImage {
		t.Fatalf("repaired = %q saved = %+v err = %v", repaired.Image, savedWith, err)
	}
	if len(eng.runOptions) != 1 || eng.runOptions[0].Image != config.DefaultImage || len(eng.pulls) != 0 {
		t.Fatalf("runs = %+v pulls = %v", eng.runOptions, eng.pulls)
	}
}

// A repaired container whose settings cannot be saved is removed again, so
// the runtime and saved settings stay aligned and a retry starts over.
func TestRepairInstanceRemovesTheContainerWhenSavingFails(t *testing.T) {
	saved := desiredTestConfig(t)
	saved.LayoutVersion = 1
	eng := &fakeEnsureEngine{
		volumes:     map[string]bool{saved.HomeVolumeName(): true, saved.StateVolumeName(): true},
		localImages: map[string]bool{saved.Image: true},
	}
	_, repaired, err := RepairInstance(eng, saved, func(*config.Config) error {
		return errors.New("read-only file system")
	}, EnsureInstanceOptions{})
	if err == nil || eng.exists || eng.removed != 1 {
		t.Fatalf("err = %v exists = %v removed = %d", err, eng.exists, eng.removed)
	}
	if repaired.LayoutVersion != 1 {
		t.Fatalf("returned layout = %d, want the unchanged saved settings", repaired.LayoutVersion)
	}
}
