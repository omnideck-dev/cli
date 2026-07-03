package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")

	cfg := &Config{
		ContainerName: "mybox",
		HomeVolume:    "custom-home",
		StateVolume:   "custom-state",
		ShmSize:       "512m",
		Engine:        "docker",
		Image:         "example.com/img:latest",
		InstalledAt:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.ContainerName != cfg.ContainerName {
		t.Errorf("ContainerName: got %q, want %q", got.ContainerName, cfg.ContainerName)
	}
	if got.HomeVolume != cfg.HomeVolume {
		t.Errorf("HomeVolume: got %q, want %q", got.HomeVolume, cfg.HomeVolume)
	}
	if got.StateVolume != cfg.StateVolume {
		t.Errorf("StateVolume: got %q, want %q", got.StateVolume, cfg.StateVolume)
	}
	if got.ShmSize != cfg.ShmSize {
		t.Errorf("ShmSize: got %q, want %q", got.ShmSize, cfg.ShmSize)
	}
	if got.Engine != cfg.Engine {
		t.Errorf("Engine: got %q, want %q", got.Engine, cfg.Engine)
	}
	if !got.InstalledAt.Equal(cfg.InstalledAt) {
		t.Errorf("InstalledAt: got %v, want %v", got.InstalledAt, cfg.InstalledAt)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	got := expandHome("~/foo/bar")
	want := filepath.Join(home, "foo/bar")
	if got != want {
		t.Errorf("expandHome: got %q, want %q", got, want)
	}

	got2 := expandHome("/absolute/path")
	if got2 != "/absolute/path" {
		t.Errorf("expandHome absolute: got %q, want %q", got2, "/absolute/path")
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath should not be empty")
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config/omnideck-cli/config.yaml")
	if p != expected {
		t.Errorf("DefaultPath: got %q, want %q", p, expected)
	}
}

func TestInstancePath(t *testing.T) {
	p := InstancePath("myapp")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config/omnideck-cli/instances/myapp.yaml")
	if p != expected {
		t.Errorf("InstancePath: got %q, want %q", p, expected)
	}
}

func TestListInstancesEmpty(t *testing.T) {
	// Point InstancesDir at a temp dir to avoid touching real config.
	origDir := instancesDirOverride
	dir := t.TempDir()
	instancesDirOverride = dir
	defer func() { instancesDirOverride = origDir }()

	instances, err := ListInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestListInstancesMultiple(t *testing.T) {
	origDir := instancesDirOverride
	dir := t.TempDir()
	instancesDirOverride = dir
	defer func() { instancesDirOverride = origDir }()

	// Write two instance configs.
	for _, name := range []string{"alpha", "beta"} {
		cfg := &Config{ContainerName: name, Engine: "docker", ShmSize: "256m", Image: "img"}
		if err := Save(filepath.Join(dir, name+".yaml"), cfg); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	instances, err := ListInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}

	names := map[string]bool{}
	for _, inst := range instances {
		names[inst.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected instances alpha and beta, got %v", names)
	}
}

func TestWebUIPortOrDefault(t *testing.T) {
	cfg := &Config{}
	if got := cfg.WebUIPortOrDefault(); got != "2337" {
		t.Errorf("empty WebUIPort: got %q, want '2337'", got)
	}
	cfg.WebUIPort = "8080"
	if got := cfg.WebUIPortOrDefault(); got != "8080" {
		t.Errorf("explicit port: got %q, want '8080'", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ContainerName != "omnideck" {
		t.Errorf("ContainerName: got %q, want 'omnideck'", cfg.ContainerName)
	}
	if cfg.WebUIPort != "2337" {
		t.Errorf("WebUIPort: got %q, want '2337'", cfg.WebUIPort)
	}
	if cfg.Image != DefaultImage {
		t.Errorf("Image: got %q, want %q", cfg.Image, DefaultImage)
	}
	if cfg.HomeVolume != "" || cfg.StateVolume != "" {
		t.Errorf("DefaultConfig should leave volume overrides empty, got %q %q", cfg.HomeVolume, cfg.StateVolume)
	}
	if got := cfg.HomeVolumeName(); got != "omnideck-home" {
		t.Errorf("HomeVolumeName: got %q, want 'omnideck-home'", got)
	}
	if got := cfg.StateVolumeName(); got != "omnideck-state" {
		t.Errorf("StateVolumeName: got %q, want 'omnideck-state'", got)
	}
	if cfg.Memory == "" {
		t.Error("Memory should have a default value")
	}
	if cfg.ShmSize == "" {
		t.Error("ShmSize should have a default value")
	}
}

func TestVolumeNameAccessors(t *testing.T) {
	cfg := &Config{ContainerName: "omnideck2"}
	if got := cfg.HomeVolumeName(); got != "omnideck2-home" {
		t.Errorf("derived HomeVolumeName: got %q", got)
	}
	if got := cfg.StateVolumeName(); got != "omnideck2-state" {
		t.Errorf("derived StateVolumeName: got %q", got)
	}

	cfg.HomeVolume = "custom-home"
	cfg.StateVolume = "custom-state"
	if got := cfg.HomeVolumeName(); got != "custom-home" {
		t.Errorf("explicit HomeVolumeName: got %q", got)
	}
	if got := cfg.StateVolumeName(); got != "custom-state" {
		t.Errorf("explicit StateVolumeName: got %q", got)
	}
}

func TestLoadOldBindMountConfigDerivesVolumes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.yaml")
	data := []byte("container_name: legacy\nshared_dir: /tmp/shared\nstate_dir: /tmp/shared/.state\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.HomeVolumeName(); got != "legacy-home" {
		t.Errorf("HomeVolumeName: got %q", got)
	}
	if got := cfg.StateVolumeName(); got != "legacy-state" {
		t.Errorf("StateVolumeName: got %q", got)
	}
}

func TestMigrateImage(t *testing.T) {
	tests := []struct {
		name      string
		image     string
		want      string
		wantMoved bool
	}{
		{"legacy main tag", "ghcr.io/lefoulkrod/computron_9000:main", DefaultImage, true},
		{"legacy latest tag", "ghcr.io/lefoulkrod/computron_9000:latest", DefaultImage, true},
		{"legacy no tag", "ghcr.io/lefoulkrod/computron_9000", DefaultImage, true},
		{"already current", DefaultImage, DefaultImage, false},
		{"custom override untouched", "ghcr.io/example/omnideck:dev", "ghcr.io/example/omnideck:dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Image: tt.image}
			moved := cfg.MigrateImage()
			if moved != tt.wantMoved {
				t.Errorf("MigrateImage returned %v, want %v", moved, tt.wantMoved)
			}
			if cfg.Image != tt.want {
				t.Errorf("Image = %q, want %q", cfg.Image, tt.want)
			}
		})
	}
}
