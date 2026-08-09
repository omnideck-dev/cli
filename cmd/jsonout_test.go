package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return string(buf)
}

func TestGatherStatusPayload(t *testing.T) {
	cfg := &config.Config{
		ContainerName: "main",
		Image:         "ghcr.io/omnideck-dev/omnideck:main",
		WebUIPort:     "2337",
	}
	eng := &mockEngine{
		name:            "podman",
		containerStatus: map[string]string{"main": "running"},
		volumes: map[string]bool{
			"main-home":  true,
			"main-state": false,
		},
	}

	payload := gatherStatusPayload(cfg, eng)

	if payload.Name != "main" || payload.Container != "main" {
		t.Fatalf("name/container = %q/%q, want main/main", payload.Name, payload.Container)
	}
	if payload.Status != "running" {
		t.Fatalf("status = %q, want running", payload.Status)
	}
	if payload.Engine != "podman" {
		t.Fatalf("engine = %q, want podman", payload.Engine)
	}
	if !payload.HomeVolume.Exists || payload.HomeVolume.Name != "main-home" {
		t.Fatalf("homeVolume = %+v", payload.HomeVolume)
	}
	if payload.StateVolume.Exists {
		t.Fatalf("stateVolume.Exists = true, want false")
	}
}

func TestGatherStatusPayloadStopped(t *testing.T) {
	cfg := &config.Config{ContainerName: "main", Image: "img", WebUIPort: "2337"}
	eng := &mockEngine{
		name:            "podman",
		containerStatus: map[string]string{"main": "exited"},
		volumes:         map[string]bool{"main-home": true, "main-state": true},
	}
	payload := gatherStatusPayload(cfg, eng)
	if payload.Status != "exited" {
		t.Fatalf("status = %q, want exited", payload.Status)
	}
}

// TestClosedErrorCodeSetIsStable pins the exact set of documented error
// codes from JSON_MODE_SPEC.md's "Structured error shape" table — adding,
// removing, or renaming one here is a breaking JSON contract change and
// should come with a jsonContract bump.
func TestClosedErrorCodeSetIsStable(t *testing.T) {
	want := map[string]bool{
		"AMBIGUOUS_INSTANCE":      true,
		"NOT_INSTALLED":           true,
		"ENGINE_NOT_FOUND":        true,
		"CONTAINER_NOT_FOUND":     true,
		"MISSING_REQUIRED_FLAG":   true,
		"MISSING_SUBCOMMAND":      true,
		"CANCELLED":               true,
		"RESTART_REQUIRED":        true,
		"PERMISSION_DENIED":       true,
		"PERMISSION_CANCELLED":    true,
		"WINDOWS_FEATURES_FAILED": true,
		"PACKAGE_INDEX_FAILED":    true,
		"INSTALLER_FAILED":        true,
		"DOWNLOAD_FAILED":         true,
		"UNSUPPORTED":             true,
		"RUNTIME_SETUP_FAILED":    true,
		"PORT_IN_USE":             true,
		"CONTAINER_CONFLICT":      true,
		"INTERNAL_ERROR":          true,
	}
	got := map[string]bool{
		ErrCodeAmbiguousInstance:     true,
		ErrCodeNotInstalled:          true,
		ErrCodeEngineNotFound:        true,
		ErrCodeContainerNotFound:     true,
		ErrCodeMissingRequiredFlag:   true,
		ErrCodeMissingSubcommand:     true,
		ErrCodeCancelled:             true,
		ErrCodeRestartRequired:       true,
		ErrCodePermissionDenied:      true,
		ErrCodePermissionCancelled:   true,
		ErrCodeWindowsFeaturesFailed: true,
		ErrCodePackageIndexFailed:    true,
		ErrCodeInstallerFailed:       true,
		ErrCodeDownloadFailed:        true,
		ErrCodeUnsupported:           true,
		ErrCodeRuntimeSetupFailed:    true,
		ErrCodePortInUse:             true,
		ErrCodeContainerConflict:     true,
		ErrCodeInternal:              true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d distinct codes, want %d — a constant collides with another", len(got), len(want))
	}
	for code := range want {
		if !got[code] {
			t.Fatalf("missing documented error code %q", code)
		}
	}
	for code := range got {
		if !want[code] {
			t.Fatalf("undocumented error code %q — add it to JSON_MODE_SPEC.md's code table too", code)
		}
	}
}

// TestEveryJSONErrorResolvesToAClosedCode is the regression guard for
// JSON_MODE_SPEC.md's "every error path must resolve to one of these
// codes, never an unstructured failure": both a hand-built jsonCmdError and
// an arbitrary plain error must marshal with one of the closed codes.
func TestEveryJSONErrorResolvesToAClosedCode(t *testing.T) {
	closed := map[string]bool{
		ErrCodeAmbiguousInstance: true, ErrCodeNotInstalled: true, ErrCodeEngineNotFound: true,
		ErrCodeContainerNotFound: true, ErrCodeMissingRequiredFlag: true, ErrCodeMissingSubcommand: true,
		ErrCodeCancelled: true, ErrCodeRestartRequired: true, ErrCodePermissionDenied: true,
		ErrCodePermissionCancelled: true, ErrCodeWindowsFeaturesFailed: true,
		ErrCodePackageIndexFailed: true, ErrCodeInstallerFailed: true,
		ErrCodeDownloadFailed: true, ErrCodeUnsupported: true, ErrCodeRuntimeSetupFailed: true,
		ErrCodePortInUse: true, ErrCodeContainerConflict: true,
		ErrCodeInternal: true,
	}
	inputs := []error{
		errors.New("some arbitrary internal failure"),
		newJSONError(ErrCodeContainerNotFound, "gone"),
	}
	for _, err := range inputs {
		out := captureStdout(t, func() { _ = writeJSONError(err) })
		var envelope jsonErrorEnvelope
		if jsonErr := json.Unmarshal([]byte(out), &envelope); jsonErr != nil {
			t.Fatalf("output not valid JSON: %v\n%s", jsonErr, out)
		}
		if !closed[envelope.Error.Code] {
			t.Fatalf("error code %q for input %v is not in the closed set", envelope.Error.Code, err)
		}
	}
}

func TestGatherStatusPayloadUnknownWhenContainerMissing(t *testing.T) {
	cfg := &config.Config{ContainerName: "ghost"}
	eng := &mockEngine{containerStatusErr: map[string]error{"ghost": errors.New("no such container")}}

	payload := gatherStatusPayload(cfg, eng)
	if payload.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", payload.Status)
	}
}

func TestGatherListEntryRunningHasLiveFields(t *testing.T) {
	instance := config.InstanceInfo{
		Name: "demo",
		Config: &config.Config{
			ContainerName: "demo",
			Image:         "ghcr.io/omnideck-dev/omnideck:latest",
			WebUIPort:     "2338",
		},
	}
	started := time.Now().Add(-90 * time.Minute)
	eng := &mockEngine{
		containerStatus: map[string]string{"demo": "running"},
		stats: map[string]mockStats{
			"demo": {cpu: "0.11%", cpuPct: 0.0011, ram: "195.4MB", ramTotal: "1.074GB", ramPct: 0.18},
		},
		inspect: map[string]engine.InspectData{
			"demo": {StartedAt: started, CreatedAt: started, RestartCount: 2, HealthStatus: "healthy"},
		},
	}

	entry := gatherListEntry(eng, instance)

	if entry.Status != "running" {
		t.Fatalf("status = %q, want running", entry.Status)
	}
	if entry.CPU == nil || *entry.CPU != "0.11%" {
		t.Fatalf("cpu = %v, want 0.11%%", entry.CPU)
	}
	if entry.Restarts == nil || *entry.Restarts != 2 {
		t.Fatalf("restarts = %v, want 2", entry.Restarts)
	}
	if entry.Uptime == nil || *entry.Uptime == "" {
		t.Fatal("uptime should be populated for a running container")
	}
	if entry.Health != "healthy" {
		t.Fatalf("health = %q, want healthy", entry.Health)
	}
}

// TestGatherListEntryPausedHasLiveFields is the regression guard for a
// paused instance silently losing its stats in list --json: paused is not
// stopped, and Docker/Podman still return real, non-error stats for it.
func TestGatherListEntryPausedHasLiveFields(t *testing.T) {
	instance := config.InstanceInfo{
		Name: "demo",
		Config: &config.Config{
			ContainerName: "demo",
			Image:         "ghcr.io/omnideck-dev/omnideck:latest",
			WebUIPort:     "2338",
		},
	}
	eng := &mockEngine{
		containerStatus: map[string]string{"demo": "paused"},
		stats: map[string]mockStats{
			"demo": {cpu: "0.00%", ram: "195.4MB", ramTotal: "1.074GB", ramPct: 0.18},
		},
		inspect: map[string]engine.InspectData{
			"demo": {RestartCount: 1},
		},
	}

	entry := gatherListEntry(eng, instance)

	if entry.Status != "paused" {
		t.Fatalf("status = %q, want paused", entry.Status)
	}
	if entry.CPU == nil || entry.RAM == nil || *entry.RAM != "195.4MB" {
		t.Fatalf("a paused instance should still report live stats, got entry=%+v", entry)
	}
}

// TestGatherListEntryStoppedHasNullLiveFields is the regression guard for
// JSON_MODE_SPEC.md's "every object has the same set of keys, always" rule:
// live-stat fields must marshal to explicit null, never be omitted and
// never fall back to a zero value that looks like a real reading.
func TestGatherListEntryStoppedHasNullLiveFields(t *testing.T) {
	instance := config.InstanceInfo{
		Name:   "main",
		Config: &config.Config{ContainerName: "main", Image: "ghcr.io/omnideck-dev/omnideck:main", WebUIPort: "2337"},
	}
	eng := &mockEngine{
		containerStatus: map[string]string{"main": "exited"},
		inspect: map[string]engine.InspectData{
			"main": {CreatedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},
		},
	}

	entry := gatherListEntry(eng, instance)

	if entry.Status != "exited" {
		t.Fatalf("status = %q, want exited", entry.Status)
	}
	if entry.CPU != nil || entry.CPUPct != nil || entry.RAM != nil || entry.RAMTotal != nil || entry.RAMPct != nil {
		t.Fatalf("live stat fields must be nil when stopped, got entry=%+v", entry)
	}
	if entry.Uptime != nil || entry.Restarts != nil {
		t.Fatalf("uptime/restarts must be nil when stopped, got entry=%+v", entry)
	}
	if entry.Created != "2026-07-20" {
		t.Fatalf("created = %q, want 2026-07-20 (populated even when stopped)", entry.Created)
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cpu", "cpuPct", "ram", "ramTotal", "ramPct", "uptime", "restarts"} {
		v, ok := decoded[key]
		if !ok {
			t.Fatalf("key %q must be present (as null), not omitted; decoded=%v", key, decoded)
		}
		if v != nil {
			t.Fatalf("key %q = %v, want JSON null", key, v)
		}
	}
}

func TestGatherListEntryNoEngineIsUnknownNotError(t *testing.T) {
	instance := config.InstanceInfo{Name: "main", Config: &config.Config{ContainerName: "main"}}
	entry := gatherListEntry(nil, instance)
	if entry.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", entry.Status)
	}
}

func TestGatherListEntriesRunsConcurrentlyOverEveryInstance(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "one", Config: &config.Config{ContainerName: "one"}},
		{Name: "two", Config: &config.Config{ContainerName: "two"}},
	}
	eng := &mockEngine{
		containerStatus: map[string]string{"one": "running", "two": "exited"},
	}
	entries := gatherListEntries(eng, instances)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	byName := map[string]listEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if byName["one"].Status != "running" || byName["two"].Status != "exited" {
		t.Fatalf("unexpected statuses: %+v", byName)
	}
}

func TestGatherListEntriesEmptyIsEmptyArrayNotNull(t *testing.T) {
	entries := gatherListEntries(nil, nil)
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("marshal(empty entries) = %s, want []", raw)
	}
}

func TestWriteJSONErrorWrapsPlainErrorsAsInternal(t *testing.T) {
	out := captureStdout(t, func() {
		_ = writeJSONError(errors.New("boom"))
	})
	var envelope jsonErrorEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Code != ErrCodeInternal {
		t.Fatalf("code = %q, want %q", envelope.Error.Code, ErrCodeInternal)
	}
	if envelope.Error.Message != "boom" {
		t.Fatalf("message = %q, want boom", envelope.Error.Message)
	}
}

func TestWriteJSONErrorPreservesStructuredFields(t *testing.T) {
	err := newJSONError(ErrCodeEngineNotFound, "no runtime").
		withHint("install one").
		withAction(workflow.DoctorActionRuntimeSetup, "docker")

	out := captureStdout(t, func() {
		_ = writeJSONError(err)
	})
	var envelope jsonErrorEnvelope
	if jsonErr := json.Unmarshal([]byte(out), &envelope); jsonErr != nil {
		t.Fatalf("output not valid JSON: %v\n%s", jsonErr, out)
	}
	if envelope.Error.Code != ErrCodeEngineNotFound {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
	if envelope.Error.Hint != "install one" {
		t.Fatalf("hint = %q", envelope.Error.Hint)
	}
	if envelope.Error.Action != "runtime_setup" {
		t.Fatalf("action = %q, want runtime_setup", envelope.Error.Action)
	}
	if envelope.Error.ActionValue != "docker" {
		t.Fatalf("actionValue = %q", envelope.Error.ActionValue)
	}
}

func TestJSONCheckStatusMapping(t *testing.T) {
	cases := map[workflow.CheckStatus]string{
		workflow.CheckPass: "pass",
		workflow.CheckFail: "fail",
		workflow.CheckWarn: "warn",
		workflow.CheckInfo: "info",
	}
	for status, want := range cases {
		if got := jsonCheckStatus(status); got != want {
			t.Fatalf("jsonCheckStatus(%v) = %q, want %q", status, got, want)
		}
	}
}

func TestRunLifecycleJSONSuccessReusesStatusShape(t *testing.T) {
	cfg := &config.Config{ContainerName: "main", Image: "img", WebUIPort: "2337"}
	eng := &mockEngine{name: "docker", containerStatus: map[string]string{"main": "running"}}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runLifecycleJSON(cfg, eng, func() error { return nil })
	})
	if runErr != nil {
		t.Fatalf("runLifecycleJSON: %v", runErr)
	}
	var payload statusPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if payload.Status != "running" || payload.Container != "main" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunLifecycleJSONFailureEmitsStructuredError(t *testing.T) {
	cfg := &config.Config{ContainerName: "main"}
	eng := &mockEngine{}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runLifecycleJSON(cfg, eng, func() error { return errors.New("boom") })
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	var envelope jsonErrorEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Code != ErrCodeInternal || envelope.Error.Message != "boom" {
		t.Fatalf("unexpected error payload: %+v", envelope.Error)
	}
}

func TestJSONLogLineWriterSplitsOnNewlines(t *testing.T) {
	out := captureStdout(t, func() {
		w := newJSONLogWriter()
		_, _ = w.Write([]byte("first line\nsecond "))
		_, _ = w.Write([]byte("line\n"))
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d: %q", len(lines), out)
	}
	for i, want := range []string{"first line", "second line"} {
		var evt logLineEvent
		if err := json.Unmarshal([]byte(lines[i]), &evt); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
		if evt.Line != want {
			t.Fatalf("line %d = %q, want %q", i, evt.Line, want)
		}
		if evt.TS == "" {
			t.Fatalf("line %d missing ts", i)
		}
	}
}

func TestJSONLogLineWriterBuffersPartialLines(t *testing.T) {
	out := captureStdout(t, func() {
		w := newJSONLogWriter()
		_, _ = w.Write([]byte("no newline yet"))
	})
	if out != "" {
		t.Fatalf("expected no output before a newline, got %q", out)
	}
}

// alwaysErrWriter simulates a consumer that closed its end of the pipe: every
// write fails, the way a real broken NDJSON stdout pipe would.
type alwaysErrWriter struct{ err error }

func (w *alwaysErrWriter) Write([]byte) (int, error) { return 0, w.err }

// TestNDJSONEncoderBrokenStopsEmittingAfterWriteFailure is the regression
// guard for a broken output pipe going silently undetected: once a write to
// the consumer fails, broken() must report it and further emits must not
// attempt (or panic on) more writes.
func TestNDJSONEncoderBrokenStopsEmittingAfterWriteFailure(t *testing.T) {
	writeErr := errors.New("broken pipe")
	bw := bufio.NewWriter(&alwaysErrWriter{err: writeErr})
	nd := &ndjsonEncoder{w: bw, enc: json.NewEncoder(bw)}

	if nd.broken() {
		t.Fatal("a fresh encoder must not report broken")
	}

	nd.emit(stageEvent{Stage: "pull_image", State: "start"})
	if !nd.broken() {
		t.Fatal("expected broken() to be true after a failed write")
	}
	if nd.err == nil {
		t.Fatal("expected the encoder to record the write error")
	}

	// A second emit must be a no-op, not attempt (or panic on) another write.
	nd.emit(stageEvent{Stage: "run_container", State: "start"})
	if !nd.broken() {
		t.Fatal("encoder should remain broken")
	}
}

func TestJSONLogLineWriterReturnsErrorOnceEncoderIsBroken(t *testing.T) {
	bw := bufio.NewWriter(&alwaysErrWriter{err: errors.New("broken pipe")})
	w := &jsonLogLineWriter{enc: &ndjsonEncoder{w: bw, enc: json.NewEncoder(bw)}}
	_, err := w.Write([]byte("a line\n"))
	if err == nil {
		t.Fatal("expected Write to surface the broken pipe once the encoder fails")
	}
}

func TestJSONCheckResultsMapsFieldsAndEnums(t *testing.T) {
	results := []workflow.CheckResult{
		{Label: "Container runtime", Status: workflow.CheckPass, Detail: "Podman 5.8.4"},
		{Label: "Local AI (optional)", Status: workflow.CheckInfo, Detail: "Not connected", Hint: "optional"},
		{
			Label: "Omnideck instance", Status: workflow.CheckFail,
			Detail: "missing", Hint: "repair it",
			Action: workflow.DoctorActionRepairInstance, ActionLabel: "Repair", ActionValue: "main",
		},
	}
	payloads := jsonCheckResults(results)
	if len(payloads) != 3 {
		t.Fatalf("len = %d, want 3", len(payloads))
	}
	if payloads[0].Status != "pass" {
		t.Fatalf("status[0] = %q, want pass", payloads[0].Status)
	}
	if payloads[1].Status != "info" {
		t.Fatalf("status[1] = %q, want info", payloads[1].Status)
	}
	if payloads[2].Action != "repair_instance" || payloads[2].ActionValue != "main" || payloads[2].ActionLabel != "Repair" {
		t.Fatalf("action payload = %+v", payloads[2])
	}
}

// TestAllChecksPassIgnoresWarnAndInfo is the regression guard for
// JSON_MODE_SPEC.md's "Ollama stays optional" invariant: doctor --json's
// allPass must only be affected by CheckFail, matching
// tui.RenderDoctorReport's existing logic exactly.
func TestAllChecksPassIgnoresWarnAndInfo(t *testing.T) {
	results := []workflow.CheckResult{
		{Status: workflow.CheckPass},
		{Status: workflow.CheckWarn},
		{Status: workflow.CheckInfo},
	}
	if !allChecksPass(results) {
		t.Fatal("warn/info should not affect allPass")
	}
	results = append(results, workflow.CheckResult{Status: workflow.CheckFail})
	if allChecksPass(results) {
		t.Fatal("a single fail should flip allPass to false")
	}
}

func TestJSONActionMapping(t *testing.T) {
	cases := map[workflow.CheckAction]string{
		workflow.DoctorActionNone:           "",
		workflow.DoctorActionRuntimeSetup:   "runtime_setup",
		workflow.DoctorActionStartInstance:  "start_instance",
		workflow.DoctorActionSetupInstance:  "setup_instance",
		workflow.DoctorActionRepairInstance: "repair_instance",
	}
	for action, want := range cases {
		if got := jsonAction(action); got != want {
			t.Fatalf("jsonAction(%v) = %q, want %q", action, got, want)
		}
	}
}
