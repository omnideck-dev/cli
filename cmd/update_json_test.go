package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestRunUpdateJSONSuccessEmitsStagesAndCompletes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("demo")
	defer func() { ConfigPath = origPath }()
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())

	cfg := &config.Config{ContainerName: "demo", Image: "ghcr.io/omnideck-dev/omnideck:legacy", WebUIPort: "2337"}
	eng := &mockEngine{
		name:            "docker",
		containerExists: map[string]bool{"demo": true},
		containerStatus: map[string]string{"demo": "running"},
		pullMsgs:        []string{"Pulling fs layer"},
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runUpdateJSON(eng, cfg)
	})
	if runErr != nil {
		t.Fatalf("runUpdateJSON: %v\noutput=%s", runErr, out)
	}

	events := decodeNDJSON(t, out)
	wantStages := []string{"pull_image", "pull_image", "pull_image", "recreate", "recreate", "complete"}
	if len(events) != len(wantStages) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantStages), events)
	}
	for i, want := range wantStages {
		if events[i]["stage"] != want {
			t.Fatalf("event[%d].stage = %v, want %s", i, events[i]["stage"], want)
		}
	}
	last := events[len(events)-1]
	result, ok := last["result"].(map[string]any)
	if !ok {
		t.Fatalf("final event missing result: %+v", last)
	}
	if result["status"] != "running" {
		t.Fatalf("result.status = %v, want running", result["status"])
	}

	if len(eng.runCalls) != 1 {
		t.Fatalf("expected RunContainer called once, got %d", len(eng.runCalls))
	}
}

func TestRunUpdateJSONFailureEmitsNestedError(t *testing.T) {
	origPath := ConfigPath
	ConfigPath = "/tmp/unused-update.yaml"
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337"}
	eng := &mockEngine{pullErr: errors.New("network unreachable")}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runUpdateJSON(eng, cfg)
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["stage"] != "pull_image" || last["state"] != "error" {
		t.Fatalf("unexpected terminal event: %+v", last)
	}
	errPayload := last["error"].(map[string]any)
	if errPayload["code"] != ErrCodeInternal || errPayload["message"] != "network unreachable" {
		t.Fatalf("unexpected error payload: %+v", errPayload)
	}
}

// TestRunUpdateJSONCancellationReportsCancelled is the regression guard for
// JSON_MODE_SPEC.md's update cancellation handling: a step failing with a
// context.Canceled-wrapped error must be reported as CANCELLED, not
// INTERNAL_ERROR, so a GUI can tell "the user/app cancelled this" apart
// from "this actually failed."
func TestRunUpdateJSONCancellationReportsCancelled(t *testing.T) {
	origPath := ConfigPath
	ConfigPath = "/tmp/unused-update.yaml"
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "demo", Image: "img", WebUIPort: "2337"}
	eng := &mockEngine{pullErr: fmt.Errorf("docker pull: %w", context.Canceled)}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runUpdateJSON(eng, cfg)
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	errPayload := last["error"].(map[string]any)
	if errPayload["code"] != ErrCodeCancelled {
		t.Fatalf("code = %v, want %s", errPayload["code"], ErrCodeCancelled)
	}
}

func TestUpdateTargetMigratesImageWithoutMutatingInput(t *testing.T) {
	const legacyImage = "ghcr.io/omnideck-dev/omnideck:main"
	cfg := &config.Config{ContainerName: "demo", Image: legacyImage}
	current, next := updateTarget(cfg)
	if current.Image != legacyImage {
		t.Fatalf("current.Image = %q, want unchanged %q", current.Image, legacyImage)
	}
	if next.Image != config.DefaultImage {
		t.Fatalf("next.Image = %q, want migrated to %q", next.Image, config.DefaultImage)
	}
	if cfg.Image != legacyImage {
		t.Fatalf("updateTarget must not mutate the caller's cfg, got %q", cfg.Image)
	}
}
