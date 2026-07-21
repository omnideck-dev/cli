package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

func TestChooseInteractiveStart(t *testing.T) {
	installed := &config.Config{ContainerName: "omnideck"}
	instances := []config.InstanceInfo{{Name: "omnideck", Config: installed}}

	tests := []struct {
		name           string
		loaded         *config.Config
		instances      []config.InstanceInfo
		listErr        error
		runtimeReady   bool
		instanceBroken bool
		want           interactiveStart
	}{
		{name: "brand new", want: interactiveStartSetup},
		{name: "installed and ready", loaded: installed, instances: instances, runtimeReady: true, want: interactiveStartDashboard},
		{name: "installed container missing", loaded: installed, instances: instances, runtimeReady: true, instanceBroken: true, want: interactiveStartDoctor},
		{name: "installed runtime needs setup", loaded: installed, instances: instances, want: interactiveStartRuntimeSetup},
		{name: "legacy config falls back safely", loaded: installed, want: interactiveStartDashboard},
		{name: "instance listing failed", listErr: errors.New("permission denied"), want: interactiveStartDashboard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseInteractiveStart(tt.loaded, tt.instances, tt.listErr, tt.runtimeReady, tt.instanceBroken); got != tt.want {
				t.Fatalf("chooseInteractiveStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakeContainerLookup struct {
	exists map[string]bool
	err    error
}

func (f fakeContainerLookup) ContainerExists(name string) (bool, error) {
	return f.exists[name], f.err
}

func TestFirstBrokenInstanceSelectsTheAffectedInstallation(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "one", Config: &config.Config{ContainerName: "one"}},
		{Name: "two", Config: &config.Config{ContainerName: "two"}},
	}
	eng := fakeContainerLookup{exists: map[string]bool{"one": true, "two": false}}
	if got := firstBrokenInstance(eng, instances); got != 1 {
		t.Fatalf("firstBrokenInstance() = %d, want 1", got)
	}
	eng.exists["two"] = true
	if got := firstBrokenInstance(eng, instances); got != -1 {
		t.Fatalf("healthy firstBrokenInstance() = %d, want -1", got)
	}
}

func TestExplicitMissingNameNeverFallsBackToAnotherInstance(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	if err := config.Save(config.InstancePath("existing"), &config.Config{ContainerName: "existing"}); err != nil {
		t.Fatal(err)
	}
	originalLoaded, originalPath := LoadedConfig, ConfigPath
	originalName, originalCfgPath := nameFlag, cfgPath
	defer func() {
		LoadedConfig, ConfigPath = originalLoaded, originalPath
		nameFlag, cfgPath = originalName, originalCfgPath
	}()
	LoadedConfig = nil
	nameFlag = "missing"
	cfgPath = ""

	err := requireConfigMulti()
	if err == nil || !strings.Contains(err.Error(), `no Omnideck installation named "missing"`) {
		t.Fatalf("requireConfigMulti() = %v", err)
	}
	if LoadedConfig != nil {
		t.Fatal("an explicit missing selector must not resolve a different instance")
	}
}

func TestNonInteractiveMultipleInstancesRequireAName(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	for _, name := range []string{"one", "two"} {
		if err := config.Save(config.InstancePath(name), &config.Config{ContainerName: name}); err != nil {
			t.Fatal(err)
		}
	}
	originalLoaded, originalPath := LoadedConfig, ConfigPath
	originalName, originalCfgPath := nameFlag, cfgPath
	defer func() {
		LoadedConfig, ConfigPath = originalLoaded, originalPath
		nameFlag, cfgPath = originalName, originalCfgPath
	}()
	LoadedConfig = nil
	nameFlag, cfgPath = "", ""

	err := requireConfigMulti()
	if err == nil || !strings.Contains(err.Error(), "choose one with --name") {
		t.Fatalf("requireConfigMulti() = %v", err)
	}
}

func TestConfiguredEngineName(t *testing.T) {
	originalRuntime := RuntimeName
	RuntimeName = ""
	defer func() { RuntimeName = originalRuntime }()
	instances := []config.InstanceInfo{
		{Name: "omnideck", Config: &config.Config{Engine: "podman"}},
	}

	if got := configuredEngineName(&config.Config{Engine: "docker"}, instances); got != "docker" {
		t.Fatalf("explicitly loaded engine = %q, want docker", got)
	}
	if got := configuredEngineName(nil, instances); got != "podman" {
		t.Fatalf("instance engine = %q, want podman", got)
	}
	if got := configuredEngineName(nil, nil); got != "" {
		t.Fatalf("missing engine = %q, want empty", got)
	}
	RuntimeName = "docker"
	if got := configuredEngineName(nil, instances); got != "docker" {
		t.Fatalf("machine-wide runtime = %q, want docker", got)
	}
}

func TestReadyEngineFromProbesHonorsTheSharedRuntime(t *testing.T) {
	probes := []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeReady},
	}
	eng, err := readyEngineFromProbes("docker", probes)
	if err != nil || eng == nil || eng.Name() != "docker" {
		t.Fatalf("ready Docker = %v, %v", eng, err)
	}
	if _, err := readyEngineFromProbes("podman", probes); err == nil || !strings.Contains(err.Error(), "Podman: Not installed") {
		t.Fatalf("missing saved Podman error = %v", err)
	}
	if eng, err := readyEngineFromProbes("", probes); err != nil || eng.Name() != "docker" {
		t.Fatalf("automatic ready engine = %v, %v", eng, err)
	}
}

func TestOneLegacyRuntime(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "one", Config: &config.Config{Engine: "docker"}},
		{Name: "two", Config: &config.Config{Engine: "docker"}},
	}
	if got, ok := oneLegacyRuntime(nil, instances); !ok || got != "docker" {
		t.Fatalf("oneLegacyRuntime() = %q, %v, want docker, true", got, ok)
	}
	instances[1].Config.Engine = "podman"
	if got, ok := oneLegacyRuntime(nil, instances); ok || got != "" {
		t.Fatalf("mixed oneLegacyRuntime() = %q, %v, want empty, false", got, ok)
	}
}

func TestInstallRemainsAnAliasForSetup(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"install"})
	if err != nil {
		t.Fatalf("Find(install): %v", err)
	}
	if command.Name() != "setup" {
		t.Fatalf("install resolves to %q, want setup", command.Name())
	}
}
