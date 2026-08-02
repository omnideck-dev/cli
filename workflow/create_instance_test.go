package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

type fakeCreationEngine struct {
	containerExists bool
	existsErr       error
	createVolumeErr error
	createdVolumes  []string
	removedVolumes  []string
	pullErr         error
	imagePresent    bool
	pullMsgs        []string
	runErr          error
	removedContaner bool
}

func (f *fakeCreationEngine) ImageExists(string) (bool, error) {
	return f.imagePresent, nil
}

func (f *fakeCreationEngine) ContainerExists(string) (bool, error) {
	return f.containerExists, f.existsErr
}
func (f *fakeCreationEngine) CreateVolume(name string) error {
	if f.createVolumeErr != nil {
		return f.createVolumeErr
	}
	f.createdVolumes = append(f.createdVolumes, name)
	return nil
}
func (f *fakeCreationEngine) RemoveVolume(name string) error {
	f.removedVolumes = append(f.removedVolumes, name)
	return nil
}
func (f *fakeCreationEngine) PullImage(_ string, msgs chan<- string) error {
	for _, m := range f.pullMsgs {
		msgs <- m
	}
	return f.pullErr
}
func (f *fakeCreationEngine) RunContainer(engine.RunOptions) error { return f.runErr }
func (f *fakeCreationEngine) RemoveContainer(string) error {
	f.removedContaner = true
	return nil
}

func testCreateConfig() *config.Config {
	return &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "58231"}
}

func TestCreateInstanceSuccessRunsStagesInOrderAndSaves(t *testing.T) {
	eng := &fakeCreationEngine{pullMsgs: []string{"layer 1", "layer 2"}}
	cfg := testCreateConfig()

	var stages []string
	var progress []string
	saved := false
	err := CreateInstance(eng, cfg, func() error {
		saved = true
		return nil
	}, CreateInstanceOptions{
		OnStage:        func(stage string) { stages = append(stages, stage) },
		OnPullProgress: func(line string) { progress = append(progress, line) },
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if !saved {
		t.Fatal("expected save to be called")
	}
	wantStages := []string{"check_availability", "create_home_volume", "create_state_volume", "pull_image", "run_container", "save_config"}
	if len(stages) != len(wantStages) {
		t.Fatalf("stages = %v, want %v", stages, wantStages)
	}
	for i, want := range wantStages {
		if stages[i] != want {
			t.Fatalf("stages[%d] = %s, want %s (full: %v)", i, stages[i], want, stages)
		}
	}
	if len(progress) != 2 || progress[0] != "layer 1" || progress[1] != "layer 2" {
		t.Fatalf("progress = %v, want [layer 1 layer 2]", progress)
	}
	if len(eng.removedVolumes) != 0 || eng.removedContaner {
		t.Fatalf("expected no cleanup on success, got removedVolumes=%v removedContainer=%v", eng.removedVolumes, eng.removedContaner)
	}
}

func TestCreateInstanceFailureBeforeAnyResourceNeedsNoCleanup(t *testing.T) {
	eng := &fakeCreationEngine{containerExists: true}
	cfg := testCreateConfig()

	err := CreateInstance(eng, cfg, func() error { return nil }, CreateInstanceOptions{})
	if err == nil {
		t.Fatal("expected an error when the container name is already taken")
	}
	if len(eng.removedVolumes) != 0 || eng.removedContaner {
		t.Fatalf("expected no cleanup, got removedVolumes=%v removedContainer=%v", eng.removedVolumes, eng.removedContaner)
	}
}

func TestCreateInstanceFailureAfterHomeVolumeCleansUpOnlyThat(t *testing.T) {
	eng := &fakeCreationEngine{}
	cfg := testCreateConfig()

	// Fail create_state_volume: make CreateVolume fail on its second call.
	calls := 0
	failOnSecond := &failSecondCreateEngine{fakeCreationEngine: eng, calls: &calls}

	err := CreateInstance(failOnSecond, cfg, func() error { return nil }, CreateInstanceOptions{})
	if err == nil {
		t.Fatal("expected create_state_volume to fail")
	}
	if len(eng.removedVolumes) != 1 || eng.removedVolumes[0] != cfg.HomeVolumeName() {
		t.Fatalf("removedVolumes = %v, want only the home volume removed", eng.removedVolumes)
	}
	if eng.removedContaner {
		t.Fatal("container was never created, so it must not be removed")
	}
}

// failSecondCreateEngine fails the second CreateVolume call (state volume)
// while letting the first (home volume) succeed.
type failSecondCreateEngine struct {
	*fakeCreationEngine
	calls *int
}

func (f *failSecondCreateEngine) CreateVolume(name string) error {
	*f.calls++
	if *f.calls == 2 {
		return errors.New("disk full")
	}
	return f.fakeCreationEngine.CreateVolume(name)
}

func TestCreateInstanceFailureAfterRunContainerCleansUpEverythingItCreated(t *testing.T) {
	eng := &fakeCreationEngine{runErr: errors.New("port already bound")}
	cfg := testCreateConfig()

	err := CreateInstance(eng, cfg, func() error { return nil }, CreateInstanceOptions{})
	if err == nil {
		t.Fatal("expected run_container to fail")
	}
	wantVolumes := map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true}
	if len(eng.removedVolumes) != 2 {
		t.Fatalf("removedVolumes = %v, want both volumes removed", eng.removedVolumes)
	}
	for _, v := range eng.removedVolumes {
		if !wantVolumes[v] {
			t.Fatalf("unexpected volume removed: %s", v)
		}
	}
	// run_container itself is what failed, so no container was ever created.
	if eng.removedContaner {
		t.Fatal("container was never created, so it must not be removed")
	}
}

func TestCreateInstanceFailureDuringCancellationReportsContextCanceled(t *testing.T) {
	eng := &fakeCreationEngine{runErr: errors.New("signal: killed")}
	cfg := testCreateConfig()

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.SetCancelContext(cancelledCtx)
	defer engine.SetCancelContext(context.Background())

	err := CreateInstance(eng, cfg, func() error { return nil }, CreateInstanceOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want it to satisfy errors.Is(err, context.Canceled)", err)
	}
	if len(eng.removedVolumes) != 2 {
		t.Fatalf("removedVolumes = %v, want both volumes cleaned up despite cancellation", eng.removedVolumes)
	}
}

func TestCreateInstanceUsesTheLocalImageWhenTheRegistryCannotBeReached(t *testing.T) {
	// Setting up again offline, or repairing an installation whose container is
	// gone, needs nothing downloaded. An image named by digest cannot have
	// changed since it was fetched, so the copy already here is the right one.
	eng := &fakeCreationEngine{pullErr: errors.New("pinging container registry: Forbidden"), imagePresent: true}
	cfg := testCreateConfig()

	var progress []string
	err := CreateInstance(eng, cfg, func() error { return nil }, CreateInstanceOptions{
		OnPullProgress: func(line string) { progress = append(progress, line) },
	})
	if err != nil {
		t.Fatalf("expected the local image to be used, got %v", err)
	}
	if len(eng.removedVolumes) != 0 || eng.removedContaner {
		t.Fatal("nothing failed, so nothing should have been cleaned up")
	}
	if len(progress) == 0 || !strings.Contains(progress[len(progress)-1], "already on this computer") {
		t.Fatalf("expected the fallback to be reported, got %v", progress)
	}
}

func TestCreateInstanceFailsWhenTheImageIsNeitherFetchableNorPresent(t *testing.T) {
	eng := &fakeCreationEngine{pullErr: errors.New("pinging container registry: Forbidden")}
	cfg := testCreateConfig()

	err := CreateInstance(eng, cfg, func() error { return nil }, CreateInstanceOptions{})
	if err == nil {
		t.Fatal("expected the unreachable registry to fail when nothing is here to use")
	}
	if len(eng.removedVolumes) != 2 {
		t.Fatalf("removedVolumes = %v, want both volumes cleaned up", eng.removedVolumes)
	}
}
