package tui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestConfigApplySavesOnlyAfterContainerStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omnideck.yaml")
	current := config.DefaultConfig()
	if err := config.Save(path, current); err != nil {
		t.Fatal(err)
	}
	candidate := *current
	candidate.Memory = "4g"
	eng := &mockEngine{containerExists: true, containerStatus: "running"}

	msg := applySettingsCmd(current, &candidate, path, eng, 0)().(settingsApplyDoneMsg)
	if msg.err != nil {
		t.Fatalf("applySettingsCmd: %v", msg.err)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Memory != "4g" || msg.cfg == nil || msg.cfg.Memory != "4g" {
		t.Fatalf("saved/message config = %#v / %#v", saved, msg.cfg)
	}
	if eng.lastRunOptions.OllamaHost != "" {
		t.Fatalf("container-facing Ollama host must be selected by the engine, got %q", eng.lastRunOptions.OllamaHost)
	}
}

func TestConfigApplyFailureKeepsSavedConfigUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omnideck.yaml")
	current := config.DefaultConfig()
	if err := config.Save(path, current); err != nil {
		t.Fatal(err)
	}
	candidate := *current
	candidate.Memory = "4g"
	eng := &mockEngine{
		containerExists: true,
		containerStatus: "running",
		runErrors:       []error{errors.New("replacement failed"), nil},
	}

	msg := applySettingsCmd(current, &candidate, path, eng, 0)().(settingsApplyDoneMsg)
	if msg.err == nil {
		t.Fatal("expected replacement failure")
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Memory != current.Memory {
		t.Fatalf("saved config changed after failed apply: memory=%q", saved.Memory)
	}
}
