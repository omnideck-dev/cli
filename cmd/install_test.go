package cmd

import (
	"path/filepath"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestSaveInstalledConfigRecordsSelectedEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.yaml")
	cfg := config.DefaultConfig()

	if err := saveInstalledConfig(path, cfg, "docker"); err != nil {
		t.Fatalf("saveInstalledConfig: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Engine != "docker" {
		t.Fatalf("Engine = %q, want docker", got.Engine)
	}
	if got.InstalledAt.IsZero() {
		t.Fatal("InstalledAt was not recorded")
	}
}
