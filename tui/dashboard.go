package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
)

// Screen identifies which view is currently visible.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenLogs
	ScreenSettings    // modal overlay over Dashboard
	ScreenDoctor      // modal overlay over Dashboard
	ScreenSetup       // setup workflow modal
	ScreenMaintenance // update or repair workflow modal
)

// LogLine is one parsed container log entry.
type LogLine struct {
	Time  string // "14:32:01"
	Level string // "INFO" | "WARN" | "ERROR" | "DEBUG" | "READY"
	Msg   string
}

// settingField is one editable row in the settings modal.
type settingField struct {
	Key     string
	Label   string
	Type    string // short input-kind label shown beside the value
	Value   string
	Orig    string // value at modal open time, used to detect changes
	Changed bool
}

type settingsStage int

const (
	settingsStageEditing settingsStage = iota
	settingsStageApplying
)

type doctorStage int

const (
	doctorStageChecking doctorStage = iota
	doctorStageResults
	doctorStageActing
)

// InstanceState holds live runtime data for one managed instance.
type InstanceState struct {
	Info       config.InstanceInfo
	Status     string // "running" | "stopped" | "paused" | "unknown"
	CPU        string // e.g. "12.3%"
	CPUPct     float64
	RAM        string // e.g. "1.24 GiB" (used)
	RAMTotal   string // e.g. "15.6 GiB" (limit)
	RAMPct     float64
	Uptime     string
	Restarts   string
	Created    string
	Health     string
	NetUp      string
	NetDown    string
	Disk       string
	Logs       []LogLine
	CPUHistory []float64 // last 16 samples (0.0–1.0)
	RAMHistory []float64 // last 16 samples (0.0–1.0)
}

// --- internal messages ---

type statsTickMsg time.Time
type toastClearMsg struct{}

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
type doctorResultsMsg struct {
	results []CheckResult
	eng     engine.Engine
}

type doctorActionDoneMsg struct{ err error }

// containerToggleDoneMsg preserves lifecycle failures instead of inferring
// success from a follow-up status poll.
type containerToggleDoneMsg struct {
	stats  instanceStatsMsg
	action string
	err    error
}

// logCopyResultMsg is returned after a clipboard copy attempt.
type logCopyResultMsg struct{ err error }

// logCopyClearMsg clears the "Copied!" flash after a delay.
type logCopyClearMsg struct{}

// WorkflowExitMsg is emitted when an embedded workflow returns to the dashboard.
type WorkflowExitMsg struct{}

// settingsApplyDoneMsg is returned after settings recreate and persist a container.
type settingsApplyDoneMsg struct {
	err error
	cfg *config.Config
	idx int
}

// instancesRefreshedMsg carries either a fresh instance list or the read error.
// A refresh failure must never look like every instance was deleted.
type instancesRefreshedMsg struct {
	instances []config.InstanceInfo
	err       error
}

// DashboardModel is the top-level Bubble Tea model for the v2 dashboard TUI.
type DashboardModel struct {
	width, height int
	screen        Screen

	eng       engine.Engine
	instances []InstanceState
	selected  int // index into instances

	// Card state
	expanded  map[string]bool // per-card expand flag keyed by instance name
	chipFocus int             // -1 = none focused, 0–3 = focused chip index

	// Toast notification
	toast string

	// Doctor modal
	doctorResults []CheckResult
	doctorStage   doctorStage
	doctorSpinner spinner.Model
	doctorFocus   int
	doctorMessage string

	// Logs screen
	logScroll      int
	logSearchMode  bool
	logSearchQuery string
	logCopied      bool

	// Settings modal
	settingFields   []settingField
	settingFocus    int
	settingEditing  bool
	settingBuffer   string // in-progress edit buffer
	settingsStage   settingsStage
	settingsMessage string

	// Embedded workflow models (active when screen == ScreenSetup/ScreenMaintenance).
	setupModel       SetupModel
	maintenanceModel MaintenanceModel
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
	return DashboardModel{
		eng:           eng,
		instances:     states,
		doctorSpinner: sp,
		expanded:      make(map[string]bool),
		chipFocus:     -1,
	}
}

// NewDashboardModelForDoctor opens Doctor for a selected broken installation.
func NewDashboardModelForDoctor(eng engine.Engine, instances []config.InstanceInfo, selected int) DashboardModel {
	m := NewDashboardModel(eng, instances)
	if selected >= 0 && selected < len(instances) {
		m.selected = selected
	}
	m.screen = ScreenDoctor
	m.doctorStage = doctorStageChecking
	return m
}

// NewDashboardModelForSetup creates a dashboard that opens directly in setup.
func NewDashboardModelForSetup(eng engine.Engine, instances []config.InstanceInfo, imageOverride, preferredEngine string) DashboardModel {
	m := NewDashboardModel(eng, instances)
	cfg := workflow.NewInstanceDefaults(instances)
	mode := SetupAdditionalInstance
	if len(instances) == 0 {
		mode = SetupFirstRun
	}
	im := NewSetupModel(SetupRequest{
		Initial:           cfg,
		ImageOverride:     imageOverride,
		ExistingInstances: instances,
		Mode:              mode,
		PreferredEngine:   preferredEngine,
		Embedded:          true,
	})
	m.setupModel = im
	m.screen = ScreenSetup
	return m
}

// NewDashboardModelForRuntimeSetup opens container runtime setup for existing
// installations and returns to the dashboard as soon as a runtime is ready.
func NewDashboardModelForRuntimeSetup(instances []config.InstanceInfo, preferredEngine string) DashboardModel {
	m := NewDashboardModel(nil, instances)
	cfg := config.DefaultConfig()
	if len(instances) > 0 && instances[0].Config != nil {
		cfg = instances[0].Config
	}
	im := NewSetupModel(SetupRequest{
		Initial:           cfg,
		ExistingInstances: instances,
		Mode:              SetupRuntimeRepair,
		PreferredEngine:   preferredEngine,
		Embedded:          true,
	})
	m.setupModel = im
	m.screen = ScreenSetup
	return m
}

// NewDashboardModelForUpdate opens Maintenance in update mode.
func NewDashboardModelForUpdate(eng engine.Engine, instances []config.InstanceInfo, cfg *config.Config, selectedIdx int) DashboardModel {
	m := NewDashboardModel(eng, instances)
	m.selected = selectedIdx
	configPath := ""
	if selectedIdx >= 0 && selectedIdx < len(instances) {
		configPath = instances[selectedIdx].Path
	}
	um := NewMaintenanceModel(MaintenanceRequest{Config: cfg, ConfigPath: configPath, Engine: eng, Embedded: true})
	m.maintenanceModel = um
	m.screen = ScreenMaintenance
	return m
}

// Init starts live polling for all instance stats.
func (m DashboardModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return statsTickMsg(t) }),
		m.doctorSpinner.Tick,
	}
	for i := range m.instances {
		cmds = append(cmds, m.pollStats(i))
	}
	if m.screen == ScreenSetup {
		cmds = append(cmds, m.setupModel.Init())
	}
	if m.screen == ScreenMaintenance {
		cmds = append(cmds, m.maintenanceModel.Init())
	}
	if m.screen == ScreenDoctor {
		cmds = append(cmds, m.runDoctorCmd())
	}
	return tea.Batch(cmds...)
}

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
		results, usableEngine := diagnoseDoctorWithProbes(cfg, eng, engine.ProbeAll())
		return doctorResultsMsg{results: results, eng: usableEngine}
	}
}

// buildSettingFields populates m.settingFields from the current instance's config.
func (m *DashboardModel) buildSettingFields() {
	inst := m.CurrentInstance()
	if inst == nil {
		m.settingFields = nil
		return
	}
	cfg := inst.Info.Config
	m.settingFields = []settingField{
		{Key: "home_volume", Label: "File storage", Type: "name", Value: cfg.HomeVolumeName(), Orig: cfg.HomeVolumeName()},
		{Key: "state_volume", Label: "App storage", Type: "name", Value: cfg.StateVolumeName(), Orig: cfg.StateVolumeName()},
		{Key: "memory", Label: "Memory limit", Type: "size", Value: cfg.Memory, Orig: cfg.Memory},
		{Key: "shm_size", Label: "Shared memory", Type: "size", Value: cfg.ShmSize, Orig: cfg.ShmSize},
		{Key: "web_ui_port", Label: "Browser port", Type: "number", Value: cfg.WebUIPortOrDefault(), Orig: cfg.WebUIPortOrDefault()},
		{Key: "image", Label: "Container image", Type: "advanced", Value: cfg.Image, Orig: cfg.Image},
	}
	m.settingFocus = 0
	m.settingsStage = settingsStageEditing
	m.settingsMessage = ""
}

// configFromSettingFields returns a candidate configuration without mutating or
// persisting the configuration that describes the currently running container.
func (m *DashboardModel) configFromSettingFields() *config.Config {
	inst := m.CurrentInstance()
	if inst == nil {
		return nil
	}
	candidate := *inst.Info.Config
	for _, f := range m.settingFields {
		if !f.Changed {
			continue
		}
		_ = workflow.ApplySetting(&candidate, f.Key, f.Value)
	}
	return &candidate
}

func (m *DashboardModel) validateSettingFields() error {
	inst := m.CurrentInstance()
	if inst == nil || inst.Info.Config == nil {
		return nil
	}
	oldPort := inst.Info.Config.WebUIPortOrDefault()
	candidate := *inst.Info.Config
	for _, field := range m.settingFields {
		if !field.Changed {
			continue
		}
		if err := workflow.ApplySetting(&candidate, field.Key, field.Value); err != nil {
			return err
		}
		switch field.Key {
		case "web_ui_port":
			if !checks.ValidPort(field.Value) {
				return fmt.Errorf("browser address number must be between 1 and 65535")
			}
			for i, other := range m.instances {
				if i != m.selected && other.Info.Config != nil && other.Info.Config.WebUIPortOrDefault() == field.Value {
					return fmt.Errorf("another Omnideck installation already uses browser address number %s", field.Value)
				}
			}
			if field.Value != oldPort && !checks.PortAvailable(field.Value) {
				return fmt.Errorf("another app is already using browser address number %s", field.Value)
			}
		}
	}
	return nil
}

// startEmbeddedSetup launches setup as an embedded workflow.
func (m DashboardModel) startEmbeddedSetup() (DashboardModel, tea.Cmd) {
	return m.startEmbeddedSetupWithRuntime("")
}

func (m DashboardModel) startEmbeddedSetupWithRuntime(requestedRuntime string) (DashboardModel, tea.Cmd) {
	instances := make([]config.InstanceInfo, len(m.instances))
	for i := range m.instances {
		instances[i] = m.instances[i].Info
	}
	cfg := workflow.NewInstanceDefaults(instances)
	mode := SetupAdditionalInstance
	if len(m.instances) == 0 {
		mode = SetupFirstRun
	}
	preferredEngine := requestedRuntime
	if preferredEngine == "" && m.eng != nil {
		preferredEngine = m.eng.Name()
	}
	im := NewSetupModel(SetupRequest{
		Initial:           cfg,
		ExistingInstances: instances,
		Mode:              mode,
		PreferredEngine:   preferredEngine,
		Embedded:          true,
		WindowWidth:       m.width,
		WindowHeight:      m.height,
	})
	m.setupModel = im
	m.screen = ScreenSetup
	return m, im.Init()
}

// startEmbeddedUpdate launches Maintenance in update mode.
func (m DashboardModel) startEmbeddedUpdate() (DashboardModel, tea.Cmd) {
	return m.startEmbeddedMaintenance(MaintenanceUpdate)
}

func (m DashboardModel) startEmbeddedRepair() (DashboardModel, tea.Cmd) {
	return m.startEmbeddedMaintenance(MaintenanceRepair)
}

func (m DashboardModel) startEmbeddedMaintenance(mode MaintenanceMode) (DashboardModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil {
		return m, nil
	}
	um := NewMaintenanceModel(MaintenanceRequest{
		Config:     inst.Info.Config,
		ConfigPath: inst.Info.Path,
		Engine:     m.eng,
		Embedded:   true,
		Mode:       mode,
	})
	um.WindowWidth = m.width
	um.WindowHeight = m.height
	m.maintenanceModel = um
	m.screen = ScreenMaintenance
	return m, um.Init()
}

// reloadInstancesCmd fetches the current instance list and returns instancesRefreshedMsg.
func reloadInstancesCmd() tea.Cmd {
	return func() tea.Msg {
		instances, err := config.ListInstances()
		return instancesRefreshedMsg{instances: instances, err: err}
	}
}

// clearToastCmd fires toastClearMsg after ~1.7 seconds.
func clearToastCmd() tea.Cmd {
	return tea.Tick(1700*time.Millisecond, func(time.Time) tea.Msg { return toastClearMsg{} })
}

// applySettingsCmd recreates the container first, then saves the candidate settings.
// If either operation fails it restores the previous runtime/settings pairing.
func applySettingsCmd(current, candidate *config.Config, configPath string, eng engine.Engine, idx int) tea.Cmd {
	return func() tea.Msg {
		currentCopy := *current
		candidateCopy := *candidate
		if candidateCopy.WebUIPortOrDefault() != currentCopy.WebUIPortOrDefault() && !checks.PortAvailable(candidateCopy.WebUIPortOrDefault()) {
			return settingsApplyDoneMsg{err: fmt.Errorf("another app is already using browser address number %s", candidateCopy.WebUIPortOrDefault()), idx: idx}
		}
		if err := workflow.RecreateAndSave(eng, &currentCopy, &candidateCopy, configPath); err != nil {
			return settingsApplyDoneMsg{err: err, idx: idx}
		}
		return settingsApplyDoneMsg{cfg: &candidateCopy, idx: idx}
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

	// Docker/Podman --timestamps prefix: RFC3339Nano timestamp followed by a space.
	// Podman emits 35-char timestamps like "2026-07-10T18:11:11.207455000-04:00 <rest>".
	// We look for the first space within the first 40 chars and attempt a parse.
	if len(line) > 20 && line[10] == 'T' {
		if idx := strings.IndexByte(line, ' '); idx > 0 && idx <= 40 {
			if t, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
				ts = t.Format("01-02-06 3:04 PM")
				rest = strings.TrimSpace(line[idx+1:])
			}
		}
	}

	// Level is the first word of the trimmed remainder when it is a known keyword.
	trimmed := strings.TrimSpace(rest)
	level := ""
	msg := trimmed
	for _, lvl := range []string{"ERROR", "WARN", "INFO", "READY", "DEBUG"} {
		if strings.HasPrefix(trimmed, lvl) {
			level = lvl
			msg = strings.TrimSpace(trimmed[len(lvl):])
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
