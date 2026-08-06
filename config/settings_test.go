package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSettingsMissingIsEmpty(t *testing.T) {
	original := settingsPathOverride
	settingsPathOverride = filepath.Join(t.TempDir(), "settings.yaml")
	defer func() { settingsPathOverride = original }()

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Runtime != "" {
		t.Fatalf("Runtime = %q, want empty", settings.Runtime)
	}
}

func TestSaveAndLoadRuntime(t *testing.T) {
	original := settingsPathOverride
	settingsPathOverride = filepath.Join(t.TempDir(), "nested", "settings.yaml")
	defer func() { settingsPathOverride = original }()

	if err := SaveRuntime("podman"); err != nil {
		t.Fatalf("SaveRuntime: %v", err)
	}
	if runtime.GOOS != "windows" {
		assertPrivateMode(t, filepath.Dir(settingsPathOverride), 0o700)
		assertPrivateMode(t, settingsPathOverride, 0o600)
	}
	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Runtime != "podman" {
		t.Fatalf("Runtime = %q, want podman", settings.Runtime)
	}
}

func TestLegacyDockerSettingMigratesToPodman(t *testing.T) {
	original := settingsPathOverride
	settingsPathOverride = filepath.Join(t.TempDir(), "settings.yaml")
	defer func() { settingsPathOverride = original }()

	if err := os.WriteFile(settingsPathOverride, []byte("runtime: docker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Runtime != "podman" {
		t.Fatalf("legacy Runtime = %q, want podman", settings.Runtime)
	}
	if err := SaveRuntime("docker"); err == nil {
		t.Fatal("new settings must reject Docker")
	}
}

func TestRuntimeValidation(t *testing.T) {
	original := settingsPathOverride
	settingsPathOverride = filepath.Join(t.TempDir(), "settings.yaml")
	defer func() { settingsPathOverride = original }()

	if err := SaveRuntime("containerd"); err == nil {
		t.Fatal("SaveRuntime should reject an unsupported runtime")
	}
	if err := os.WriteFile(settingsPathOverride, []byte("runtime: containerd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(); err == nil {
		t.Fatal("LoadSettings should reject an unsupported runtime")
	}
}
