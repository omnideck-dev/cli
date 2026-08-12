package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	PodmanInstallerVersion = "v6.0.2"
	SetupStageSoftware     = "software"
	SetupStageEnvironment  = "environment"

	SetupActivitySoftware    = "Getting your computer ready…"
	SetupActivityEnvironment = "Preparing a secure space to run in…"

	SetupSubstageWSLPermission    = "wsl-permission"
	SetupSubstageWSLEnable        = "wsl-enable"
	SetupSubstageWindowsRestart   = "windows-restart"
	SetupSubstagePodmanPermission = "podman-permission"
	SetupSubstagePodmanDownload   = "podman-download"
	SetupSubstagePodmanInstall    = "podman-install"
	SetupSubstageMacPermission    = "macos-permission"
	SetupSubstagePackageIndex     = "package-index"
	SetupSubstagePackageInstall   = "podman-install"
	SetupSubstageSecureSpace      = "secure-space"
)

type RuntimeSetupFailure string

const (
	RuntimeSetupComponents          RuntimeSetupFailure = "components"
	RuntimeSetupPermission          RuntimeSetupFailure = "permission"
	RuntimeSetupDownload            RuntimeSetupFailure = "downloads"
	RuntimeSetupEnvironment         RuntimeSetupFailure = "environment"
	RuntimeSetupRestart             RuntimeSetupFailure = "restart"
	RuntimeSetupSupport             RuntimeSetupFailure = "support"
	RuntimeSetupPermissionCancelled RuntimeSetupFailure = "permission_cancelled"
	RuntimeSetupWindowsFeatures     RuntimeSetupFailure = "windows_features"
	RuntimeSetupPackageIndex        RuntimeSetupFailure = "package_index"
	RuntimeSetupInstaller           RuntimeSetupFailure = "installer"
)

// RuntimeSetupEvent is the shared setup progress contract consumed by both the
// TUI and desktop. Stage and activity use the desktop setup vocabulary.
type RuntimeSetupEvent struct {
	Stage    string
	Substage string
	State    string
	Activity string
	Status   string
	Detail   string
	Progress *float64
}

// RuntimeSetupError carries the user-facing failure category and next step
// without requiring either front end to classify command output.
type RuntimeSetupError struct {
	Failure RuntimeSetupFailure
	Message string
	Hint    string
	Err     error
}

func (e *RuntimeSetupError) Error() string { return e.Message }
func (e *RuntimeSetupError) Unwrap() error { return e.Err }

// RuntimeSetupOptions configures the shared prerequisite and runtime setup.
// The private seams are populated only by unit tests in this package.
type RuntimeSetupOptions struct {
	Host                   HostPlatform
	DownloadRoot           string
	OnEvent                func(RuntimeSetupEvent)
	AllowTerminalElevation bool

	probe      func() ProbeResult
	ensureHost func(HostPlatform, func(RuntimeSetupEvent)) error
	install    func(HostPlatform, string, func(RuntimeSetupEvent)) error
	runCommand func(SetupCommand, func(string)) error
}

func emitRuntimeSetup(onEvent func(RuntimeSetupEvent), event RuntimeSetupEvent) {
	if onEvent != nil {
		onEvent(event)
	}
}

func setupActivity(stage string) string {
	if stage == SetupStageEnvironment {
		return SetupActivityEnvironment
	}
	return SetupActivitySoftware
}

func setupEvent(stage, state, detail string) RuntimeSetupEvent {
	return RuntimeSetupEvent{
		Stage:    stage,
		State:    state,
		Activity: setupActivity(stage),
		Detail:   detail,
	}
}

func setupSubstageEvent(stage, substage, state, activity, status, detail string) RuntimeSetupEvent {
	return RuntimeSetupEvent{
		Stage:    stage,
		Substage: substage,
		State:    state,
		Activity: activity,
		Status:   status,
		Detail:   detail,
	}
}

func setupProgress(stage, detail string, fraction float64) RuntimeSetupEvent {
	event := setupEvent(stage, "progress", detail)
	if fraction >= 0 {
		bounded := max(0, min(1, fraction))
		event.Progress = &bounded
	}
	return event
}

func setupSubstageProgress(stage, substage, activity, status, detail string, fraction float64) RuntimeSetupEvent {
	event := setupSubstageEvent(stage, substage, "progress", activity, status, detail)
	if fraction >= 0 {
		bounded := max(0, min(1, fraction))
		event.Progress = &bounded
	}
	return event
}

func runtimeSetupError(failure RuntimeSetupFailure, message, hint string, err error) error {
	return &RuntimeSetupError{Failure: failure, Message: message, Hint: hint, Err: err}
}

func runtimeSetupCommandProgress(command SetupCommand, host HostPlatform) (string, string) {
	status := "Podman machine starting"
	if host.OS == "darwin" && command.Name == "podman" && len(command.Args) == 3 &&
		command.Args[0] == "machine" && command.Args[1] == "stop" && command.Args[2] != OmnideckMachineName {
		return "Switching Podman machines", fmt.Sprintf(
			"macOS can run only one Podman machine at a time. Stopping %q keeps its files but also stops its running containers.",
			command.Args[2],
		)
	}
	return status, ""
}

// EnsureRuntime installs host prerequisites and Podman when needed, prepares
// the shared omnideck-runtime machine on macOS/Windows, and verifies readiness.
func EnsureRuntime(options RuntimeSetupOptions) (ProbeResult, error) {
	host := options.Host
	if host.OS == "" {
		host = DetectHostPlatform()
	}
	probe := options.probe
	if probe == nil {
		probe = func() ProbeResult {
			probes := ProbeAll()
			if len(probes) == 0 {
				return ProbeResult{Name: "podman", State: RuntimeMissing}
			}
			return probes[0]
		}
	}
	ensureHost := options.ensureHost
	if ensureHost == nil {
		ensureHost = ensureRuntimeHostPrerequisites
	}
	install := options.install
	if install == nil {
		install = func(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent)) error {
			return installPodmanRuntime(host, downloadRoot, onEvent, options.AllowTerminalElevation)
		}
	}
	runCommand := options.runCommand
	if runCommand == nil {
		runCommand = RunSetupCommand
	}
	downloadRoot := options.DownloadRoot

	current := probe()
	if current.Ready() {
		return current, nil
	}

	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageSoftware, "start", ""))
	if err := ensureHost(host, options.OnEvent); err != nil {
		return current, err
	}

	if current.State == RuntimeMissing || current.State == RuntimeUnsupportedVersion {
		if downloadRoot == "" {
			cacheRoot, err := os.UserCacheDir()
			if err != nil {
				return current, runtimeSetupError(
					RuntimeSetupComponents,
					"omnideck could not prepare a download location.",
					"Check that your account can write to its local application-data folder, then try again.",
					err,
				)
			}
			downloadRoot = filepath.Join(cacheRoot, "omnideck-cli", "downloads")
		}
		if err := install(host, downloadRoot, options.OnEvent); err != nil {
			return current, err
		}
		current = probe()
		if current.State == RuntimeMissing {
			return current, runtimeSetupError(
				RuntimeSetupComponents,
				"Podman was installed but omnideck still cannot open it.",
				"Restart this terminal and try again. If Podman is still missing, restart the computer.",
				nil,
			)
		}
	}
	emitRuntimeSetup(options.OnEvent, setupProgress(SetupStageSoftware, "", 1))
	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageSoftware, "done", ""))

	if current.Ready() {
		return current, nil
	}

	plans := BuildSetupPlans([]ProbeResult{current}, host)
	if len(plans) == 0 || len(plans[0].Commands) == 0 {
		return current, runtimeSetupError(
			RuntimeSetupEnvironment,
			"The secure workspace needs attention before omnideck can use it.",
			"Restart the computer, then try setup again. If it still fails, open diagnostics for the Podman details.",
			nil,
		)
	}

	commands := plans[0].Commands
	initialStatus, initialDetail := runtimeSetupCommandProgress(commands[0], host)
	emitRuntimeSetup(options.OnEvent, setupSubstageEvent(
		SetupStageEnvironment,
		SetupSubstageSecureSpace,
		"start",
		SetupActivityEnvironment,
		initialStatus,
		initialDetail,
	))
	for i, command := range commands {
		status, detail := runtimeSetupCommandProgress(command, host)
		if i > 0 {
			emitRuntimeSetup(options.OnEvent, setupSubstageProgress(
				SetupStageEnvironment,
				SetupSubstageSecureSpace,
				SetupActivityEnvironment,
				status,
				detail,
				float64(i)/float64(len(commands)),
			))
		}
		err := runCommand(command, func(line string) {
			fraction := float64(i) / float64(len(commands))
			emitRuntimeSetup(options.OnEvent, setupSubstageProgress(
				SetupStageEnvironment,
				SetupSubstageSecureSpace,
				SetupActivityEnvironment,
				status,
				line,
				fraction,
			))
		})
		if err != nil {
			return current, runtimeSetupError(
				RuntimeSetupEnvironment,
				"The secure workspace could not be prepared.",
				"Restart the computer and try again. Anything already downloaded will be reused.",
				err,
			)
		}
		emitRuntimeSetup(options.OnEvent, setupSubstageProgress(
			SetupStageEnvironment,
			SetupSubstageSecureSpace,
			SetupActivityEnvironment,
			status,
			"",
			float64(i+1)/float64(len(commands)),
		))
	}

	current = probe()
	if !current.Ready() {
		return current, runtimeSetupError(
			RuntimeSetupEnvironment,
			"The secure workspace was prepared but is not responding.",
			"Restart the computer and try again. If it still fails, open diagnostics for the Podman details.",
			nil,
		)
	}
	emitRuntimeSetup(options.OnEvent, setupSubstageEvent(
		SetupStageEnvironment,
		SetupSubstageSecureSpace,
		"done",
		SetupActivityEnvironment,
		"Secure space ready",
		"",
	))
	return current, nil
}
