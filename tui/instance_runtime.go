package tui

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

// fetchStats calls the engine synchronously and returns an instanceStatsMsg.
//
// CPU/RAM are only fetched while the container is in an active state (see
// workflow.IsActiveContainerStatus): Podman reports 0%/0B for
// a stopped container without erroring, which would otherwise render as a
// misleadingly precise "0.00%" instead of the dash a stopped instance should
// show. A paused or restarting container is not stopped, though, and still
// returns real, non-error stats — skipping those too would make a paused
// instance indistinguishable from a stopped one. When the container is
// active but the stats call itself fails (e.g. a stale network namespace
// after the host restarts out from under a long-lived container),
// statsUnavailable is set instead of silently leaving the same blank fields
// a "not polled yet" state would show.
func fetchStats(eng engine.Engine, name string, idx int) tea.Msg {
	status, _ := eng.ContainerStatus(name)
	if status == "" {
		status = "unknown"
	}
	msg := instanceStatsMsg{idx: idx, status: status}
	active := workflow.IsActiveContainerStatus(status)
	if active {
		cpu, cpuPct, ram, ramTotal, ramPct, err := eng.ContainerStats(name)
		if err != nil {
			msg.statsUnavailable = true
		} else {
			msg.cpu, msg.cpuPct = cpu, cpuPct
			msg.ram, msg.ramTotal, msg.ramPct = ram, ramTotal, ramPct
			msg.sampleOK = true
		}
	}
	if inspect, err := eng.ContainerInspect(name); err == nil {
		msg.health = inspect.HealthStatus
		msg.restarts = strconv.Itoa(inspect.RestartCount)
		if !inspect.CreatedAt.IsZero() {
			msg.created = inspect.CreatedAt.Format("2006-01-02")
		}
		if !inspect.StartedAt.IsZero() && active {
			msg.uptime = formatDuration(time.Since(inspect.StartedAt))
		}
	}
	return msg
}

// applyInstanceStats copies a fetched instanceStatsMsg onto inst, shared by
// the periodic poll (instanceStatsMsg) and the immediate post-toggle refresh
// (containerToggleDoneMsg) so both apply the same running/unavailable/history
// rules instead of drifting apart.
func applyInstanceStats(inst *InstanceState, stats instanceStatsMsg) {
	inst.Status = stats.status
	inst.CPU = stats.cpu
	inst.CPUPct = stats.cpuPct
	inst.RAM = stats.ram
	inst.RAMTotal = stats.ramTotal
	inst.RAMPct = stats.ramPct
	inst.StatsUnavailable = stats.statsUnavailable
	if stats.uptime != "" {
		inst.Uptime = stats.uptime
	}
	if stats.restarts != "" {
		inst.Restarts = stats.restarts
	}
	if stats.created != "" {
		inst.Created = stats.created
	}
	if stats.health != "" {
		inst.Health = stats.health
	}
	// Only a genuine fresh running-and-succeeded sample is worth recording —
	// a stopped or stats-unavailable poll must not push a fake 0.0 into the
	// sparkline history.
	if stats.sampleOK {
		inst.CPUHistory = pushHistory(inst.CPUHistory, stats.cpuPct)
		inst.RAMHistory = pushHistory(inst.RAMHistory, stats.ramPct)
	}
}

// pollStats returns a command that fetches status+stats for instance idx.
func (m AppModel) pollStats(idx int) tea.Cmd {
	if m.eng == nil || idx < 0 || idx >= len(m.instances) {
		return nil
	}
	name := m.instances[idx].Info.Config.ContainerName
	eng := m.eng
	return func() tea.Msg {
		return fetchStats(eng, name, idx)
	}
}

// pushHistory appends val to hist and trims to the last 16 samples.
func pushHistory(hist []float64, val float64) []float64 {
	hist = append(hist, val)
	if len(hist) > 16 {
		hist = hist[len(hist)-16:]
	}
	return hist
}

// formatDuration formats a duration as a human-readable uptime string.
func formatDuration(d time.Duration) string {
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
