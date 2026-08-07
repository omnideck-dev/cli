package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestSetupUsesRuntimeFlagAndHidesLegacyEngineAlias(t *testing.T) {
	if setupCmd.Flags().Lookup("runtime") == nil {
		t.Fatal("setup is missing --runtime")
	}
	legacy := setupCmd.Flags().Lookup("engine")
	if legacy == nil || !legacy.Hidden {
		t.Fatal("the legacy --engine alias must remain available but hidden")
	}

	tests := []struct {
		name          string
		runtimeFlag   string
		legacyFlag    string
		wantErrSubstr string
	}{
		{name: "automatic"},
		{name: "runtime", runtimeFlag: "podman"},
		{name: "legacy alias", legacyFlag: "podman"},
		{name: "matching flags", runtimeFlag: "podman", legacyFlag: "podman"},
		{name: "conflicting flags", runtimeFlag: "docker", legacyFlag: "podman", wantErrSubstr: "use only --runtime"},
		{name: "Docker ignored", runtimeFlag: "docker", wantErrSubstr: "is ignored"},
		{name: "invalid runtime", runtimeFlag: "containerd", wantErrSubstr: "must be podman"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSetupRuntimeFlags(tt.runtimeFlag, tt.legacyFlag)
			if tt.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("error = %v, want text %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSetupRuntimeFlags() error = %v", err)
			}
		})
	}
}

func TestSaveInstalledConfigRecordsMachineWideRuntime(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "instance.yaml")
	cfg := config.DefaultConfig()

	if err := saveInstalledConfig(path, cfg, "podman"); err != nil {
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
	if settings.Runtime != "podman" {
		t.Fatalf("machine-wide Runtime = %q, want podman", settings.Runtime)
	}
}
