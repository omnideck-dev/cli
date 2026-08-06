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
		want          string
		wantErrSubstr string
	}{
		{name: "automatic", want: ""},
		{name: "runtime", runtimeFlag: "podman", want: "podman"},
		{name: "legacy alias", legacyFlag: "podman", want: "podman"},
		{name: "matching flags", runtimeFlag: "podman", legacyFlag: "podman", want: "podman"},
		{name: "conflicting flags", runtimeFlag: "docker", legacyFlag: "podman", wantErrSubstr: "use only --runtime"},
		{name: "Docker rejected", runtimeFlag: "docker", wantErrSubstr: "must be podman"},
		{name: "invalid runtime", runtimeFlag: "containerd", wantErrSubstr: "must be podman"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := setupRuntimeOverride(tt.runtimeFlag, tt.legacyFlag)
			if tt.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("error = %v, want text %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("setupRuntimeOverride = %q, %v; want %q", got, err, tt.want)
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

func TestFreshSetupCanChooseAgainAfterLastInstanceIsRemoved(t *testing.T) {
	got, err := setupRuntimePreference("podman", "", 0)
	if err != nil || got != "" {
		t.Fatalf("fresh setup preference = %q, %v; want an open choice", got, err)
	}
	got, err = setupRuntimePreference("podman", "podman", 0)
	if err != nil || got != "podman" {
		t.Fatalf("fresh requested preference = %q, %v; want podman", got, err)
	}
}

func TestExistingInstancesKeepTheirSharedRuntime(t *testing.T) {
	got, err := setupRuntimePreference("podman", "", 1)
	if err != nil || got != "podman" {
		t.Fatalf("existing preference = %q, %v; want podman", got, err)
	}
	if _, err := setupRuntimePreference("podman", "docker", 1); err == nil {
		t.Fatal("an existing installation must not accept Docker")
	}
}
