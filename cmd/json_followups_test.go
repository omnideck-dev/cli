package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func decodeJSONError(t *testing.T, out string) jsonErrorPayload {
	t.Helper()
	var envelope jsonErrorEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid JSON error: %v\n%s", err, out)
	}
	return envelope.Error
}

// freePort returns a local port nothing is listening on, so workflows that
// check port availability behave the same on every test machine.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func TestRunConfigSetJSONRejectionsAreInvalidSetting(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, setting string
	}{
		{"bad value", "web_ui_port", "99999", "web_ui_port"},
		{"unknown key", "colour", "blue", "colour"},
		{"container name", "container_name", "other", "container_name"},
		{"shared memory above memory", "shm_size", "4g", "shm_size"},
		{"memory below shared memory", "memory", "512m", "memory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withJSONConfigState(t, &config.Config{
				ContainerName: "demo", Memory: "2g", ShmSize: "1g", WebUIPort: "2337", Image: config.DefaultImage,
			}, filepath.Join(t.TempDir(), "demo.yaml"), "podman")

			var runErr error
			out := captureStdout(t, func() { runErr = runConfigSet(nil, []string{tc.key, tc.value}) })
			if runErr != errAborted {
				t.Fatalf("runConfigSet error = %v, want errAborted", runErr)
			}
			payload := decodeJSONError(t, out)
			if payload.Code != ErrCodeInvalidSetting || payload.Setting != tc.setting {
				t.Fatalf("payload = %+v, want INVALID_SETTING for %s", payload, tc.setting)
			}
		})
	}
}

func TestEnvironmentJSONErrorClassifiesValidationAndStorage(t *testing.T) {
	invalid := environmentJSONError(&workflow.ConfigValidationError{Field: "port", Message: "--port must be a number between 1 and 65535"})
	if invalid.payload.Code != ErrCodeInvalidSetting || invalid.payload.Setting != "web_ui_port" {
		t.Fatalf("validation payload = %+v", invalid.payload)
	}
	storage := environmentJSONError(errors.Join(workflow.ErrMissingStorage, errors.New("saved data volume \"x-home\" is missing")))
	if storage.payload.Code != ErrCodeMissingStorage {
		t.Fatalf("storage payload = %+v", storage.payload)
	}
	// add --json reports flag validation the same way.
	if err := setupValidationError(&workflow.ConfigValidationError{Field: "memory", Message: "memory must be a positive number and unit, such as 2g"}); environmentJSONError(err).payload.Setting != "memory" {
		t.Fatalf("setup validation lost its setting: %v", err)
	}
}

func TestRunLifecycleJSONMissingContainerOffersRepairOnlyWithSavedData(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck-desktop", WebUIPort: "2338"}
	start := func() error { return mustStart(cfg.ContainerName) }

	withData := &mockEngine{volumes: map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true}}
	out := captureStdout(t, func() { _ = runLifecycleJSON("desktop", cfg, withData, start) })
	payload := decodeJSONError(t, out)
	if payload.Code != ErrCodeContainerNotFound || payload.Action != "repair_instance" || payload.ActionValue != "desktop" {
		t.Fatalf("payload = %+v, want CONTAINER_NOT_FOUND with repair_instance for desktop", payload)
	}

	// A configuration that cannot be selected by name must not borrow the
	// container name, which --name would reject.
	out = captureStdout(t, func() { _ = runLifecycleJSON("", cfg, withData, start) })
	payload = decodeJSONError(t, out)
	if payload.Action != "repair_instance" || payload.ActionValue != "" || payload.Hint != "Run: omnideck repair (saved data is kept)" {
		t.Fatalf("nameless payload = %+v", payload)
	}

	withoutData := &mockEngine{volumes: map[string]bool{cfg.HomeVolumeName(): true}}
	out = captureStdout(t, func() { _ = runLifecycleJSON("desktop", cfg, withoutData, start) })
	payload = decodeJSONError(t, out)
	if payload.Code != ErrCodeContainerNotFound || payload.Action != "" {
		t.Fatalf("payload = %+v, want CONTAINER_NOT_FOUND without an action", payload)
	}
}

// mustStart runs the shared start workflow against an engine with no
// containers, which is what start --json does for a missing container.
func mustStart(name string) error {
	_, err := workflow.EnsureStarted(&mockEngine{}, name)
	return err
}

func TestResolveRemoveOptionsBackupDir(t *testing.T) {
	orig := []any{removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag, removeBackupFlag, removeNoBackupFlag, removeBackupDirFlag}
	t.Cleanup(func() {
		removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag = orig[0].(bool), orig[1].(bool), orig[2].(bool)
		removeBackupFlag, removeNoBackupFlag, removeBackupDirFlag = orig[3].(bool), orig[4].(bool), orig[5].(string)
	})
	removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag = true, false, true
	dir := t.TempDir()

	removeBackupFlag, removeNoBackupFlag, removeBackupDirFlag = false, true, dir
	if _, err := resolveRemoveOptions(); jsonErrorCode(err) != ErrCodeMissingRequiredFlag {
		t.Fatalf("--backup-dir without --backup error = %v", err)
	}

	removeBackupFlag, removeNoBackupFlag = true, false
	removeBackupDirFlag = filepath.Join(dir, "missing")
	if _, err := resolveRemoveOptions(); jsonErrorCode(err) != ErrCodeInvalidSetting {
		t.Fatalf("missing --backup-dir error = %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	removeBackupDirFlag = file
	if _, err := resolveRemoveOptions(); jsonErrorCode(err) != ErrCodeInvalidSetting {
		t.Fatalf("file as --backup-dir error = %v", err)
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		readOnly := filepath.Join(dir, "read-only")
		if err := os.Mkdir(readOnly, 0o500); err != nil {
			t.Fatal(err)
		}
		removeBackupDirFlag = readOnly
		if _, err := resolveRemoveOptions(); jsonErrorCode(err) != ErrCodeInvalidSetting {
			t.Fatalf("read-only --backup-dir error = %v", err)
		}
	}

	removeBackupDirFlag = dir
	opts, err := resolveRemoveOptions()
	if err != nil || opts.BackupDir != dir || !opts.BackupData {
		t.Fatalf("opts = %+v, err = %v", opts, err)
	}
}

func jsonErrorCode(err error) string {
	var structured *jsonCmdError
	if errors.As(err, &structured) {
		return structured.payload.Code
	}
	return ""
}

func withRepairState(t *testing.T, cfg *config.Config) {
	t.Helper()
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	withJSONConfigState(t, cfg, config.InstancePath("desktop"), "podman")
	origWait := repairWaitForApplication
	t.Cleanup(func() { repairWaitForApplication = origWait })
	repairWaitForApplication = func(context.Context, workflow.StartupEngine, *config.Config) error { return nil }
}

func TestRepairJSONRecreatesMissingContainerFromTheLocalImage(t *testing.T) {
	cfg := &config.Config{
		ContainerName: "omnideck-desktop", Memory: "2g", ShmSize: "1g",
		WebUIPort: freePort(t), Image: config.DefaultImage, LayoutVersion: 1,
	}
	withRepairState(t, cfg)
	eng := &mockEngine{
		volumes:     map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true},
		localImages: map[string]bool{config.DefaultImage: true},
		pullErr:     errors.New("repair must not download an image that is stored locally"),
	}

	var runErr error
	out := captureStdout(t, func() { runErr = repairInstance(context.Background(), eng, cfg) })
	if runErr != nil {
		t.Fatalf("repairInstance: %v\n%s", runErr, out)
	}
	events := decodeNDJSON(t, out)
	for _, event := range events {
		if event["stage"] == "pull_image" {
			t.Fatalf("repair downloaded the image: %v", events)
		}
	}
	for _, event := range events {
		if event["stage"] == "replace_container" {
			t.Fatalf("a missing container was replaced instead of created: %v", events)
		}
	}
	result := events[len(events)-1]["result"].(map[string]any)
	if result["action"] != "repaired" || result["status"].(map[string]any)["name"] != "desktop" {
		t.Fatalf("result = %v", result)
	}
	if len(eng.runCalls) != 1 || eng.runCalls[0].Image != config.DefaultImage {
		t.Fatalf("runCalls = %+v", eng.runCalls)
	}
	saved, err := config.Load(config.InstancePath("desktop"))
	if err != nil || saved.LayoutVersion != config.CurrentContainerLayout || saved.ContainerName != "omnideck-desktop" {
		t.Fatalf("saved = %+v, err = %v", saved, err)
	}
}

func TestRepairJSONStopsWhenSavedDataIsMissing(t *testing.T) {
	cfg := &config.Config{
		ContainerName: "omnideck-desktop", Memory: "2g", ShmSize: "1g",
		WebUIPort: freePort(t), Image: config.DefaultImage, LayoutVersion: config.CurrentContainerLayout,
	}
	withRepairState(t, cfg)
	eng := &mockEngine{volumes: map[string]bool{cfg.HomeVolumeName(): true}, localImages: map[string]bool{config.DefaultImage: true}}

	out := captureStdout(t, func() { _ = repairInstance(context.Background(), eng, cfg) })
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	if last["state"] != "error" || last["error"].(map[string]any)["code"] != ErrCodeMissingStorage {
		t.Fatalf("terminal event = %v", last)
	}
	if len(eng.runCalls) != 0 || len(eng.volumes) != 1 {
		t.Fatalf("repair changed the runtime: runs=%d volumes=%v", len(eng.runCalls), eng.volumes)
	}
}

// A repair that fails before EnsureInstance starts still reports its error on
// a stage that was started, so start/done pairing holds for every event.
func TestRepairJSONErrorFollowsAStart(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck-desktop", Memory: "2g", ShmSize: "1g", WebUIPort: freePort(t), Image: config.DefaultImage}
	withRepairState(t, cfg)
	eng := &mockEngine{containerExistsErr: errors.New("podman: connection refused")}

	out := captureStdout(t, func() { _ = repairInstance(context.Background(), eng, cfg) })
	events := decodeNDJSON(t, out)
	if len(events) != 2 || events[0]["state"] != "start" || events[1]["state"] != "error" || events[0]["stage"] != events[1]["stage"] {
		t.Fatalf("events = %v, want one start then its error", events)
	}
}

// requirePairedStages checks the NDJSON rule that every stage emits start
// before its done or error, and finishes before the next stage starts. The
// terminal complete event has no start.
func requirePairedStages(t *testing.T, events []map[string]any) {
	t.Helper()
	open := ""
	for _, event := range events {
		stage, _ := event["stage"].(string)
		if stage == "complete" {
			if open != "" {
				t.Fatalf("complete while %q was still running: %v", open, events)
			}
			continue
		}
		switch event["state"] {
		case "start":
			if open != "" {
				t.Fatalf("%q started while %q was still running: %v", stage, open, events)
			}
			open = stage
		case "done", "error":
			if stage != open {
				t.Fatalf("%s for %q, but the running stage is %q: %v", event["state"], stage, open, events)
			}
			open = ""
		}
	}
}

func TestRepairJSONReportsTheStartupWaitAsItsOwnStage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		waitErr   error
		wantFinal string
	}{
		{"ready", nil, "done"},
		{"fails", &workflow.StartupFailure{Code: ErrCodeAppStartupFailed}, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				ContainerName: "omnideck-desktop", Memory: "2g", ShmSize: "1g",
				WebUIPort: freePort(t), Image: config.DefaultImage, LayoutVersion: config.CurrentContainerLayout,
			}
			withRepairState(t, cfg)
			repairWaitForApplication = func(context.Context, workflow.StartupEngine, *config.Config) error { return tc.waitErr }
			eng := &mockEngine{
				volumes:     map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true},
				localImages: map[string]bool{config.DefaultImage: true},
			}

			out := captureStdout(t, func() { _ = repairInstance(context.Background(), eng, cfg) })
			events := decodeNDJSON(t, out)
			requirePairedStages(t, events)
			var startup []string
			for _, event := range events {
				if event["stage"] == "startup" {
					startup = append(startup, event["state"].(string))
				}
			}
			if want := []string{"start", tc.wantFinal}; !slices.Equal(startup, want) {
				t.Fatalf("startup events = %v, want %v\n%s", startup, want, out)
			}
		})
	}
}

func TestStartTextErrorNamesTheRepairCommand(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	origPath := ConfigPath
	ConfigPath = config.InstancePath("desktop")
	t.Cleanup(func() { ConfigPath = origPath })
	err := containerNotFoundTextError(mustStart("omnideck-desktop"))
	if err == nil || !errors.Is(err, workflow.ErrContainerNotFound) || !strings.Contains(err.Error(), "omnideck repair --name desktop") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigSetIgnoresMemoryRuleForUnrelatedKeys(t *testing.T) {
	// Saved by an older CLI with shared memory above memory: unrelated edits
	// must still work.
	withJSONConfigState(t, &config.Config{
		ContainerName: "demo", Memory: "1g", ShmSize: "2g", WebUIPort: "2337", Image: config.DefaultImage,
	}, filepath.Join(t.TempDir(), "demo.yaml"), "podman")
	out := captureStdout(t, func() {
		if err := runConfigSet(nil, []string{"image", "example:v2"}); err != nil {
			t.Fatalf("runConfigSet: %v", err)
		}
	})
	if strings.Contains(out, "INVALID_SETTING") {
		t.Fatalf("unrelated key rejected: %s", out)
	}
}
