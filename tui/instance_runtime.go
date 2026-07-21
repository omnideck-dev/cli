package tui

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/engine"
)

// fetchStats calls the engine synchronously and returns an instanceStatsMsg.
func fetchStats(eng engine.Engine, name string, idx int) tea.Msg {
	status, _ := eng.ContainerStatus(name)
	if status == "" {
		status = "unknown"
	}
	cpu, cpuPct, ram, ramTotal, ramPct, _ := eng.ContainerStats(name)
	msg := instanceStatsMsg{
		idx: idx, status: status,
		cpu: cpu, cpuPct: cpuPct,
		ram: ram, ramTotal: ramTotal, ramPct: ramPct,
	}
	if inspect, err := eng.ContainerInspect(name); err == nil {
		msg.health = inspect.HealthStatus
		msg.restarts = strconv.Itoa(inspect.RestartCount)
		if !inspect.CreatedAt.IsZero() {
			msg.created = inspect.CreatedAt.Format("2006-01-02")
		}
		if !inspect.StartedAt.IsZero() && status == "running" {
			msg.uptime = formatDuration(time.Since(inspect.StartedAt))
		}
	}
	return msg
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
