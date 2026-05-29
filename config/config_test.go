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
		SharedDir:     "/tmp/shared",
		StateDir:      "/tmp/state",
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
