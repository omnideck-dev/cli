package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

type fakeContainerEngine struct {
	exists      bool
	status      string
	started     int
	stopped     int
	removed     int
	runOptions  []engine.RunOptions
	runErrors   []error
	existsError error
}

func (f *fakeContainerEngine) ContainerExists(string) (bool, error)   { return f.exists, f.existsError }
func (f *fakeContainerEngine) ContainerStatus(string) (string, error) { return f.status, nil }
func (f *fakeContainerEngine) StartContainer(string) error            { f.started++; return nil }
func (f *fakeContainerEngine) StopContainer(string) error {
	f.stopped++
	f.status = "exited"
	return nil
}
func (f *fakeContainerEngine) RemoveContainer(string) error {
	f.removed++
	f.exists = false
	return nil
}
func (f *fakeContainerEngine) RunContainer(opts engine.RunOptions) error {
	f.runOptions = append(f.runOptions, opts)
	if len(f.runErrors) > 0 {
		err := f.runErrors[0]
		f.runErrors = f.runErrors[1:]
		if err != nil {
			f.exists = true // model a partially created container
			return err
		}
	}
	f.exists = true
	f.status = "running"
	return nil
}

func TestEnsureLifecycleOperationsAreIdempotent(t *testing.T) {
	eng := &fakeContainerEngine{exists: true, status: "exited"}
	if changed, err := EnsureStopped(eng, "omnideck"); err != nil || changed || eng.stopped != 0 {
		t.Fatalf("already stopped: changed=%v stopped=%d err=%v", changed, eng.stopped, err)
	}
	if changed, err := EnsureStarted(eng, "omnideck"); err != nil || !changed || eng.started != 1 {
		t.Fatalf("start: changed=%v started=%d err=%v", changed, eng.started, err)
	}
	eng.status = "running"
	if changed, err := EnsureStarted(eng, "omnideck"); err != nil || changed || eng.started != 1 {
		t.Fatalf("already running: changed=%v started=%d err=%v", changed, eng.started, err)
	}
	eng.exists = false
	if changed, err := EnsureRemoved(eng, "omnideck"); err != nil || changed || eng.removed != 0 {
		t.Fatalf("already removed: changed=%v removed=%d err=%v", changed, eng.removed, err)
	}
}

func TestRunOptionsLeavesContainerFacingHostToEngine(t *testing.T) {
	cfg := config.DefaultConfig()
	opts := RunOptions(cfg)
	if opts.OllamaHost != "" {
		t.Fatalf("OllamaHost = %q, want engine default", opts.OllamaHost)
	}
	if opts.Name != cfg.ContainerName || opts.WebUIPort != cfg.WebUIPortOrDefault() {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestRecreateRestoresPreviousConfigWhenReplacementFails(t *testing.T) {
	oldCfg := config.DefaultConfig()
	newCfg := *oldCfg
	newCfg.WebUIPort = "2448"
	eng := &fakeContainerEngine{
		exists:    true,
		status:    "running",
		runErrors: []error{errors.New("port is busy"), nil},
	}
	err := Recreate(eng, oldCfg, &newCfg)
	if err == nil || !strings.Contains(err.Error(), "previous settings were restored") {
		t.Fatalf("Recreate() error = %v", err)
	}
	if len(eng.runOptions) != 2 || eng.runOptions[0].WebUIPort != "2448" || eng.runOptions[1].WebUIPort != oldCfg.WebUIPortOrDefault() {
		t.Fatalf("run sequence = %#v", eng.runOptions)
	}
}

func TestRecreateAndSaveRestoresContainerWhenSaveFails(t *testing.T) {
	oldCfg := config.DefaultConfig()
	newCfg := *oldCfg
	newCfg.Memory = "4g"
	eng := &fakeContainerEngine{exists: true, status: "running"}

	// A directory cannot be overwritten as the YAML file, forcing the save
	// failure after the replacement container has started.
	err := RecreateAndSave(eng, oldCfg, &newCfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "previous container was restored") {
		t.Fatalf("RecreateAndSave() error = %v", err)
	}
	if len(eng.runOptions) != 2 || eng.runOptions[0].Memory != "4g" || eng.runOptions[1].Memory != oldCfg.Memory {
		t.Fatalf("run sequence = %#v", eng.runOptions)
	}
}
