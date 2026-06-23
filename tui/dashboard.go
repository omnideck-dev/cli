package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// Screen identifies which view is currently visible.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenDetail
	ScreenLogs
	ScreenConfig   // modal overlay over Detail
	ScreenDoctor   // modal overlay over Dashboard/Detail
	ScreenInstall  // embedded install wizard
	ScreenUpdate   // embedded update wizard
)

// LogLine is one parsed container log entry.
type LogLine struct {
	Time  string // "14:32:01"
	Level string // "INFO" | "WARN" | "ERROR" | "DEBUG" | "READY"
	Msg   string
}

// cfgField is one editable row in the config modal.
type cfgField struct {
	Key     string
	Type    string // "string" | "path" | "memory" | "port"
	Value   string
	Orig    string // value at modal open time, used to detect changes
	Changed bool
}

// InstanceState holds live runtime data for one managed instance.
type InstanceState struct {
	Info     config.InstanceInfo
	Status   string // "running" | "stopped" | "paused" | "unknown"
	CPU      string // e.g. "12.3%"
	CPUPct   float64
	RAM      string // e.g. "1.24 GiB" (used)
	RAMTotal string // e.g. "15.6 GiB" (limit)
	RAMPct   float64
	Uptime   string // e.g. "2d 3h"
	Restarts string
	Created  string
	Health   string
	NetUp    string // e.g. "1.2 KB"
	NetDown  string // e.g. "3.4 KB"
	Disk     string // e.g. "2.1 GB"
	Logs     []LogLine // last N lines cached for detail/logs screens
}

// --- internal messages ---

type clockTickMsg time.Time
type statsTickMsg time.Time

type instanceStatsMsg struct {
	idx      int
	status   string
	cpu      string
	cpuPct   float64
	ram      string
	ramTotal string
	ramPct   float64
	uptime   string
	restarts string
	created  string
	health   string
}

type instanceLogsMsg struct {
	idx   int
	lines []LogLine
}
type doctorResultsMsg []CheckResult

// containerToggleDoneMsg is returned by toggleContainer so the handler can
// clear the busy flag separately from a normal stats poll.
type containerToggleDoneMsg instanceStatsMsg

// logCopyResultMsg is returned after a clipboard copy attempt.
type logCopyResultMsg struct{ err error }

// logCopyClearMsg clears the "Copied!" flash after a delay.
type logCopyClearMsg struct{}

// WizardExitMsg is emitted by an embedded install/update wizard when done.
type WizardExitMsg struct{}

// instancesRefreshedMsg carries a fresh list of instances (e.g. after install).
type instancesRefreshedMsg []config.InstanceInfo

// DashboardModel is the top-level Bubble Tea model for the v2 dashboard TUI.
// It manages all screens and embeds sub-models for install/update/doctor.
type DashboardModel struct {
	width, height int
	screen        Screen
	prevScreen    Screen

	eng       engine.Engine
	instances []InstanceState
	selected  int // index into instances

	clock string

	// Doctor modal
	doctorResults []CheckResult
	doctorDone    bool
	doctorSpinner spinner.Model

	// Logs screen
	logScroll      int
	logSearchMode  bool
	logSearchQuery string
	logCopied      bool

	// Detail screen menu
	detailMenuIdx    int
	detailBusy       bool         // true while start/stop is in flight
	detailBusyAction string       // "Stopping" or "Starting", set at trigger time
	detailSpinner    spinner.Model

	// Config modal
	cfgFields  []cfgField
	cfgFocus   int
	cfgEditing bool
	cfgBuf     string // in-progress edit buffer

	// Embedded wizard sub-models (active when screen == ScreenInstall/ScreenUpdate).
	installModel InstallModel
	updateModel  UpdateModel
}

// CurrentInstance returns a pointer to the selected instance, or nil.
func (m *DashboardModel) CurrentInstance() *InstanceState {
	if len(m.instances) == 0 || m.selected < 0 || m.selected >= len(m.instances) {
		return nil
	}
	return &m.instances[m.selected]
}

// NewDashboardModel creates a ready-to-run dashboard model.
func NewDashboardModel(eng engine.Engine, instances []config.InstanceInfo) DashboardModel {
	states := make([]InstanceState, len(instances))
	for i, inst := range instances {
		states[i] = InstanceState{
			Info:   inst,
			Status: "unknown",
		}
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(styles.TNBlue)
	dsp := spinner.New()
	dsp.Spinner = spinner.Dot
	dsp.Style = lipgloss.NewStyle().Foreground(styles.TNBlue)
	return DashboardModel{
		eng:           eng,
		instances:     states,
		clock:         time.Now().Format("15:04:05"),
		doctorSpinner: sp,
		detailSpinner: dsp,
	}
}

// NewDashboardModelForInstall creates a dashboard that opens directly on the install wizard screen.
func NewDashboardModelForInstall(eng engine.Engine, instances []config.InstanceInfo, imageOverride string) DashboardModel {
	m := NewDashboardModel(eng, instances)
	cfg := suggestInstallDefaults()
	im := NewInstallModel(config.InstancePath(cfg.ContainerName), cfg, imageOverride)
	im.Embedded = true
	m.installModel = im
	m.screen = ScreenInstall
	return m
}

// NewDashboardModelForUpdate creates a dashboard that opens directly on the update wizard screen.
func NewDashboardModelForUpdate(eng engine.Engine, instances []config.InstanceInfo, cfg *config.Config, selectedIdx int) DashboardModel {
	m := NewDashboardModel(eng, instances)
	m.selected = selectedIdx
	um := NewUpdateModel(cfg, eng)
	um.Embedded = true
	m.updateModel = um
	m.screen = ScreenUpdate
	return m
}

// Init starts the clock ticker and immediately polls all instance stats.
func (m DashboardModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return clockTickMsg(t) }),
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return statsTickMsg(t) }),
		m.doctorSpinner.Tick,
	}
	for i := range m.instances {
		cmds = append(cmds, m.pollStats(i))
	}
	if m.screen == ScreenInstall {
		cmds = append(cmds, m.installModel.Init())
	}
	if m.screen == ScreenUpdate {
		cmds = append(cmds, m.updateModel.Init())
	}
	return tea.Batch(cmds...)
}

// fetchStats calls the engine synchronously and returns an instanceStatsMsg.
// Used by both pollStats and toggleContainer to avoid duplicating logic.
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
func (m DashboardModel) pollStats(idx int) tea.Cmd {
	if m.eng == nil || idx < 0 || idx >= len(m.instances) {
		return nil
	}
	name := m.instances[idx].Info.Config.ContainerName
	eng := m.eng
	return func() tea.Msg {
		return fetchStats(eng, name, idx)
	}
}

// pollLogs returns a command that fetches recent logs for instance idx.
func (m DashboardModel) pollLogs(idx int) tea.Cmd {
	if m.eng == nil || idx < 0 || idx >= len(m.instances) {
		return nil
	}
	name := m.instances[idx].Info.Config.ContainerName
	eng := m.eng
	return func() tea.Msg {
		raw, err := eng.FetchLogs(name, 200)
		if err != nil {
			return instanceLogsMsg{idx: idx}
		}
		return instanceLogsMsg{idx: idx, lines: parseLogLines(raw)}
	}
}

// filteredLogs returns the logs for the current instance, filtered by logSearchQuery.
func (m DashboardModel) filteredLogs() []LogLine {
	inst := m.CurrentInstance()
	if inst == nil {
		return nil
	}
	if m.logSearchQuery == "" {
		return inst.Logs
	}
	q := strings.ToLower(m.logSearchQuery)
	var out []LogLine
	for _, ll := range inst.Logs {
		if strings.Contains(strings.ToLower(ll.Time+" "+ll.Level+" "+ll.Msg), q) {
			out = append(out, ll)
		}
	}
	return out
}

// copyLogsCmd returns a command that copies the last 200 (filtered) log lines to the clipboard.
func (m DashboardModel) copyLogsCmd() tea.Cmd {
	lines := m.filteredLogs()
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	var sb strings.Builder
	for _, ll := range lines {
		if ll.Time != "" {
			sb.WriteString(ll.Time + "  ")
		}
		if ll.Level != "" {
			sb.WriteString("[" + ll.Level + "]  ")
		}
		sb.WriteString(ll.Msg + "\n")
	}
	text := sb.String()
	return func() tea.Msg {
		return logCopyResultMsg{err: copyToClipboard(text)}
	}
}

// runDoctorCmd runs doctor checks for the current instance.
func (m DashboardModel) runDoctorCmd() tea.Cmd {
	var cfg *config.Config
	if inst := m.CurrentInstance(); inst != nil {
		cfg = inst.Info.Config
	}
	eng := m.eng
	return func() tea.Msg {
		return doctorResultsMsg(RunDoctorChecks(cfg, eng))
	}
}

// buildCfgFields populates m.cfgFields from the current instance's config.
func (m *DashboardModel) buildCfgFields() {
	inst := m.CurrentInstance()
	if inst == nil {
		m.cfgFields = nil
		return
	}
	cfg := inst.Info.Config
	m.cfgFields = []cfgField{
		{Key: "container_name", Type: "string", Value: cfg.ContainerName, Orig: cfg.ContainerName},
		{Key: "shared_dir", Type: "path", Value: cfg.SharedDir, Orig: cfg.SharedDir},
		{Key: "memory", Type: "memory", Value: cfg.Memory, Orig: cfg.Memory},
		{Key: "shm_size", Type: "memory", Value: cfg.ShmSize, Orig: cfg.ShmSize},
		{Key: "web_ui_port", Type: "port", Value: cfg.WebUIPortOrDefault(), Orig: cfg.WebUIPortOrDefault()},
		{Key: "image", Type: "string", Value: cfg.Image, Orig: cfg.Image},
	}
	m.cfgFocus = 0
}

// saveCfgFields writes changed fields back to the instance config on disk.
func (m *DashboardModel) saveCfgFields() error {
	inst := m.CurrentInstance()
	if inst == nil {
		return nil
	}
	cfg := inst.Info.Config
	for _, f := range m.cfgFields {
		if !f.Changed {
			continue
		}
		switch f.Key {
		case "container_name":
			cfg.ContainerName = f.Value
		case "shared_dir":
			cfg.SharedDir = f.Value
		case "memory":
			cfg.Memory = f.Value
		case "shm_size":
			cfg.ShmSize = f.Value
		case "web_ui_port":
			cfg.WebUIPort = f.Value
		case "image":
			cfg.Image = f.Value
		}
	}
	return config.Save(inst.Info.Path, cfg)
}

// startEmbeddedInstall sets up and launches the install wizard as an embedded screen.
func (m DashboardModel) startEmbeddedInstall() (DashboardModel, tea.Cmd) {
	cfg := suggestInstallDefaults()
	im := NewInstallModel(config.InstancePath(cfg.ContainerName), cfg, "")
	im.Embedded = true
	im.WindowWidth = m.width
	im.WindowHeight = m.height
	m.installModel = im
	m.screen = ScreenInstall
	return m, im.Init()
}

// startEmbeddedUpdate sets up and launches the update wizard as an embedded screen.
func (m DashboardModel) startEmbeddedUpdate() (DashboardModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil {
		return m, nil
	}
	um := NewUpdateModel(inst.Info.Config, m.eng)
	um.Embedded = true
	um.WindowWidth = m.width
	um.WindowHeight = m.height
	m.updateModel = um
	m.screen = ScreenUpdate
	return m, um.Init()
}

// reloadInstancesCmd fetches the current instance list and returns instancesRefreshedMsg.
func reloadInstancesCmd() tea.Cmd {
	return func() tea.Msg {
		instances, _ := config.ListInstances()
		return instancesRefreshedMsg(instances)
	}
}

// suggestInstallDefaults builds a Config pre-filled with a unique name/port/dirs.
func suggestInstallDefaults() *config.Config {
	instances, _ := config.ListInstances()
	takenNames := map[string]bool{}
	maxPort := 2337
	for _, inst := range instances {
		if inst.Config == nil {
			continue
		}
		takenNames[inst.Config.ContainerName] = true
		if p, err := strconv.Atoi(inst.Config.WebUIPortOrDefault()); err == nil && p >= maxPort {
			maxPort = p
		}
	}
	name := "omnideck2"
	for i := 2; takenNames[name]; i++ {
		name = fmt.Sprintf("omnideck%d", i)
	}
	suffix := name[len("omnideck"):]
	dirBase := "Omnideck" + suffix
	home, _ := os.UserHomeDir()
	d := config.DefaultConfig()
	d.ContainerName = name
	d.SharedDir = filepath.Join(home, dirBase)
	d.StateDir = filepath.Join(d.SharedDir, ".state")
	d.WebUIPort = strconv.Itoa(maxPort + 1)
	return d
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

// --- Log parsing ---

func parseLogLines(raw []string) []LogLine {
	out := make([]LogLine, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, parseLogLine(line))
	}
	return out
}

func parseLogLine(line string) LogLine {
	ts := ""
	rest := line

	// Docker/Podman --timestamps prefix: 2024-01-01T14:32:01.123456789Z <rest>
	if len(line) > 30 && line[10] == 'T' {
		if idx := strings.IndexByte(line, ' '); idx > 0 && idx < 35 {
			raw := line[:idx]
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				ts = t.Format("15:04:05")
			} else {
				ts = raw
			}
			rest = strings.TrimSpace(line[idx+1:])
		}
	}

	level := ""
	msg := rest
	for _, lvl := range []string{"ERROR", "WARN", "INFO", "READY", "DEBUG"} {
		if idx := strings.Index(rest, lvl); idx >= 0 {
			level = lvl
			msg = strings.TrimSpace(rest[idx+len(lvl):])
			break
		}
	}

	return LogLine{Time: ts, Level: level, Msg: msg}
}

// tnTruncate truncates a string to at most n runes, adding "…" if needed.
func tnTruncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}
