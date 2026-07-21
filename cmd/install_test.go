package cmd

import (
	"path/filepath"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestSaveInstalledConfigRecordsMachineWideRuntime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "instance.yaml")
	cfg := config.DefaultConfig()

	if err := saveInstalledConfig(path, cfg, "docker"); err != nil {
		t.Fatalf("saveInstalledConfig: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Engine != "" {
		t.Fatalf("legacy per-instance Engine = %q, want empty", got.Engine)
	}
	if got.InstalledAt.IsZero() {
		t.Fatal("InstalledAt was not recorded")
	}
	settings, err := config.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Runtime != "docker" {
		t.Fatalf("machine-wide Runtime = %q, want docker", settings.Runtime)
	}
}
