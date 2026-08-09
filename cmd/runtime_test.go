package cmd

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/omnideck-dev/cli/engine"
)

func withRuntimeCommandStubs(
	t *testing.T,
	probe func() engine.ProbeResult,
	host engine.HostPlatform,
	ensure func(engine.RuntimeSetupOptions) (engine.ProbeResult, error),
) {
	t.Helper()
	originalProbe := runtimeProbe
	originalHost := runtimeHostPlatform
	originalEnsure := runtimeEnsure
	originalJSON := jsonFlag
	runtimeProbe = probe
	runtimeHostPlatform = func() engine.HostPlatform { return host }
	if ensure != nil {
		runtimeEnsure = ensure
	}
	jsonFlag = true
	t.Cleanup(func() {
		runtimeProbe = originalProbe
		runtimeHostPlatform = originalHost
		runtimeEnsure = originalEnsure
		jsonFlag = originalJSON
	})
}

func TestRuntimeStatusJSONReportsTheSharedMachine(t *testing.T) {
	withRuntimeCommandStubs(t, func() engine.ProbeResult {
		return engine.ProbeResult{
			Name:        "podman",
			State:       engine.RuntimeReady,
			Path:        `C:\Program Files\Podman\podman.exe`,
			Version:     "6.0.2",
			MachineName: "developer-machine",
		}
	}, engine.HostPlatform{OS: "windows", TotalMemoryMB: 32 * 1024}, nil)

	out := captureStdout(t, func() {
		if err := runRuntimeStatus(nil, nil); err != nil {
			t.Fatalf("runRuntimeStatus: %v", err)
		}
	})
	var payload runtimeStatusPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, out)
	}
	if payload.SchemaVersion != runtimeStatusSchema || !payload.Ready || payload.Runtime != "podman" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.MachineName != "developer-machine" {
		t.Fatalf("machine = %q", payload.MachineName)
	}
	if payload.Resources.Container.Memory != "4g" || payload.Resources.Container.SHMSize != "2048m" || payload.Resources.Machine.Mode != "wsl-managed" {
		t.Fatalf("resources = %#v", payload.Resources)
	}
}

func TestRuntimeStatusUsesDesktopSetupVocabulary(t *testing.T) {
	withRuntimeCommandStubs(t, func() engine.ProbeResult {
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeMachineMissing}
	}, engine.HostPlatform{OS: "windows", Arch: "amd64"}, nil)

	out := captureStdout(t, func() { _ = runRuntimeStatus(nil, nil) })
	var payload runtimeStatusPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Phase != "environment" || payload.Activity != "Preparing a secure space to run in…" {
		t.Fatalf("phase copy = %#v", payload)
	}
	if payload.Resources.Machine.Mode != "wsl-managed" {
		t.Fatalf("resources = %#v", payload.Resources)
	}
}

func TestRuntimeEnsureExecutesSharedPlanAndReturnsReadyStatus(t *testing.T) {
	probeCalls := 0
	withRuntimeCommandStubs(t, func() engine.ProbeResult {
		probeCalls++
		if probeCalls == 1 {
			return engine.ProbeResult{Name: "podman", State: engine.RuntimeMachineMissing}
		}
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeReady, MachineName: engine.OmnideckMachineName}
	}, engine.HostPlatform{OS: "windows", Arch: "amd64"}, func(options engine.RuntimeSetupOptions) (engine.ProbeResult, error) {
		if options.AllowTerminalElevation {
			t.Fatal("the Desktop/automation runtime boundary must not launch an invisible sudo prompt")
		}
		progress := 0.5
		options.OnEvent(engine.RuntimeSetupEvent{
			Stage:    engine.SetupStageEnvironment,
			State:    "start",
			Activity: engine.SetupActivityEnvironment,
		})
		options.OnEvent(engine.RuntimeSetupEvent{
			Stage:    engine.SetupStageEnvironment,
			State:    "progress",
			Activity: engine.SetupActivityEnvironment,
			Detail:   "Machine init complete",
			Progress: &progress,
		})
		options.OnEvent(engine.RuntimeSetupEvent{
			Stage:    engine.SetupStageEnvironment,
			State:    "done",
			Activity: engine.SetupActivityEnvironment,
		})
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeReady, MachineName: engine.OmnideckMachineName}, nil
	})

	out := captureStdout(t, func() {
		if err := runRuntimeEnsure(nil, nil); err != nil {
			t.Fatalf("runRuntimeEnsure: %v", err)
		}
	})
	events := decodeNDJSON(t, out)
	if len(events) < 4 {
		t.Fatalf("events = %#v", events)
	}
	if events[0]["stage"] != engine.SetupStageEnvironment || events[0]["activity"] != engine.SetupActivityEnvironment {
		t.Fatalf("first event = %#v", events[0])
	}
	last := events[len(events)-1]
	if last["stage"] != "complete" || last["state"] != "done" {
		t.Fatalf("last event = %#v", last)
	}
	result := last["result"].(map[string]any)
	if result["ready"] != true || result["machineName"] != engine.OmnideckMachineName {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeSetupJSONErrorPreservesTypedFailureAndTechnicalDetail(t *testing.T) {
	structured := runtimeSetupJSONError(&engine.RuntimeSetupError{
		Failure: engine.RuntimeSetupPermissionCancelled,
		Message: "Windows approval wasn’t granted",
		Hint:    "Try again and choose Yes.",
		Err:     errors.New("ERROR_CANCELLED (1223)"),
	})
	if structured.payload.Code != ErrCodePermissionCancelled || structured.payload.Hint == "" || structured.payload.Detail != "ERROR_CANCELLED (1223)" {
		t.Fatalf("structured error = %#v", structured.payload)
	}
}
