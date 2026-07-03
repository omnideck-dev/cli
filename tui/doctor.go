package tui

import (
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// CheckStatus indicates pass/fail for a doctor check.
type CheckStatus int

const (
	CheckPass CheckStatus = iota
	CheckFail
	CheckWarn
)

// CheckResult is the result of a single doctor check.
type CheckResult struct {
	Label  string
	Status CheckStatus
	Detail string
	Hint   string
}

// RunDoctorChecks runs all health checks concurrently and returns results.
func RunDoctorChecks(cfg *config.Config, eng engine.Engine) []CheckResult {
	results := make([]CheckResult, 10)
	var wg sync.WaitGroup

	runCheck := func(i int, fn func() CheckResult) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = fn()
		}()
	}

	// 0: Engine version
	runCheck(0, func() CheckResult {
		if eng == nil {
			return CheckResult{"Container engine", CheckFail, "not found — install Podman or Docker",
				"Install Podman: https://podman.io/docs/installation"}
		}
		return CheckResult{"Container engine", CheckPass, eng.Name() + " " + eng.Version(), ""}
	})

	// 1: Engine socket permissions
	runCheck(1, func() CheckResult {
		if eng == nil {
			return CheckResult{"Engine permissions", CheckFail, "n/a — no engine found", ""}
		}
		if !eng.HasPermission() {
			hint := "sudo usermod -aG " + eng.Name() + " $USER  (then log out and back in)"
			return CheckResult{"Engine permissions", CheckFail, "permission denied", hint}
		}
		return CheckResult{"Engine permissions", CheckPass, "ok", ""}
	})

	// 2: Container exists + status
	runCheck(2, func() CheckResult {
		if cfg == nil {
			return CheckResult{"Container", CheckFail, "no config found",
				"Run: omnideck install"}
		}
		if eng == nil {
			return CheckResult{"Container", CheckWarn, "skipped — no engine found", ""}
		}
		status, err := eng.ContainerStatus(cfg.ContainerName)
		if err != nil {
			return CheckResult{"Container", CheckFail, fmt.Sprintf("'%s' not found", cfg.ContainerName),
				"Run: omnideck install"}
		}
		if status == "running" {
			return CheckResult{"Container", CheckPass, cfg.ContainerName + " — " + status, ""}
		}
		return CheckResult{"Container", CheckWarn, cfg.ContainerName + " — " + status,
			"Run: omnideck start"}
	})

	// 3: Image freshness (compare digest, best-effort)
	runCheck(3, func() CheckResult {
		if cfg == nil || eng == nil {
			return CheckResult{"Image", CheckWarn, "skipped", ""}
		}
		digest := eng.ImageDigest(cfg.Image)
		if digest == "" {
			return CheckResult{"Image", CheckWarn, "could not check digest", ""}
		}
		return CheckResult{"Image", CheckPass, digest[:min(40, len(digest))] + "...", ""}
	})

	// 4: Ollama reachable
	runCheck(4, func() CheckResult {
		ok, host := checks.CheckOllama()
		if !ok {
			return CheckResult{"Ollama", CheckWarn, "not reachable at " + host,
				"Install: https://ollama.com"}
		}
		return CheckResult{"Ollama", CheckPass, "reachable at " + host, ""}
	})

	// 5: Web UI port (Omnideck web UI)
	runCheck(5, func() CheckResult {
		port := "2337"
		if cfg != nil {
			port = cfg.WebUIPortOrDefault()
		}
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
		if err != nil {
			return CheckResult{"Port " + port + " (Web UI)", CheckWarn, "not reachable",
				"Is Omnideck running? Try: omnideck start"}
		}
		conn.Close()
		return CheckResult{"Port " + port + " (Web UI)", CheckPass, "reachable", ""}
	})

	// 6: Home volume exists
	runCheck(6, func() CheckResult {
		if cfg == nil {
			return CheckResult{"Home volume", CheckWarn, "no config", ""}
		}
		return volumeCheck("Home volume", cfg.HomeVolumeName(), eng)
	})

	// 7: State volume exists
	runCheck(7, func() CheckResult {
		if cfg == nil {
			return CheckResult{"State volume", CheckWarn, "no config", ""}
		}
		return volumeCheck("State volume", cfg.StateVolumeName(), eng)
	})

	// 8: Memory
	runCheck(8, func() CheckResult {
		mb, err := checks.AvailableMemoryMB()
		if err != nil {
			return CheckResult{"Memory", CheckWarn, "could not read", ""}
		}
		w := checks.MemoryWarning(mb)
		if w != "" {
			return CheckResult{"Memory", CheckWarn, fmt.Sprintf("%d MB available", mb),
				"Consider freeing memory for best performance"}
		}
		return CheckResult{"Memory", CheckPass, fmt.Sprintf("%d MB available", mb), ""}
	})

	// 9: OS + arch
	runCheck(9, func() CheckResult {
		return CheckResult{"OS / Arch", CheckPass,
			runtime.GOOS + " / " + runtime.GOARCH, ""}
	})

	wg.Wait()
	return results
}

// RenderDoctorReport renders a static report string from check results.
func RenderDoctorReport(results []CheckResult) (string, bool) {
	allPass := true
	out := "\n" + styles.Title.Render("  Omnideck Doctor Report") + "\n"
	out += "  " + styles.Dim.Render("────────────────────────────────────────") + "\n\n"

	for _, r := range results {
		var icon, labelStyle, detailStyle string
		switch r.Status {
		case CheckPass:
			icon = styles.CheckMark
			labelStyle = styles.Success.Render(r.Label)
			detailStyle = styles.Dim.Render(r.Detail)
		case CheckFail:
			icon = styles.CrossMark
			labelStyle = styles.Error.Render(r.Label)
			detailStyle = styles.Error.Render(r.Detail)
			allPass = false
		case CheckWarn:
			icon = styles.Warning.Render("!")
			labelStyle = styles.Warning.Render(r.Label)
			detailStyle = styles.Warning.Render(r.Detail)
		}

		line := fmt.Sprintf("  %s  %-26s %s\n", icon, labelStyle, detailStyle)
		out += line
		if r.Hint != "" {
			out += fmt.Sprintf("       %s\n", styles.Dim.Render("→ "+r.Hint))
		}
	}
	out += "\n"
	return out, allPass
}

func volumeCheck(label, name string, eng engine.Engine) CheckResult {
	if eng == nil {
		return CheckResult{label, CheckWarn, "skipped — no engine found", ""}
	}
	exists, err := eng.VolumeExists(name)
	if err != nil {
		return CheckResult{label, CheckWarn, name + " — could not check", err.Error()}
	}
	if !exists {
		return CheckResult{label, CheckFail, name + " — not found", "Run: omnideck install"}
	}
	return CheckResult{label, CheckPass, name + " — exists", ""}
}
