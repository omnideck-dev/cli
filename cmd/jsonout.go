package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

// jsonContract is bumped whenever a JSON shape documented in
// JSON_MODE_SPEC.md changes in a way that isn't backward compatible. A
// calling GUI checks this against the value it was built against before
// trusting any other --json output.
const jsonContract = 3

// Closed set of --json error codes. Every error surfaced under --json must
// resolve to one of these — see JSON_MODE_SPEC.md's "Structured error shape".
const (
	ErrCodeAmbiguousInstance     = "AMBIGUOUS_INSTANCE"
	ErrCodeNotInstalled          = "NOT_INSTALLED"
	ErrCodeEngineNotFound        = "ENGINE_NOT_FOUND"
	ErrCodeContainerNotFound     = "CONTAINER_NOT_FOUND"
	ErrCodeMissingRequiredFlag   = "MISSING_REQUIRED_FLAG"
	ErrCodeMissingSubcommand     = "MISSING_SUBCOMMAND"
	ErrCodeCancelled             = "CANCELLED"
	ErrCodeRestartRequired       = "RESTART_REQUIRED"
	ErrCodePermissionDenied      = "PERMISSION_DENIED"
	ErrCodePermissionCancelled   = "PERMISSION_CANCELLED"
	ErrCodeWindowsFeaturesFailed = "WINDOWS_FEATURES_FAILED"
	ErrCodePackageIndexFailed    = "PACKAGE_INDEX_FAILED"
	ErrCodeInstallerFailed       = "INSTALLER_FAILED"
	ErrCodeDownloadFailed        = "DOWNLOAD_FAILED"
	ErrCodeUnsupported           = "UNSUPPORTED"
	ErrCodeRuntimeSetupFailed    = "RUNTIME_SETUP_FAILED"
	ErrCodePortInUse             = "PORT_IN_USE"
	ErrCodeContainerConflict     = "CONTAINER_CONFLICT"
	ErrCodeInternal              = "INTERNAL_ERROR"
)

// jsonAction is the stable string form of workflow.CheckAction, shared by
// doctor --json's per-check "action" field and the structured error shape's
// "action" field so both surfaces use one closed vocabulary.
func jsonAction(a workflow.CheckAction) string {
	switch a {
	case workflow.DoctorActionRuntimeSetup:
		return "runtime_setup"
	case workflow.DoctorActionStartInstance:
		return "start_instance"
	case workflow.DoctorActionSetupInstance:
		return "setup_instance"
	case workflow.DoctorActionRepairInstance:
		return "repair_instance"
	default:
		return ""
	}
}

// jsonCheckStatus is the stable string form of workflow.CheckStatus.
func jsonCheckStatus(s workflow.CheckStatus) string {
	switch s {
	case workflow.CheckPass:
		return "pass"
	case workflow.CheckFail:
		return "fail"
	case workflow.CheckWarn:
		return "warn"
	default:
		return "info"
	}
}

// jsonErrorPayload is the "error" object nested in the structured error
// envelope. Hint/Action/ActionValue/Instances are optional context; Code and
// Message are always present.
type jsonErrorPayload struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Hint        string   `json:"hint,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Action      string   `json:"action,omitempty"`
	ActionValue string   `json:"actionValue,omitempty"`
	Instances   []string `json:"instances,omitempty"`
}

type jsonErrorEnvelope struct {
	Error jsonErrorPayload `json:"error"`
}

// jsonCmdError carries a structured jsonErrorPayload through Go's error
// interface so it survives being returned from a cobra RunE and can still be
// rendered as the full structured shape by writeJSONError.
type jsonCmdError struct {
	payload jsonErrorPayload
}

func (e *jsonCmdError) Error() string { return e.payload.Message }

// newJSONError builds a structured --json error. Chain with/With* to attach
// optional context before returning it from a command.
func newJSONError(code, message string) *jsonCmdError {
	return &jsonCmdError{payload: jsonErrorPayload{Code: code, Message: message}}
}

func (e *jsonCmdError) withHint(hint string) *jsonCmdError {
	e.payload.Hint = hint
	return e
}

func (e *jsonCmdError) withDetail(detail string) *jsonCmdError {
	e.payload.Detail = detail
	return e
}

func (e *jsonCmdError) withAction(action workflow.CheckAction, value string) *jsonCmdError {
	e.payload.Action = jsonAction(action)
	e.payload.ActionValue = value
	return e
}

func (e *jsonCmdError) withInstances(names []string) *jsonCmdError {
	e.payload.Instances = names
	return e
}

// writeJSON marshals v as a single JSON value to stdout, newline-terminated.
func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(v)
}

// writeJSONError prints the structured error envelope for err to stdout and
// returns errAborted so callers can `return writeJSONError(err)` from a
// cobra RunE. errAborted carries an empty message, so cobra's default
// "Error: ..." stderr line stays effectively blank instead of duplicating
// the message that's already been written to stdout as JSON — the same
// convention cmd/status.go and cmd/doctor.go already use for their own
// redundant-error suppression.
func writeJSONError(err error) error {
	var jerr *jsonCmdError
	var payload jsonErrorPayload
	if errors.As(err, &jerr) {
		payload = jerr.payload
	} else {
		payload = jsonErrorPayload{Code: ErrCodeInternal, Message: err.Error()}
	}
	writeJSON(jsonErrorEnvelope{Error: payload})
	return errAborted
}

// ndjsonEncoder emits one flushed JSON object per line for streaming
// commands (add/update/remove progress, logs --follow). Each Emit call is
// written and flushed immediately so a consumer sees progress incrementally
// rather than in one burst at process exit.
type ndjsonEncoder struct {
	w   *bufio.Writer
	enc *json.Encoder
	// err is set by the first emit that fails to reach the consumer (e.g. a
	// closed pipe on the far end). Once set, further emits are no-ops: once
	// nothing is listening, there's no point doing more work, or more I/O,
	// to describe progress no one will see.
	err error
}

func newNDJSONEncoder() *ndjsonEncoder {
	w := bufio.NewWriter(os.Stdout)
	return &ndjsonEncoder{w: w, enc: json.NewEncoder(w)}
}

// broken reports whether a previous emit failed to reach the consumer.
// Callers running a multi-step streaming command should check this between
// steps and stop rather than performing further irreversible work (pulling
// an image, starting a container) that nothing will ever be told about.
func (n *ndjsonEncoder) broken() bool { return n.err != nil }

func (n *ndjsonEncoder) emit(v any) {
	if n.err != nil {
		return
	}
	if err := n.enc.Encode(v); err != nil {
		n.err = err
		return
	}
	if err := n.w.Flush(); err != nil {
		n.err = err
	}
}

// stageEvent is one line of the shared NDJSON progress shape used by
// add/update/remove --json. State is "start" | "progress" | "done" | "error".
type stageEvent struct {
	Stage  string            `json:"stage"`
	State  string            `json:"state"`
	Detail string            `json:"detail,omitempty"`
	Error  *jsonErrorPayload `json:"error,omitempty"`
	Result any               `json:"result,omitempty"`
}

func (n *ndjsonEncoder) start(stage string) { n.emit(stageEvent{Stage: stage, State: "start"}) }
func (n *ndjsonEncoder) done(stage string)  { n.emit(stageEvent{Stage: stage, State: "done"}) }
func (n *ndjsonEncoder) progress(stage, detail string) {
	n.emit(stageEvent{Stage: stage, State: "progress", Detail: detail})
}
func (n *ndjsonEncoder) complete(result any) {
	n.emit(stageEvent{Stage: "complete", State: "done", Result: result})
}

// fail emits the terminal error event for a streaming command and returns
// errAborted, mirroring writeJSONError's convention for non-streaming
// commands.
func (n *ndjsonEncoder) fail(stage string, err error) error {
	var jerr *jsonCmdError
	var payload jsonErrorPayload
	if errors.As(err, &jerr) {
		payload = jerr.payload
	} else {
		payload = jsonErrorPayload{Code: ErrCodeInternal, Message: err.Error()}
	}
	n.emit(stageEvent{Stage: stage, State: "error", Error: &payload})
	return errAborted
}

// jsonNowRFC3339 is a seam so log-line timestamps are deterministic in tests.
var jsonNowRFC3339 = func() string { return time.Now().UTC().Format(time.RFC3339) }

// volumePayload/ollamaPayload/statusPayload are the "status --json" shape.
// They're also reused, verbatim, by start/stop/restart --json (gathered
// fresh after the action) and by add/update's NDJSON "complete" result.
type volumePayload struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

type ollamaPayload struct {
	Reachable bool   `json:"reachable"`
	Host      string `json:"host"`
}

type statusPayload struct {
	Name        string        `json:"name"`
	Container   string        `json:"container"`
	Status      string        `json:"status"`
	Image       string        `json:"image"`
	Engine      string        `json:"engine"`
	WebUIPort   string        `json:"webUiPort"`
	HomeVolume  volumePayload `json:"homeVolume"`
	StateVolume volumePayload `json:"stateVolume"`
	Ollama      ollamaPayload `json:"ollama"`
}

// gatherStatusPayload concurrently gathers the same data cmd/status.go's
// human-readable table renders (container status, Ollama reachability,
// volume existence), for reuse by every --json shape documented as "same
// shape as status --json".
func gatherStatusPayload(cfg *config.Config, eng engine.Engine) statusPayload {
	var (
		containerStatus   string
		ollamaOK          bool
		ollamaHost        string
		homeVolumeExists  bool
		stateVolumeExists bool
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		s, err := eng.ContainerStatus(cfg.ContainerName)
		if err == nil {
			containerStatus = s
		}
	}()
	go func() {
		defer wg.Done()
		ollamaOK, ollamaHost = checks.CheckOllama()
	}()
	go func() {
		defer wg.Done()
		homeVolumeExists, _ = eng.VolumeExists(cfg.HomeVolumeName())
		stateVolumeExists, _ = eng.VolumeExists(cfg.StateVolumeName())
	}()
	wg.Wait()

	if containerStatus == "" {
		containerStatus = "unknown"
	}

	return statusPayload{
		Name:        cfg.ContainerName,
		Container:   cfg.ContainerName,
		Status:      containerStatus,
		Image:       cfg.Image,
		Engine:      eng.Name(),
		WebUIPort:   cfg.WebUIPortOrDefault(),
		HomeVolume:  volumePayload{Name: cfg.HomeVolumeName(), Exists: homeVolumeExists},
		StateVolume: volumePayload{Name: cfg.StateVolumeName(), Exists: stateVolumeExists},
		Ollama:      ollamaPayload{Reachable: ollamaOK, Host: ollamaHost},
	}
}

// listEntry is one element of "list --json"'s array. Every entry has the
// same set of keys, always: live-stat fields are explicit null (via pointer
// types with no omitempty) when the instance isn't running, never omitted
// and never a zero value that could be mistaken for a real reading.
type listEntry struct {
	Name      string   `json:"name"`
	Container string   `json:"container"`
	Status    string   `json:"status"`
	Image     string   `json:"image"`
	WebUIPort string   `json:"webUiPort"`
	CPU       *string  `json:"cpu"`
	CPUPct    *float64 `json:"cpuPct"`
	RAM       *string  `json:"ram"`
	RAMTotal  *string  `json:"ramTotal"`
	RAMPct    *float64 `json:"ramPct"`
	Uptime    *string  `json:"uptime"`
	Restarts  *int     `json:"restarts"`
	Health    string   `json:"health"`
	Created   string   `json:"created"`
}

// formatUptime mirrors tui/instance_runtime.go's unexported formatDuration
// so list --json's uptime string matches the app's own dashboard formatting.
func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	if h < 24 {
		return fmt.Sprintf("%dh %dm", h, int(d.Minutes())%60)
	}
	days := h / 24
	return fmt.Sprintf("%dd %dh", days, h%24)
}

// gatherListEntry builds one list --json array element. eng may be nil (no
// ready container runtime); in that case the entry reports what's known
// from saved config alone, with status "unknown" and every live field nil.
//
// ContainerStatus and ContainerInspect are independent engine calls, so
// they're fetched concurrently rather than one after another — each is its
// own Podman subprocess, and gatherListEntries already expects to be
// polled repeatedly by a live dashboard.
func gatherListEntry(eng engine.Engine, instance config.InstanceInfo) listEntry {
	entry := listEntry{Name: instance.Name, Status: "unknown"}
	if instance.Config != nil {
		entry.Container = instance.Config.ContainerName
		entry.Image = instance.Config.Image
		entry.WebUIPort = instance.Config.WebUIPortOrDefault()
	}
	if eng == nil || instance.Config == nil {
		return entry
	}
	name := instance.Config.ContainerName

	var (
		status    string
		inspect   engine.InspectData
		inspectOK bool
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		status, _ = eng.ContainerStatus(name)
	}()
	go func() {
		defer wg.Done()
		var err error
		inspect, err = eng.ContainerInspect(name)
		inspectOK = err == nil
	}()
	wg.Wait()

	if status != "" {
		entry.Status = status
	}
	active := workflow.IsActiveContainerStatus(entry.Status)
	if active {
		cpu, cpuPct, ram, ramTotal, ramPct, err := eng.ContainerStats(name)
		if err == nil {
			entry.CPU, entry.CPUPct = &cpu, &cpuPct
			entry.RAM, entry.RAMTotal, entry.RAMPct = &ram, &ramTotal, &ramPct
		}
	}
	if inspectOK {
		entry.Health = inspect.HealthStatus
		if !inspect.CreatedAt.IsZero() {
			entry.Created = inspect.CreatedAt.Format("2006-01-02")
		}
		if active {
			restarts := inspect.RestartCount
			entry.Restarts = &restarts
			if !inspect.StartedAt.IsZero() {
				uptime := formatUptime(time.Since(inspect.StartedAt))
				entry.Uptime = &uptime
			}
		}
	}
	return entry
}

// logsPayload is the non-follow "logs --json" shape.
type logsPayload struct {
	Lines []string `json:"lines"`
}

// logLineEvent is one line of "logs --follow --json"'s NDJSON stream. TS is
// an RFC 3339 timestamp of when this CLI process observed the line, not a
// timestamp parsed from the container's own log output — the container log
// format doesn't carry a reliable one today.
type logLineEvent struct {
	Line string `json:"line"`
	TS   string `json:"ts"`
}

// jsonLogLineWriter is the io.Writer passed to Engine.TailLogs for
// --follow --json: it splits arbitrary byte writes on newlines and emits one
// flushed NDJSON logLineEvent per complete line, keeping the engine itself
// completely agnostic of JSON formatting.
type jsonLogLineWriter struct {
	enc *ndjsonEncoder
	buf []byte
}

func newJSONLogWriter() *jsonLogLineWriter {
	return &jsonLogLineWriter{enc: newNDJSONEncoder()}
}

func (w *jsonLogLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		w.enc.emit(logLineEvent{Line: line, TS: jsonNowRFC3339()})
		if w.enc.broken() {
			return len(p), w.enc.err
		}
	}
	return len(p), nil
}

// checkResultPayload is one entry of "doctor --json"'s checks array —
// workflow.CheckResult with its enums mapped to stable strings.
type checkResultPayload struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	Hint        string `json:"hint"`
	Action      string `json:"action,omitempty"`
	ActionLabel string `json:"actionLabel,omitempty"`
	ActionValue string `json:"actionValue,omitempty"`
}

type doctorPayload struct {
	Checks  []checkResultPayload `json:"checks"`
	AllPass bool                 `json:"allPass"`
}

func jsonCheckResults(results []workflow.CheckResult) []checkResultPayload {
	out := make([]checkResultPayload, len(results))
	for i, r := range results {
		out[i] = checkResultPayload{
			ID:          r.ID,
			Label:       r.Label,
			Status:      jsonCheckStatus(r.Status),
			Detail:      r.Detail,
			Hint:        r.Hint,
			Action:      jsonAction(r.Action),
			ActionLabel: r.ActionLabel,
			ActionValue: r.ActionValue,
		}
	}
	return out
}

// allChecksPass matches tui.RenderDoctorReport's allPass logic exactly:
// only CheckFail flips it — CheckWarn/CheckInfo (including Ollama) never do.
func allChecksPass(results []workflow.CheckResult) bool {
	for _, r := range results {
		if r.Status == workflow.CheckFail {
			return false
		}
	}
	return true
}

// configPayload is "config show --json" and "config set --json"'s shape.
type configPayload struct {
	ContainerName string `json:"containerName"`
	HomeVolume    string `json:"homeVolume"`
	StateVolume   string `json:"stateVolume"`
	Memory        string `json:"memory"`
	ShmSize       string `json:"shmSize"`
	WebUIPort     string `json:"webUiPort"`
	Runtime       string `json:"runtime"`
	Image         string `json:"image"`
	InstalledAt   string `json:"installedAt"`
}

func newConfigPayload(cfg *config.Config, runtimeName string) configPayload {
	return configPayload{
		ContainerName: cfg.ContainerName,
		HomeVolume:    cfg.HomeVolumeName(),
		StateVolume:   cfg.StateVolumeName(),
		Memory:        cfg.Memory,
		ShmSize:       cfg.ShmSize,
		WebUIPort:     cfg.WebUIPortOrDefault(),
		Runtime:       runtimeName,
		Image:         cfg.Image,
		InstalledAt:   cfg.InstalledAt.Format(time.RFC3339),
	}
}

// configPathPayload is "config path --json"'s shape.
type configPathPayload struct {
	Path string `json:"path"`
}

// removeResultPayload is "remove --plain --json"'s final result shape,
// mirroring workflow.RemoveInstanceResult field-for-field.
type removeResultPayload struct {
	ContainerStopped bool     `json:"containerStopped"`
	ContainerRemoved bool     `json:"containerRemoved"`
	RemovedVolumes   []string `json:"removedVolumes"`
	BackupPath       string   `json:"backupPath,omitempty"`
}

// runLifecycleJSON runs a start/stop/restart action and, on success, emits
// the same shape as status --json gathered fresh afterward — so a caller
// can re-render its UI from one response without an immediate follow-up
// status call. On failure it emits the standard structured error.
func runLifecycleJSON(cfg *config.Config, eng engine.Engine, action func() error) error {
	if err := action(); err != nil {
		return writeJSONError(newJSONError(ErrCodeInternal, err.Error()))
	}
	writeJSON(gatherStatusPayload(cfg, eng))
	return nil
}

// gatherListEntries fetches every instance's entry concurrently, matching
// the concurrency pattern cmd/status.go already uses for a single instance.
func gatherListEntries(eng engine.Engine, instances []config.InstanceInfo) []listEntry {
	entries := make([]listEntry, len(instances))
	var wg sync.WaitGroup
	for i, instance := range instances {
		wg.Add(1)
		go func(i int, instance config.InstanceInfo) {
			defer wg.Done()
			entries[i] = gatherListEntry(eng, instance)
		}(i, instance)
	}
	wg.Wait()
	return entries
}
