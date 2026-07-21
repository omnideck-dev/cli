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
func Diagnose(cfg *config.Config, eng engine.Engine) []CheckResult {
	results, _ := DiagnoseWithProbes(cfg, eng, engine.ProbeAll())
	return results
}

func DiagnoseWithProbes(cfg *config.Config, eng engine.Engine, probes []engine.ProbeResult) ([]CheckResult, engine.Engine) {
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
		return results, usableEngine
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
		return results, nil
	}

	containerResult, running := doctorContainerCheck(cfg, usableEngine)
	results = append(results, containerResult)
	results = append(results, doctorBrowserCheck(cfg, running))
	results = append(results,
		CheckVolume("Saved files", cfg.HomeVolumeName(), usableEngine),
		CheckVolume("Saved app data", cfg.StateVolumeName(), usableEngine),
		doctorImageCheck(cfg, usableEngine),
		doctorMemoryCheck(),
		doctorOllamaCheck(usableEngine, cfg.ContainerName, running),
		CheckResult{Label: "This computer", Status: CheckInfo, Detail: friendlyOS(runtime.GOOS) + " · " + runtime.GOARCH},
	)
	return results, usableEngine
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
	target := ""
	if eng != nil {
		target = eng.Name()
	}

	var selected *engine.ProbeResult
	if target != "" {
		for i := range probes {
			if probes[i].Name == target {
				selected = &probes[i]
				break
			}
		}
	} else {
		for i := range probes {
			if probes[i].Ready() {
				selected = &probes[i]
				break
			}
		}
		if selected == nil {
			plans := engine.BuildSetupPlans(probes, engine.DetectHostPlatform())
			for _, plan := range plans {
				if !plan.Recommended {
					continue
				}
				for i := range probes {
					if probes[i].Name == plan.Runtime {
						selected = &probes[i]
						break
					}
				}
				break
			}
			if selected == nil && len(probes) > 0 {
				selected = &probes[0]
			}
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

	detail := "Podman or Docker is not ready"
	actionLabel := "Set up a container runtime"
	actionValue := target
	if selected != nil {
		detail = runtimeNameForPeople(selected.Name) + ": " + engine.RuntimeStateLabel(selected.State)
		actionValue = selected.Name
		plans := engine.BuildSetupPlans(probes, engine.DetectHostPlatform())
		for _, plan := range plans {
			if plan.Runtime == selected.Name {
				actionLabel = plan.Action
				break
			}
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

func doctorContainerCheck(cfg *config.Config, eng engine.Engine) (CheckResult, bool) {
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
			ActionValue: cfg.ContainerName,
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
		Hint:        "Run: omnideck start --name " + cfg.ContainerName,
		Action:      DoctorActionStartInstance,
		ActionLabel: "Start Omnideck",
		ActionValue: cfg.ContainerName,
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
	address := "http://localhost:" + cfg.WebUIPortOrDefault()
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
	case "docker":
		return "Docker"
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
