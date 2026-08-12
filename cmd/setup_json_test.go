package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

func decodeNDJSON(t *testing.T, out string) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("line not valid JSON: %v\nline=%s\nfull output=%s", err, line, out)
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestRunSetupStepsJSONSuccessEmitsAllStagesAndCompletes(t *testing.T) {
	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337", Memory: "2g", ShmSize: "512m"}
	eng := &mockEngine{
		name:            "podman",
		containerStatus: map[string]string{"demo": "running"},
		pullMsgs:        []string{"Pulling fs layer", "Download complete"},
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	savePath := config.InstancePath("demo")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetupStepsJSON(eng, cfg, savePath)
	})
	if runErr != nil {
		t.Fatalf("runSetupStepsJSON: %v\noutput=%s", runErr, out)
	}

	events := decodeNDJSON(t, out)
	wantStages := []string{
		"check_availability", "check_availability",
		"create_home_volume", "create_home_volume",
		"create_state_volume", "create_state_volume",
		"pull_image", "pull_image", "pull_image", "pull_image", // start + 2 progress + done
		"run_container", "run_container",
		"save_config", "save_config",
		"complete",
	}
	if len(events) != len(wantStages) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantStages), events)
	}
	for i, want := range wantStages {
		if events[i]["stage"] != want {
			t.Fatalf("event[%d].stage = %v, want %s (full: %+v)", i, events[i]["stage"], want, events[i])
		}
	}
	// Two progress events between pull_image's start and done, carrying detail.
	if events[7]["state"] != "progress" || events[7]["detail"] != "Pulling fs layer" {
		t.Fatalf("unexpected progress event: %+v", events[7])
	}
	last := events[len(events)-1]
	if last["state"] != "done" {
		t.Fatalf("final event state = %v, want done", last["state"])
	}
	result, ok := last["result"].(map[string]any)
	if !ok {
		t.Fatalf("final event missing result: %+v", last)
	}
	if result["status"] != "running" {
		t.Fatalf("result.status = %v, want running", result["status"])
	}

	if _, err := config.Load(savePath); err != nil {
		t.Fatalf("expected config saved at %s: %v", savePath, err)
	}
}

func TestRunSetupStepsJSONFailureEmitsNestedError(t *testing.T) {
	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337", Memory: "2g", ShmSize: "512m"}
	eng := &mockEngine{createVolumeErr: errors.New("disk full")}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetupStepsJSON(eng, cfg, "/tmp/unused.yaml")
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["stage"] != "create_home_volume" || last["state"] != "error" {
		t.Fatalf("unexpected terminal event: %+v", last)
	}
	errPayload, ok := last["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error payload: %+v", last)
	}
	if errPayload["code"] != ErrCodeInternal {
		t.Fatalf("code = %v, want %s", errPayload["code"], ErrCodeInternal)
	}
	if errPayload["message"] != "disk full" {
		t.Fatalf("message = %v, want disk full", errPayload["message"])
	}
}

func TestRunSetupStepsJSONClassifiesDownloadFailure(t *testing.T) {
	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337", Memory: "2g", ShmSize: "512m"}
	eng := &mockEngine{pullErr: errors.New("registry unavailable")}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetupStepsJSON(eng, cfg, "/tmp/unused.yaml")
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	errorPayload, ok := last["error"].(map[string]any)
	if !ok || errorPayload["code"] != ErrCodeDownloadFailed {
		t.Fatalf("terminal event = %+v, want %s", last, ErrCodeDownloadFailed)
	}
}

// TestRunSetupStepsJSONCancellationCleansUpAndReportsCancelled is the
// regression guard for JSON_MODE_SPEC.md's "Cancellation and teardown": a
// run cancelled after creating real resources (both volumes here) but
// before the container/config exist must best-effort remove what it already
// created, and report CANCELLED rather than INTERNAL_ERROR.
//
// The shared process context is actually cancelled here, then run_container
// is made to fail the way a real killed docker/podman subprocess does — a
// plain non-context error, not one wrapping context.Canceled (see
// engine.CancelRequested's doc: exec.CommandContext's default Cancel kills
// the process, which always returns a plain *exec.ExitError). This is what
// regressed originally: detection must come from the shared context having
// been cancelled, not from the subprocess's own returned error.
func TestRunSetupStepsJSONCancellationCleansUpAndReportsCancelled(t *testing.T) {
	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337", Memory: "2g", ShmSize: "512m"}
	eng := &mockEngine{
		runErr: errors.New("signal: killed"),
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.SetCancelContext(cancelledCtx)
	defer engine.SetCancelContext(context.Background())

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetupStepsJSON(eng, cfg, "/tmp/unused.yaml")
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}

	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["stage"] != "run_container" || last["state"] != "error" {
		t.Fatalf("unexpected terminal event: %+v", last)
	}
	errPayload, ok := last["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error payload: %+v", last)
	}
	if errPayload["code"] != ErrCodeCancelled {
		t.Fatalf("code = %v, want %s", errPayload["code"], ErrCodeCancelled)
	}

	// Both volumes were created by the two prior successful steps, so
	// cleanup must remove both. The container was never created (run_container
	// itself is what failed), so RemoveContainer must not be called.
	wantVolumes := map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true}
	if len(eng.removeVolumeCalls) != 2 {
		t.Fatalf("removeVolumeCalls = %v, want cleanup of both volumes", eng.removeVolumeCalls)
	}
	for _, name := range eng.removeVolumeCalls {
		if !wantVolumes[name] {
			t.Fatalf("unexpected volume removed: %s", name)
		}
	}
	if eng.removeContainerCalls != 0 {
		t.Fatalf("removeContainerCalls = %d, want 0 (container was never created)", eng.removeContainerCalls)
	}
}
