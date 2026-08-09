package cmd

import (
	"errors"
	"fmt"

	"github.com/omnideck-dev/cli/engine"
	"github.com/spf13/cobra"
)

// Schema 4 adds the shared container and machine resource defaults consumed by
// Desktop. Setup events and automatic prerequisite installation arrived in 3.
const runtimeStatusSchema = 4

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Inspect or prepare the Podman runtime",
}

var runtimeStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Report whether Podman is ready",
	SilenceUsage: true,
	RunE:         runRuntimeStatus,
}

var runtimeEnsureCmd = &cobra.Command{
	Use:          "ensure",
	Short:        "Finish Podman's safe one-time machine setup",
	SilenceUsage: true,
	RunE:         runRuntimeEnsure,
}

func init() {
	rootCmd.AddCommand(runtimeCmd)
	runtimeCmd.AddCommand(runtimeStatusCmd, runtimeEnsureCmd)
}

type runtimeStatusPayload struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Runtime       string                 `json:"runtime"`
	State         engine.RuntimeState    `json:"state"`
	Ready         bool                   `json:"ready"`
	Path          string                 `json:"path,omitempty"`
	Version       string                 `json:"version,omitempty"`
	Detail        string                 `json:"detail,omitempty"`
	Warning       string                 `json:"warning,omitempty"`
	MachineName   string                 `json:"machineName,omitempty"`
	Phase         string                 `json:"phase"`
	Activity      string                 `json:"activity"`
	Resources     runtimeResourcePayload `json:"resources"`
}

type runtimeResourcePayload struct {
	Container runtimeContainerResources `json:"container"`
	Machine   runtimeMachineResources   `json:"machine"`
}

type runtimeContainerResources struct {
	Memory  string `json:"memory"`
	SHMSize string `json:"shmSize"`
}

type runtimeMachineResources struct {
	Mode     string `json:"mode"`
	CPUs     int    `json:"cpus,omitempty"`
	MemoryMB int64  `json:"memoryMB,omitempty"`
	DiskGB   int    `json:"diskGB,omitempty"`
}

var runtimeProbe = func() engine.ProbeResult {
	probes := engine.ProbeAll()
	if len(probes) == 0 {
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeMissing}
	}
	return probes[0]
}

var runtimeHostPlatform = engine.DetectHostPlatform
var runtimeEnsure = engine.EnsureRuntime

func runtimePayload(probe engine.ProbeResult, host engine.HostPlatform) runtimeStatusPayload {
	resources := engine.DefaultRuntimeResources(host)
	payload := runtimeStatusPayload{
		SchemaVersion: runtimeStatusSchema,
		Runtime:       "podman",
		State:         probe.State,
		Ready:         probe.Ready(),
		Path:          probe.Path,
		Version:       probe.Version,
		Detail:        probe.Detail,
		Warning:       probe.Warning,
		MachineName:   probe.MachineName,
		Phase:         "environment",
		Activity:      "Preparing a secure space to run in…",
		Resources: runtimeResourcePayload{
			Container: runtimeContainerResources{
				Memory:  resources.ContainerMemory,
				SHMSize: resources.ContainerSHMSize,
			},
			Machine: runtimeMachineResources{
				Mode:     resources.MachineMode,
				CPUs:     resources.MachineCPUs,
				MemoryMB: resources.MachineMemoryMB,
				DiskGB:   resources.MachineDiskGB,
			},
		},
	}
	if probe.State == engine.RuntimeMissing || probe.State == engine.RuntimeUnsupportedVersion {
		payload.Phase = "software"
		payload.Activity = "Getting your computer ready…"
	}
	return payload
}

func runRuntimeStatus(_ *cobra.Command, _ []string) error {
	payload := runtimePayload(runtimeProbe(), runtimeHostPlatform())
	if jsonFlag {
		writeJSON(payload)
		return nil
	}
	fmt.Printf("Podman: %s\n", engine.RuntimeStateLabel(payload.State))
	if payload.MachineName != "" {
		fmt.Printf("Machine: %s\n", payload.MachineName)
	}
	if payload.Detail != "" {
		fmt.Printf("Details: %s\n", payload.Detail)
	}
	return nil
}

func runRuntimeEnsure(_ *cobra.Command, _ []string) error {
	host := runtimeHostPlatform()
	probe := runtimeProbe()
	if probe.Ready() {
		if jsonFlag {
			writeJSON(runtimePayload(probe, host))
		} else {
			fmt.Println("Podman is ready.")
		}
		return nil
	}

	var nd *ndjsonEncoder
	lastStage := engine.SetupStageSoftware
	if jsonFlag {
		nd = newNDJSONEncoder()
	}
	probe, err := runtimeEnsure(engine.RuntimeSetupOptions{
		Host: host,
		OnEvent: func(event engine.RuntimeSetupEvent) {
			lastStage = event.Stage
			if nd != nil {
				nd.emit(runtimeSetupEventPayload{
					Stage:    event.Stage,
					Substage: event.Substage,
					State:    event.State,
					Activity: event.Activity,
					Status:   event.Status,
					Detail:   event.Detail,
					Progress: event.Progress,
				})
			}
		},
	})
	if err != nil {
		structured := runtimeSetupJSONError(err)
		if nd != nil {
			return nd.fail(lastStage, structured)
		}
		return structured
	}
	if nd != nil {
		nd.complete(runtimePayload(probe, host))
		return nil
	}
	fmt.Println("Podman is ready.")
	return nil
}

type runtimeSetupEventPayload struct {
	Stage    string   `json:"stage"`
	Substage string   `json:"substage,omitempty"`
	State    string   `json:"state"`
	Activity string   `json:"activity,omitempty"`
	Status   string   `json:"status,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Progress *float64 `json:"progress,omitempty"`
}

func runtimeSetupJSONError(err error) *jsonCmdError {
	var setupError *engine.RuntimeSetupError
	if !errors.As(err, &setupError) {
		return newJSONError(ErrCodeRuntimeSetupFailed, err.Error())
	}
	code := ErrCodeRuntimeSetupFailed
	switch setupError.Failure {
	case engine.RuntimeSetupRestart:
		code = ErrCodeRestartRequired
	case engine.RuntimeSetupPermission:
		code = ErrCodePermissionDenied
	case engine.RuntimeSetupPermissionCancelled:
		code = ErrCodePermissionCancelled
	case engine.RuntimeSetupWindowsFeatures:
		code = ErrCodeWindowsFeaturesFailed
	case engine.RuntimeSetupPackageIndex:
		code = ErrCodePackageIndexFailed
	case engine.RuntimeSetupInstaller:
		code = ErrCodeInstallerFailed
	case engine.RuntimeSetupDownload:
		code = ErrCodeDownloadFailed
	case engine.RuntimeSetupSupport:
		code = ErrCodeUnsupported
	}
	structured := newJSONError(code, setupError.Message).withHint(setupError.Hint)
	if setupError.Err != nil {
		structured.withDetail(setupError.Err.Error())
	}
	return structured
}
