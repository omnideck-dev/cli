package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func TestRunUpdateJSONSuccessEmitsStagesAndCompletes(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("demo")
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "demo", Image: "ghcr.io/omnideck-dev/omnideck:legacy", WebUIPort: "2337"}
	eng := &mockEngine{
		name:            "podman",
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
	wantStages := []string{"pull_image", "pull_image", "pull_image", "recreate", "recreate", "external_url", "external_url", "complete"}
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
	if errPayload["code"] != ErrCodeDownloadFailed || errPayload["message"] != "network unreachable" {
		t.Fatalf("unexpected error payload: %+v", errPayload)
	}
}

// TestRunUpdateJSONClassifiesPortConflict guards that update reports the same
// error codes as add and environment ensure, so a caller can offer the same
// recovery instead of treating a busy port as an internal failure.
func TestRunUpdateJSONClassifiesPortConflict(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("demo")
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "demo", Image: config.DefaultImage, WebUIPort: "2337"}
	eng := &mockEngine{
		containerExists: map[string]bool{"demo": true},
		containerStatus: map[string]string{"demo": "running"},
		runErr:          errors.New("rootlessport listen tcp 0.0.0.0:2337: bind: address already in use"),
	}

	out := captureStdout(t, func() { _ = runUpdateJSON(eng, cfg) })
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["stage"] != "recreate" || last["state"] != "error" {
		t.Fatalf("unexpected terminal event: %+v", last)
	}
	if code := last["error"].(map[string]any)["code"]; code != ErrCodePortInUse {
		t.Fatalf("code = %v, want %s", code, ErrCodePortInUse)
	}
}

// TestRunUpdateJSONCompleteNamesTheInstance guards that the complete result's
// name is the instance name --name accepts, even when its container differs.
func TestRunUpdateJSONCompleteNamesTheInstance(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("desktop")
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "omnideck-desktop", Image: config.DefaultImage, WebUIPort: "2338"}
	eng := &mockEngine{
		containerExists: map[string]bool{"omnideck-desktop": true},
		containerStatus: map[string]string{"omnideck-desktop": "running"},
	}

	out := captureStdout(t, func() {
		if err := runUpdateJSON(eng, cfg); err != nil {
			t.Fatalf("runUpdateJSON: %v", err)
		}
	})
	events := decodeNDJSON(t, out)
	result := events[len(events)-1]["result"].(map[string]any)
	if result["name"] != "desktop" || result["container"] != "omnideck-desktop" {
		t.Fatalf("name/container = %v/%v, want desktop/omnideck-desktop", result["name"], result["container"])
	}
}

// TestRunUpdateJSONCancellationReportsCancelled is the regression guard for
// JSON_MODE_SPEC.md §5 update --json cancellation handling: a step failing
// with a context.Canceled-wrapped error must be reported as CANCELLED, not
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
	current, next := workflow.UpdateTarget(cfg)
	if current.Image != legacyImage {
		t.Fatalf("current.Image = %q, want unchanged %q", current.Image, legacyImage)
	}
	if next.Image != config.DefaultImage {
		t.Fatalf("next.Image = %q, want migrated to %q", next.Image, config.DefaultImage)
	}
	if cfg.Image != legacyImage {
		t.Fatalf("UpdateTarget must not mutate the caller's cfg, got %q", cfg.Image)
	}
}

func TestRunUpdateJSONReportsExternalURLChange(t *testing.T) {
	for _, tc := range []struct {
		name         string
		env          []string
		wantState    string
		wantPrevious string
	}{
		{"set", []string{"PORT=8080"}, "set", ""},
		{"corrected", []string{"OMNIDECK_EXTERNAL_URL=http://127.0.0.1:2339"}, "corrected", "http://127.0.0.1:2339"},
		{"confirmed", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:2339"}, "confirmed", ""},
		// A saved `config set web_ui_port` the update now applies: the old
		// address was right for its container, so nothing was corrected.
		{"port change", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:2337"}, "confirmed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
			origPath := ConfigPath
			ConfigPath = config.InstancePath("work")
			defer func() { ConfigPath = origPath }()

			cfg := &config.Config{ContainerName: "work", Image: config.DefaultImage, WebUIPort: "2339"}
			eng := &mockEngine{
				containerExists: map[string]bool{"work": true},
				containerStatus: map[string]string{"work": "running"},
				env:             map[string][]string{"work": tc.env},
			}

			var runErr error
			out := captureStdout(t, func() {
				runErr = runUpdateJSON(eng, cfg)
			})
			if runErr != nil {
				t.Fatalf("runUpdateJSON: %v\noutput=%s", runErr, out)
			}
			var stage map[string]any
			for _, event := range decodeNDJSON(t, out) {
				if event["stage"] == "external_url" && event["state"] == "done" {
					stage = event
				}
			}
			result, ok := stage["result"].(map[string]any)
			if !ok {
				t.Fatalf("external_url done event missing result: %s", out)
			}
			if result["state"] != tc.wantState || result["expected"] != "http://localhost:2339" {
				t.Fatalf("result = %+v, want state %s", result, tc.wantState)
			}
			if previous, _ := result["previous"].(string); previous != tc.wantPrevious {
				t.Fatalf("previous = %q, want %q", previous, tc.wantPrevious)
			}

			saved, err := config.Load(ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if saved.LayoutVersion != config.CurrentContainerLayout {
				t.Fatalf("saved layout_version = %d, want %d", saved.LayoutVersion, config.CurrentContainerLayout)
			}
		})
	}
}

func TestRunUpdateJSONFailsWhenReplacementLacksExternalURL(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("work")
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "work", Image: config.DefaultImage, WebUIPort: "2339"}
	eng := &mockEngine{
		containerExists:       map[string]bool{"work": true},
		containerStatus:       map[string]string{"work": "running"},
		runIgnoresExternalURL: true,
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runUpdateJSON(eng, cfg)
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["stage"] != "external_url" || last["state"] != "error" {
		t.Fatalf("unexpected terminal event: %+v", last)
	}
	errPayload := last["error"].(map[string]any)
	if message, _ := errPayload["message"].(string); !strings.Contains(message, "http://localhost:2339") {
		t.Fatalf("error should name the expected address: %+v", errPayload)
	}
	if hint, _ := errPayload["hint"].(string); !strings.Contains(hint, "applied and saved") {
		t.Fatalf("error should say the update was applied: %+v", errPayload)
	}
}

func TestRunUpdatePlainReportsExternalURL(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("work")
	defer func() { ConfigPath = origPath }()

	cfg := &config.Config{ContainerName: "work", Image: config.DefaultImage, WebUIPort: "2339"}
	eng := &mockEngine{
		containerExists: map[string]bool{"work": true},
		containerStatus: map[string]string{"work": "running"},
		env:             map[string][]string{"work": {"OMNIDECK_EXTERNAL_URL=http://127.0.0.1:2339"}},
	}
	var runErr error
	out := captureStdout(t, func() {
		runErr = runUpdatePlain(eng, cfg)
	})
	if runErr != nil {
		t.Fatalf("runUpdatePlain: %v\noutput=%s", runErr, out)
	}
	if !strings.Contains(out, "MCP sign-in address... corrected http://127.0.0.1:2339 → http://localhost:2339") {
		t.Fatalf("plain output missing sign-in address outcome:\n%s", out)
	}
}
