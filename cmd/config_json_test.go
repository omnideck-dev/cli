package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
)

// withJSONConfigState swaps the package-level config/runtime/json globals for
// the duration of a test and restores them afterward, since runConfigShow/
// runConfigSet/runConfigPath read those directly rather than taking params.
func withJSONConfigState(t *testing.T, cfg *config.Config, cfgPath, runtime string) {
	t.Helper()
	origLoaded, origPath, origRuntime, origJSON := LoadedConfig, ConfigPath, RuntimeName, jsonFlag
	t.Cleanup(func() {
		LoadedConfig, ConfigPath, RuntimeName, jsonFlag = origLoaded, origPath, origRuntime, origJSON
	})
	LoadedConfig = cfg
	ConfigPath = cfgPath
	RuntimeName = runtime
	jsonFlag = true
}

func TestRunConfigShowJSONEmitsConfigPayload(t *testing.T) {
	withJSONConfigState(t, &config.Config{
		ContainerName: "demo",
		Memory:        "2g",
		ShmSize:       "512m",
		WebUIPort:     "2337",
		Image:         "ghcr.io/omnideck-dev/omnideck:latest",
		InstalledAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}, filepath.Join(t.TempDir(), "demo.yaml"), "docker")

	out := captureStdout(t, func() {
		if err := runConfigShow(nil, nil); err != nil {
			t.Fatalf("runConfigShow: %v", err)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%s", err, out)
	}
	if decoded["containerName"] != "demo" {
		t.Fatalf("containerName = %v, want demo", decoded["containerName"])
	}
	if decoded["runtime"] != "docker" {
		t.Fatalf("runtime = %v, want docker", decoded["runtime"])
	}
	if decoded["memory"] != "2g" {
		t.Fatalf("memory = %v, want 2g", decoded["memory"])
	}
}

func TestRunConfigSetJSONEmitsUpdatedConfigPayload(t *testing.T) {
	withJSONConfigState(t, &config.Config{
		ContainerName: "demo",
		Memory:        "2g",
		WebUIPort:     "2337",
		Image:         "ghcr.io/omnideck-dev/omnideck:latest",
	}, filepath.Join(t.TempDir(), "demo.yaml"), "docker")

	out := captureStdout(t, func() {
		if err := runConfigSet(nil, []string{"memory", "4g"}); err != nil {
			t.Fatalf("runConfigSet: %v", err)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%s", err, out)
	}
	if decoded["memory"] != "4g" {
		t.Fatalf("memory = %v, want 4g", decoded["memory"])
	}
	if LoadedConfig.Memory != "4g" {
		t.Fatalf("LoadedConfig.Memory = %q, want 4g", LoadedConfig.Memory)
	}
}

func TestRunConfigSetJSONInvalidKeyReturnsStructuredError(t *testing.T) {
	withJSONConfigState(t, &config.Config{ContainerName: "demo"}, filepath.Join(t.TempDir(), "demo.yaml"), "docker")

	out := captureStdout(t, func() {
		err := runConfigSet(nil, []string{"container_name", "other"})
		if err != errAborted {
			t.Fatalf("expected errAborted, got %v", err)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%s", err, out)
	}
	if _, ok := decoded["error"]; !ok {
		t.Fatalf("expected a structured error envelope, got %s", out)
	}
}

func TestRunConfigPathJSONEmitsPathPayload(t *testing.T) {
	wantPath := filepath.Join(t.TempDir(), "demo.yaml")
	withJSONConfigState(t, nil, wantPath, "")

	out := captureStdout(t, func() {
		if err := runConfigPath(nil, nil); err != nil {
			t.Fatalf("runConfigPath: %v", err)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%s", err, out)
	}
	if decoded["path"] != wantPath {
		t.Fatalf("path = %v, want %s", decoded["path"], wantPath)
	}
}
