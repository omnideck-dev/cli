package workflow

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

type fakeEnsureEngine struct {
	fakeContainerEngine
	volumes       map[string]bool
	created       []string
	removedVolume []string
	pulls         []string
	pullErr       error
	volumeErr     error
	// localImages lists images ImageExists reports as stored locally.
	localImages map[string]bool
	imageErr    error
}

func (f *fakeEnsureEngine) ImageExists(image string) (bool, error) {
	return f.localImages[image], f.imageErr
}

func (f *fakeEnsureEngine) VolumeExists(name string) (bool, error) {
	return f.volumes[name], f.volumeErr
}

func (f *fakeEnsureEngine) CreateVolume(name string) (bool, error) {
	if f.volumes == nil {
		f.volumes = map[string]bool{}
	}
	if f.volumes[name] {
		return false, nil
	}
	f.volumes[name] = true
	f.created = append(f.created, name)
	return true, nil
}

func (f *fakeEnsureEngine) RemoveVolume(name string) error {
	delete(f.volumes, name)
	f.removedVolume = append(f.removedVolume, name)
	return nil
}

func (f *fakeEnsureEngine) PullImage(image string, _ chan<- string) error {
	f.pulls = append(f.pulls, image)
	return f.pullErr
}

func TestEnsureInstanceCancellationCleansUpAndPreservesCancellation(t *testing.T) {
	desired := desiredTestConfig(t)
	eng := &fakeEnsureEngine{
		volumes: map[string]bool{},
		pullErr: errors.New("signal: killed"),
	}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.SetCancelContext(cancelledCtx)
	defer engine.SetCancelContext(context.Background())

	_, err := EnsureInstance(eng, nil, desired, func() error { return nil }, EnsureInstanceOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want it to satisfy errors.Is(err, context.Canceled)", err)
	}
	if !errors.Is(err, ErrImageDownload) {
		t.Fatalf("err = %v, want ErrImageDownload", err)
	}
	if len(eng.removedVolume) != 2 {
		t.Fatalf("removedVolume = %v, want both newly created volumes removed", eng.removedVolume)
	}
	if !engine.CancelRequested() {
		t.Fatal("cleanup did not restore the caller's cancelled context")
	}
}

func availableTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(port)
}

func desiredTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.ContainerName = "omnideck-desktop"
	cfg.Image = "ghcr.io/omnideck-dev/omnideck@sha256:test"
	cfg.WebUIPort = availableTestPort(t)
	return cfg
}

func TestEnsureInstanceMatchingRunningEnvironmentIsANoop(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{
			exists: true,
			status: "running",
			env:    []string{"OMNIDECK_EXTERNAL_URL=" + desired.BrowserURL()},
		},
		volumes: map[string]bool{
			desired.HomeVolumeName():  true,
			desired.StateVolumeName(): true,
		},
	}
	saved := false
	result, err := EnsureInstance(eng, &current, desired, func() error {
		saved = true
		return nil
	}, EnsureInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != "unchanged" {
		t.Fatalf("result = %#v", result)
	}
	if saved || len(eng.pulls) != 0 || len(eng.runOptions) != 0 {
		t.Fatal("matching environment performed install work")
	}
}

func TestEnsureInstanceFreshCreatesStorageContainerAndConfig(t *testing.T) {
	desired := desiredTestConfig(t)
	eng := &fakeEnsureEngine{volumes: map[string]bool{}}
	saved := false
	result, err := EnsureInstance(eng, nil, desired, func() error {
		saved = true
		return nil
	}, EnsureInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Action != "created" || !saved {
		t.Fatalf("result=%#v saved=%v", result, saved)
	}
	if len(eng.created) != 2 || len(eng.pulls) != 1 || len(eng.runOptions) != 1 {
		t.Fatalf("created=%v pulls=%v runs=%d", eng.created, eng.pulls, len(eng.runOptions))
	}
}

func TestEnsureInstanceChangedConfigRecreatesAndSaves(t *testing.T) {
	current := desiredTestConfig(t)
	desired := *current
	desired.Image = "ghcr.io/omnideck-dev/omnideck@sha256:new"
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running"},
		volumes: map[string]bool{
			current.HomeVolumeName():  true,
			current.StateVolumeName(): true,
		},
	}
	saved := false
	result, err := EnsureInstance(eng, current, &desired, func() error {
		saved = true
		return nil
	}, EnsureInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "recreated" || !result.Changed || !saved {
		t.Fatalf("result=%#v saved=%v", result, saved)
	}
	if eng.stopped != 1 || eng.removed != 1 || len(eng.runOptions) != 1 {
		t.Fatalf("stopped=%d removed=%d runs=%d", eng.stopped, eng.removed, len(eng.runOptions))
	}
	if eng.runOptions[0].Image != desired.Image {
		t.Fatalf("replacement image = %q", eng.runOptions[0].Image)
	}
}

func TestEnsureInstanceRefusesToReplaceMissingSavedStorage(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: false},
		volumes: map[string]bool{
			desired.HomeVolumeName(): true,
		},
	}
	_, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if !errors.Is(err, ErrMissingStorage) {
		t.Fatalf("err = %v, want ErrMissingStorage", err)
	}
	if len(eng.created) != 0 || len(eng.runOptions) != 0 {
		t.Fatalf("repair mutated storage/container: created=%v runs=%d", eng.created, len(eng.runOptions))
	}
}

func TestEnsureInstanceUpgradesContainerWiringOnceWithoutRemovingStorage(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	current.LayoutVersion = 1 // Before the host-local MCP callback environment.
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running"},
		volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
	}
	result, err := EnsureInstance(eng, &current, desired, func() error {
		current = *desired
		return nil
	}, EnsureInstanceOptions{})
	if err != nil || result.Action != "recreated" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(eng.created) != 0 || len(eng.removedVolume) != 0 || len(eng.runOptions) != 1 {
		t.Fatalf("created=%v removed=%v runs=%d", eng.created, eng.removedVolume, len(eng.runOptions))
	}
	if eng.runOptions[0].HomeVolume != desired.HomeVolumeName() || eng.runOptions[0].StateVolume != desired.StateVolumeName() {
		t.Fatal("container replacement changed the saved data volumes")
	}
	result, err = EnsureInstance(eng, &current, desired, func() error {
		t.Fatal("matching layout should not rewrite config")
		return nil
	}, EnsureInstanceOptions{})
	if err != nil || result.Action != "unchanged" || len(eng.runOptions) != 1 {
		t.Fatalf("second reconciliation result=%#v err=%v runs=%d", result, err, len(eng.runOptions))
	}
}

func TestEnsureInstanceRecreatesMatchingContainerWithoutExternalURL(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        []string
		wantReason string
	}{
		{"missing", []string{"PORT=8080"}, EnsureReasonExternalURLMissing},
		{"mismatch", nil, EnsureReasonExternalURLMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := desiredTestConfig(t)
			current := *desired
			if tc.env == nil {
				// Same port, wrong origin: a real drift, not a saved port change.
				tc.env = []string{"OMNIDECK_EXTERNAL_URL=http://127.0.0.1:" + desired.WebUIPortOrDefault()}
			}
			eng := &fakeEnsureEngine{
				fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: tc.env, imageID: "sha256:running"},
				volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
			}
			var stages []string
			result, err := EnsureInstance(eng, &current, desired, func() error {
				t.Fatal("repairing the sign-in address should not rewrite config")
				return nil
			}, EnsureInstanceOptions{OnStage: func(stage string) { stages = append(stages, stage) }})
			if err != nil || result.Action != "recreated" || result.Reason != tc.wantReason {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if want := []string{"check_environment", "replace_container"}; !slices.Equal(stages, want) {
				t.Fatalf("stages = %v, want %v", stages, want)
			}
			if len(eng.created) != 0 || len(eng.removedVolume) != 0 || len(eng.runOptions) != 1 {
				t.Fatalf("created=%v removed=%v runs=%d", eng.created, eng.removedVolume, len(eng.runOptions))
			}
			if len(eng.pulls) != 0 {
				t.Fatalf("repairing the sign-in address pulled %v; it must reuse the current image", eng.pulls)
			}
			if eng.runOptions[0].ExternalURL != desired.BrowserURL() {
				t.Fatalf("replacement ExternalURL = %q, want %q", eng.runOptions[0].ExternalURL, desired.BrowserURL())
			}
			// Another instance's update may have moved the saved tag; the
			// repair must keep running the exact image, not follow the tag.
			if eng.runOptions[0].Image != "sha256:running" {
				t.Fatalf("replacement image = %q, want the running image ID", eng.runOptions[0].Image)
			}
			result, err = EnsureInstance(eng, &current, desired, func() error {
				t.Fatal("a corrected container should not rewrite config")
				return nil
			}, EnsureInstanceOptions{})
			if err != nil || result.Action != "unchanged" || len(eng.runOptions) != 1 {
				t.Fatalf("second reconciliation result=%#v err=%v runs=%d", result, err, len(eng.runOptions))
			}
		})
	}
}

func TestEnsureInstanceLeavesContainerAloneWhenEnvironmentIsUnreadable(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", envErr: errors.New("inspect timed out")},
		volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
	}
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if err != nil || result.Action != "unchanged" || len(eng.runOptions) != 0 {
		t.Fatalf("result=%#v err=%v runs=%d", result, err, len(eng.runOptions))
	}
}

func TestEnsureInstanceLeavesContainerAloneWhenImageIsUnreadable(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: []string{"PORT=8080"}, imageIDErr: errors.New("inspect timed out")},
	}
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if err != nil || result.Action != "unchanged" || len(eng.runOptions) != 0 || len(eng.pulls) != 0 {
		t.Fatalf("result=%#v err=%v runs=%d pulls=%v", result, err, len(eng.runOptions), eng.pulls)
	}
}

// A transient storage check error must not fail ensure for a healthy
// container: it is left alone and started as before.
func TestEnsureInstanceLeavesDriftedContainerAloneWhenStorageIsUnreadable(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: []string{"PORT=8080"}, imageID: "sha256:running"},
		volumeErr:           errors.New("machine connection timed out"),
	}
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if err != nil || result.Action != "unchanged" || len(eng.runOptions) != 0 {
		t.Fatalf("result=%#v err=%v runs=%d", result, err, len(eng.runOptions))
	}
}

// A drifted container that does not mount the saved volumes must not be
// replaced: Podman would create the missing volume empty.
func TestEnsureInstanceStopsSignInRepairWhenSavedVolumeIsMissing(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: []string{"PORT=8080"}, imageID: "sha256:running"},
		volumes:             map[string]bool{current.HomeVolumeName(): true},
	}
	_, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if !errors.Is(err, ErrMissingStorage) || eng.removed != 0 || len(eng.runOptions) != 0 {
		t.Fatalf("err=%v removed=%d runs=%d", err, eng.removed, len(eng.runOptions))
	}
}

func TestEnsureInstanceKeepsStoppedDriftedContainerWhenPortIsTaken(t *testing.T) {
	desired := desiredTestConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:"+desired.WebUIPortOrDefault())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "exited", env: []string{"PORT=8080"}},
		volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
	}
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if eng.removed != 0 || len(eng.runOptions) != 0 || result.Action != "started" {
		t.Fatalf("result=%#v removed=%d runs=%d; a busy port must not risk removing the container", result, eng.removed, len(eng.runOptions))
	}
}

func TestEnsureInstanceLeavesSavedPortChangeForUpdate(t *testing.T) {
	desired := desiredTestConfig(t)
	current := *desired
	// `config set web_ui_port` saved a new port; the container still runs on
	// the old one. Only `update` applies saved settings.
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: []string{"OMNIDECK_EXTERNAL_URL=http://localhost:1"}, imageID: "sha256:running"},
		volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
	}
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{})
	if err != nil || result.Action != "unchanged" || result.Reason != "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if eng.stopped != 0 || eng.removed != 0 || len(eng.runOptions) != 0 {
		t.Fatalf("stopped=%d removed=%d runs=%d; a saved port change is not drift", eng.stopped, eng.removed, len(eng.runOptions))
	}
}

func TestEnsureInstanceKeepsRunningDriftedContainerWhenPortIsTaken(t *testing.T) {
	desired := desiredTestConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:"+desired.WebUIPortOrDefault())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	current := *desired
	eng := &fakeEnsureEngine{
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running", env: []string{"PORT=8080"}, imageID: "sha256:running"},
		volumes:             map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
	}
	var stages []string
	result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{OnStage: func(stage string) { stages = append(stages, stage) }})
	if err != nil {
		t.Fatal(err)
	}
	if eng.removed != 0 || len(eng.runOptions) != 0 || result.Changed {
		t.Fatalf("result=%#v removed=%d runs=%d; another app on the port must not cost the container", result, eng.removed, len(eng.runOptions))
	}
	if eng.stopped != 1 || eng.started != 1 {
		t.Fatalf("stopped=%d started=%d, want the container stopped to check its port and started again", eng.stopped, eng.started)
	}
	if want := []string{"check_environment", "start_container"}; !slices.Equal(stages, want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
}

func TestEnsureInstanceReuseLocalImageSkipsDownloadOnlyWhenStored(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stored    bool
		checkErr  error
		wantPulls int
	}{
		{"stored", true, nil, 0},
		{"not stored", false, nil, 1},
		{"check failed", true, errors.New("podman image exists: timed out"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := desiredTestConfig(t)
			current := *desired
			eng := &fakeEnsureEngine{
				volumes:     map[string]bool{current.HomeVolumeName(): true, current.StateVolumeName(): true},
				localImages: map[string]bool{desired.Image: tc.stored},
				imageErr:    tc.checkErr,
			}
			result, err := EnsureInstance(eng, &current, desired, func() error { return nil }, EnsureInstanceOptions{ReuseLocalImage: true})
			if err != nil || result.Action != "repaired" {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if len(eng.pulls) != tc.wantPulls {
				t.Fatalf("pulls = %v, want %d", eng.pulls, tc.wantPulls)
			}
		})
	}
}
