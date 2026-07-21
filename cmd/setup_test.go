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

func TestFreshSetupCanChooseAgainAfterLastInstanceIsRemoved(t *testing.T) {
	got, err := setupRuntimePreference("docker", "", 0)
	if err != nil || got != "" {
		t.Fatalf("fresh setup preference = %q, %v; want an open choice", got, err)
	}
	got, err = setupRuntimePreference("docker", "podman", 0)
	if err != nil || got != "podman" {
		t.Fatalf("fresh requested preference = %q, %v; want podman", got, err)
	}
}

func TestExistingInstancesKeepTheirSharedRuntime(t *testing.T) {
	got, err := setupRuntimePreference("docker", "", 1)
	if err != nil || got != "docker" {
		t.Fatalf("existing preference = %q, %v; want docker", got, err)
	}
	if _, err := setupRuntimePreference("docker", "podman", 1); err == nil {
		t.Fatal("an existing Docker installation must not silently switch to Podman")
	}
}
