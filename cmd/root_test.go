package cmd

import (
	"errors"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestChooseInteractiveStart(t *testing.T) {
	installed := &config.Config{ContainerName: "omnideck"}
	instances := []config.InstanceInfo{{Name: "omnideck", Config: installed}}

	tests := []struct {
		name         string
		loaded       *config.Config
		instances    []config.InstanceInfo
		listErr      error
		runtimeReady bool
		want         interactiveStart
	}{
		{name: "brand new", want: interactiveStartInstall},
		{name: "installed and ready", loaded: installed, instances: instances, runtimeReady: true, want: interactiveStartLauncher},
		{name: "installed runtime needs setup", loaded: installed, instances: instances, want: interactiveStartRuntimeSetup},
		{name: "legacy config falls back safely", loaded: installed, want: interactiveStartLauncher},
		{name: "instance listing failed", listErr: errors.New("permission denied"), want: interactiveStartLauncher},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseInteractiveStart(tt.loaded, tt.instances, tt.listErr, tt.runtimeReady); got != tt.want {
				t.Fatalf("chooseInteractiveStart() = %v, want %v", got, tt.want)
			}
		})
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
