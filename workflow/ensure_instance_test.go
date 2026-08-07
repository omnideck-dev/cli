package workflow

import (
	"context"
	"errors"
	"net"
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
}

func (f *fakeEnsureEngine) VolumeExists(name string) (bool, error) {
	return f.volumes[name], nil
}

func (f *fakeEnsureEngine) CreateVolume(name string) error {
	if f.volumes == nil {
		f.volumes = map[string]bool{}
	}
	f.volumes[name] = true
	f.created = append(f.created, name)
	return nil
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
		fakeContainerEngine: fakeContainerEngine{exists: true, status: "running"},
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
