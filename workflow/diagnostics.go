package workflow

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// CheckStatus describes whether a Doctor result needs the user's attention.
type CheckStatus int

const (
	CheckPass CheckStatus = iota
	CheckFail
	CheckWarn
	CheckInfo
)

// CheckAction is a safe next step Doctor can perform or open for the user.
type CheckAction int

const (
	DoctorActionNone CheckAction = iota
	DoctorActionRuntimeSetup
	DoctorActionStartInstance
	DoctorActionSetupInstance
	DoctorActionRepairInstance
)

// CheckResult is one plain-language Doctor result. Action fields are used by
// the interactive Doctor screen; Hint remains useful in the plain report.
type CheckResult struct {
	ID          string
	Label       string
	Status      CheckStatus
	Detail      string
	Hint        string
	Action      CheckAction
	ActionLabel string
	ActionValue string
}

// Diagnose checks the selected instance. Runtime health comes from
// the same probes and setup plans as guided Setup, so the two flows cannot
// disagree about what is installed or how to make it ready.
//
// name is the instance name `--name` accepts (which can differ from
// cfg.ContainerName); hints and ActionValue use it so a suggested command
// selects this instance. An empty name means the instance cannot be selected
// by name, and hints omit the flag.
func Diagnose(name string, cfg *config.Config, eng engine.Engine) []CheckResult {
	results, _ := DiagnoseWithProbes(name, cfg, eng, engine.ProbeAll())
	return results
}

func DiagnoseWithProbes(name string, cfg *config.Config, eng engine.Engine, probes []engine.ProbeResult) ([]CheckResult, engine.Engine) {
	runtimeResult, usableEngine := doctorRuntimeCheck(eng, probes)
	results := []CheckResult{runtimeResult}

	if cfg == nil {
		results = append(results,
			CheckResult{
				Label:       "Omnideck setup",
				Status:      CheckFail,
				Detail:      "No Omnideck instance has been set up yet",
				Hint:        "Run: omnideck setup",
				Action:      DoctorActionSetupInstance,
				ActionLabel: "Set up Omnideck",
			},
			CheckResult{Label: "This computer", Status: CheckInfo, Detail: friendlyOS(runtime.GOOS) + " · " + runtime.GOARCH},
		)
		return finalizeDiagnosticResults(results), usableEngine
	}

	if usableEngine == nil {
		results = append(results,
			CheckResult{Label: "Omnideck instance", Status: CheckInfo, Detail: "Not checked until the container runtime is ready"},
			CheckResult{Label: "Browser", Status: CheckInfo, Detail: "Not checked until the container runtime is ready"},
			CheckResult{Label: "Saved data", Status: CheckInfo, Detail: "Not checked until the container runtime is ready"},
			doctorMemoryCheck(),
			doctorOllamaCheck(nil, "", false),
			CheckResult{Label: "This computer", Status: CheckInfo, Detail: friendlyOS(runtime.GOOS) + " · " + runtime.GOARCH},
		)
		return finalizeDiagnosticResults(results), nil
	}

	homeVolumeResult := CheckVolume("Saved files", cfg.HomeVolumeName(), usableEngine)
	stateVolumeResult := CheckVolume("Saved app data", cfg.StateVolumeName(), usableEngine)
	containerResult, running := doctorContainerCheck(name, cfg, usableEngine)
	if containerResult.Action == DoctorActionRepairInstance &&
		(homeVolumeResult.Status != CheckPass || stateVolumeResult.Status != CheckPass) {
		containerResult.Action = DoctorActionNone
		containerResult.ActionLabel = ""
		containerResult.ActionValue = ""
		containerResult.Hint = "Automatic repair is paused because expected saved data is missing. Do not create replacement volumes; use the reported volume names when asking for support."
	}
	results = append(results, containerResult)
	results = append(results, doctorBrowserCheck(cfg, running))
	// A stopped container keeps its environment, and starting it would not
	// add a missing sign-in address, so check it whenever it exists.
	if running || containerResult.Action == DoctorActionStartInstance {
		results = append(results, doctorExternalURLCheck(name, cfg, usableEngine))
	}
	results = append(results,
		homeVolumeResult,
		stateVolumeResult,
		doctorImageCheck(cfg, usableEngine),
		doctorMemoryCheck(),
		doctorOllamaCheck(usableEngine, cfg.ContainerName, running),
		CheckResult{Label: "This computer", Status: CheckInfo, Detail: friendlyOS(runtime.GOOS) + " · " + runtime.GOARCH},
	)
	return finalizeDiagnosticResults(results), usableEngine
}

// DiagnoseInventoryIssues converts unreadable saved configuration files into
// durable Doctor findings. The original file is never rewritten or removed.
func DiagnoseInventoryIssues(issues []config.InstanceIssue) []CheckResult {
	results := make([]CheckResult, 0, len(issues))
	for _, issue := range issues {
		detail := "The saved instance file " + issue.Name + ".yaml could not be read"
		hint := "Keep the file in place and repair its YAML or restore it from backup: " + issue.Path
		if issue.Err != nil {
			hint += " (" + issue.Err.Error() + ")"
		}
		results = append(results, CheckResult{
			ID:     "config.instance_file." + stableCheckID(issue.Name),
			Label:  "Saved installation: " + issue.Name,
			Status: CheckFail,
			Detail: detail,
			Hint:   hint,
		})
	}
	return results
}

func finalizeDiagnosticResults(results []CheckResult) []CheckResult {
	counts := map[string]int{}
	for i := range results {
		if results[i].ID != "" {
			continue
		}
		base := diagnosticID(results[i].Label)
		counts[base]++
		results[i].ID = base
		if counts[base] > 1 {
			results[i].ID = fmt.Sprintf("%s.%d", base, counts[base])
		}
	}
	return results
}

func diagnosticID(label string) string {
	switch label {
	case "Container runtime":
		return "runtime.podman"
	case "Omnideck setup":
		return "instance.setup"
	case "Omnideck instance":
		return "instance.container"
	case "Browser":
		return "instance.browser"
	case "MCP sign-in address":
		return "instance.external_url"
	case "Saved data":
		return "storage.summary"
	case "Saved files":
		return "storage.home"
	case "Saved app data":
		return "storage.state"
	case "Omnideck download":
		return "instance.image"
	case "Available memory":
		return "host.memory"
	case "Local AI (optional)":
		return "optional.ollama"
	case "This computer":
		return "host.platform"
	default:
		return "doctor." + stableCheckID(label)
	}
}

func stableCheckID(value string) string {
	var b strings.Builder
	lastSeparator := false
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastSeparator = false
		} else if !lastSeparator && b.Len() > 0 {
			b.WriteByte('_')
			lastSeparator = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// CheckVolume reports whether one of an instance's persisted volumes is still
// available without attempting to create or replace it.
func CheckVolume(label, name string, eng engine.Engine) CheckResult {
	if eng == nil {
		return CheckResult{Label: label, Status: CheckInfo, Detail: "Not checked until the container runtime is ready"}
	}
	exists, err := eng.VolumeExists(name)
	if err != nil {
		return CheckResult{Label: label, Status: CheckWarn, Detail: "Could not check " + name, Hint: err.Error()}
	}
	if !exists {
		return CheckResult{
			Label:  label,
			Status: CheckFail,
			Detail: name + " was not found",
			Hint:   "Do not create a replacement if this instance had important data. Use this name when asking for support.",
		}
	}
	return CheckResult{Label: label, Status: CheckPass, Detail: name + " exists"}
}

func doctorRuntimeCheck(eng engine.Engine, probes []engine.ProbeResult) (CheckResult, engine.Engine) {
	if eng != nil && eng.Name() != "podman" {
		eng = nil
	}

	var selected *engine.ProbeResult
	for i := range probes {
		if probes[i].Name == "podman" {
			selected = &probes[i]
			break
		}
	}

	if selected != nil && selected.Ready() {
		if eng == nil || eng.Name() != selected.Name {
			for _, candidate := range engine.ReadyEngines(probes) {
				if candidate.Name() == selected.Name {
					eng = candidate
					break
				}
			}
		}
		detail := runtimeNameForPeople(selected.Name) + " is ready"
		if selected.Version != "" {
			detail += " · " + selected.Version
		}
		return CheckResult{Label: "Container runtime", Status: CheckPass, Detail: detail}, eng
	}

	detail := "Podman is not ready"
	actionLabel := "Set up Podman"
	actionValue := "podman"
	if selected != nil {
		detail = runtimeNameForPeople(selected.Name) + ": " + engine.RuntimeStateLabel(selected.State)
		actionValue = selected.Name
		plans := engine.BuildSetupPlans(probes, engine.DetectHostPlatform())
		if len(plans) > 0 {
			actionLabel = plans[0].Action
		}
	}
	return CheckResult{
		Label:       "Container runtime",
		Status:      CheckFail,
		Detail:      detail,
		Hint:        "Use Omnideck's guided container setup; it will explain each step before anything changes.",
		Action:      DoctorActionRuntimeSetup,
		ActionLabel: actionLabel,
		ActionValue: actionValue,
	}, nil
}

// InstanceCommand formats an omnideck command that targets instance name,
// omitting --name when the instance cannot be selected by name.
func InstanceCommand(command, name string) string {
	if name == "" {
		return "omnideck " + command
	}
	return "omnideck " + command + " --name " + name
}

func doctorContainerCheck(name string, cfg *config.Config, eng engine.Engine) (CheckResult, bool) {
	exists, err := eng.ContainerExists(cfg.ContainerName)
	if err != nil {
		return CheckResult{
			Label:  "Omnideck instance",
			Status: CheckFail,
			Detail: "Omnideck could not check whether " + cfg.ContainerName + " exists",
			Hint:   err.Error(),
		}, false
	}
	if !exists {
		return CheckResult{
			Label:       "Omnideck instance",
			Status:      CheckFail,
			Detail:      "The saved instance " + cfg.ContainerName + " is missing its container",
			Hint:        "Repair recreates the container and reconnects the same saved data volumes.",
			Action:      DoctorActionRepairInstance,
			ActionLabel: "Repair this installation",
			ActionValue: name,
		}, false
	}
	status, err := eng.ContainerStatus(cfg.ContainerName)
	if err != nil {
		return CheckResult{
			Label:  "Omnideck instance",
			Status: CheckFail,
			Detail: "The container exists, but Omnideck could not read its status",
			Hint:   err.Error(),
		}, false
	}
	if status == "running" {
		return CheckResult{Label: "Omnideck instance", Status: CheckPass, Detail: cfg.ContainerName + " is running"}, true
	}
	return CheckResult{
		Label:       "Omnideck instance",
		Status:      CheckFail,
		Detail:      cfg.ContainerName + " is " + friendlyContainerStatus(status),
		Hint:        "Run: " + InstanceCommand("start", name),
		Action:      DoctorActionStartInstance,
		ActionLabel: "Start Omnideck",
		ActionValue: name,
	}, false
}

func friendlyContainerStatus(status string) string {
	switch status {
	case "", "unknown":
		return "not reporting its status"
	case "exited", "stopped", "created":
		return "stopped"
	default:
		return status
	}
}

func doctorBrowserCheck(cfg *config.Config, containerRunning bool) CheckResult {
	address := cfg.BrowserURL()
	if !containerRunning {
		return CheckResult{Label: "Browser", Status: CheckInfo, Detail: "Not checked until Omnideck is running"}
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+cfg.WebUIPortOrDefault(), 2*time.Second)
	if err != nil {
		return CheckResult{
			Label:  "Browser",
			Status: CheckFail,
			Detail: "Omnideck is running, but " + address + " did not respond",
			Hint:   "Wait a moment and run Doctor again. If it still fails, open Logs for the selected instance.",
		}
	}
	_ = conn.Close()
	return CheckResult{Label: "Browser", Status: CheckPass, Detail: address + " is responding"}
}

// doctorExternalURLCheck detects containers created without (or with a
// different) OMNIDECK_EXTERNAL_URL, which silently hides MCP and Slack
// integrations. Start and restart never recreate containers, so Doctor is
// where this drift becomes visible outside of update.
func doctorExternalURLCheck(name string, cfg *config.Config, eng ContainerEnvEngine) CheckResult {
	const label = "MCP sign-in address"
	check, err := CheckExternalURL(eng, cfg)
	if err != nil {
		return CheckResult{Label: label, Status: CheckWarn, Detail: "Could not check the address MCP integrations use to sign in", Hint: err.Error()}
	}
	hint := "Run: " + InstanceCommand("update", name) + " (saved data is kept)"
	switch check.State {
	case ExternalURLOK:
		return CheckResult{Label: label, Status: CheckPass, Detail: "MCP sign-in address is " + check.Expected}
	case ExternalURLMismatch:
		return CheckResult{
			Label:  label,
			Status: CheckWarn,
			Detail: "MCP integrations may fail to sign in: this installation uses " + check.Actual + " instead of " + check.Expected,
			Hint:   hint,
		}
	default:
		return CheckResult{
			Label:  label,
			Status: CheckWarn,
			Detail: "MCP integrations are unavailable: this installation was created without its sign-in address",
			Hint:   hint,
		}
	}
}

func doctorImageCheck(cfg *config.Config, eng engine.Engine) CheckResult {
	digest := eng.ImageDigest(cfg.Image)
	if digest == "" {
		return CheckResult{Label: "Omnideck download", Status: CheckWarn, Detail: "Could not confirm the downloaded app image", Hint: "This does not remove or change anything. Run Doctor again if Omnideck otherwise works."}
	}
	short := strings.TrimPrefix(digest, "sha256:")
	if len(short) > 12 {
		short = short[:12]
	}
	return CheckResult{Label: "Omnideck download", Status: CheckPass, Detail: "Downloaded · " + short}
}

func doctorMemoryCheck() CheckResult {
	mb, err := checks.AvailableMemoryMB()
	if err != nil {
		return CheckResult{Label: "Available memory", Status: CheckInfo, Detail: "Could not read available memory"}
	}
	if warning := checks.MemoryWarning(mb); warning != "" {
		return CheckResult{Label: "Available memory", Status: CheckWarn, Detail: fmt.Sprintf("%d MB available", mb), Hint: warning}
	}
	return CheckResult{Label: "Available memory", Status: CheckPass, Detail: fmt.Sprintf("%d MB available", mb)}
}

func doctorOllamaCheck(eng engine.Engine, containerName string, containerRunning bool) CheckResult {
	status := checks.CheckOllamaStatus()
	if !status.Running {
		return CheckResult{Label: "Local AI (optional)", Status: CheckInfo, Detail: "Not connected · online AI still works", Hint: "You can add Ollama later if you want local AI."}
	}
	if eng == nil || !containerRunning {
		return CheckResult{Label: "Local AI (optional)", Status: CheckInfo, Detail: "Ollama is running on this computer", Hint: "Container access will be checked after Omnideck is running."}
	}
	if err := eng.CheckOllamaConnection(containerName); err != nil {
		hint := "Ollama is optional, so online AI still works. Check Ollama and the container runtime's host networking."
		if runtime.GOOS == "windows" && eng.Name() == "podman" {
			hint = "Quit Ollama. In Windows user environment variables, set OLLAMA_HOST to 0.0.0.0:11434, then open Ollama again. Do not allow public-network access."
		}
		return CheckResult{
			Label:  "Local AI (optional)",
			Status: CheckWarn,
			Detail: "Ollama is running, but Omnideck cannot connect to it",
			Hint:   hint,
		}
	}
	return CheckResult{Label: "Local AI (optional)", Status: CheckPass, Detail: "Omnideck can connect to Ollama"}
}

func runtimeNameForPeople(name string) string {
	switch name {
	case "podman":
		return "Podman"
	default:
		return name
	}
}

func friendlyOS(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}
