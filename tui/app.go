package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
)

// LogLine is one parsed container log entry.
type LogLine struct {
	Time  string // "14:32:01"
	Level string // "INFO" | "WARN" | "ERROR" | "DEBUG" | "READY"
	Msg   string
}

// settingField is one editable row on the Settings screen.
type settingField struct {
	Key     string
	Label   string
	Type    string // short input-kind label shown beside the value
	Value   string
	Orig    string // value when the screen opened, used to detect changes
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

// DashboardScreenState owns interaction state used only by the root screen.
type DashboardScreenState struct {
	expanded  map[string]bool
	chipFocus int
}

// LogsScreenState owns navigation and filtering state used only by Logs.
type LogsScreenState struct {
	logScroll      int
	logSearchMode  bool
	logSearchQuery string
	logCopied      bool
}

// SettingsScreenState owns the editable candidate shown by Settings.
type SettingsScreenState struct {
	settingFields   []settingField
	settingFocus    int
	settingEditing  bool
	settingBuffer   string
	settingsStage   settingsStage
	settingsMessage string
}

// DoctorScreenState owns health-check presentation and action selection.
type DoctorScreenState struct {
	doctorResults []CheckResult
	doctorStage   doctorStage
	doctorSpinner spinner.Model
	doctorFocus   int
	doctorMessage string
}

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

type WorkflowOutcome int

const (
	WorkflowCanceled WorkflowOutcome = iota
	WorkflowCompleted
)

// WorkflowExitMsg asks the application shell to leave an embedded workflow.
// The outcome lets a root-level cancel quit while successful first setup can
// continue to the Dashboard.
type WorkflowExitMsg struct {
	Outcome WorkflowOutcome
}

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

// AppModel is the top-level Bubble Tea application shell. It owns shared
// instance state and delegates interaction/rendering to the active route.
type AppModel struct {
	width, height int
	router        Router
	dialog        *ConfirmDialog

	DashboardScreenState
	LogsScreenState
	SettingsScreenState
	DoctorScreenState

	eng       engine.Engine
	instances []InstanceState
	selected  int // index into instances

	// Toast notification
	toast string

	// Workflow screen models.
	setupModel       SetupModel
	maintenanceModel MaintenanceModel
}

// CurrentInstance returns a pointer to the selected instance, or nil.
func (m *AppModel) CurrentInstance() *InstanceState {
	if len(m.instances) == 0 || m.selected < 0 || m.selected >= len(m.instances) {
		return nil
	}
	return &m.instances[m.selected]
}

// NewAppModel creates an application shell rooted at the Dashboard screen.
func NewAppModel(eng engine.Engine, instances []config.InstanceInfo) AppModel {
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
	return AppModel{
		eng:       eng,
		instances: states,
		DoctorScreenState: DoctorScreenState{
			doctorSpinner: sp,
		},
		DashboardScreenState: DashboardScreenState{
			expanded:  make(map[string]bool),
			chipFocus: -1,
		},
		router: NewRouter(RouteDashboard),
	}
}

// NewAppModelForDoctor opens Doctor for a selected broken installation.
func NewAppModelForDoctor(eng engine.Engine, instances []config.InstanceInfo, selected int) AppModel {
	m := NewAppModel(eng, instances)
	if selected >= 0 && selected < len(instances) {
		m.selected = selected
	}
	m.router.Push(RouteDoctor)
	m.doctorStage = doctorStageChecking
	return m
}

// NewAppModelForSetup creates an application that opens directly in Setup.
func NewAppModelForSetup(eng engine.Engine, instances []config.InstanceInfo, imageOverride, preferredEngine string) AppModel {
	m := NewAppModel(eng, instances)
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
	m.router.Replace(RouteSetup)
	return m
}

// NewAppModelForRuntimeSetup opens container runtime setup for existing
// installations and returns to the dashboard as soon as a runtime is ready.
func NewAppModelForRuntimeSetup(instances []config.InstanceInfo, preferredEngine string) AppModel {
	m := NewAppModel(nil, instances)
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
	m.router.Replace(RouteSetup)
	return m
}

// NewAppModelForUpdate opens Maintenance in update mode.
func NewAppModelForUpdate(eng engine.Engine, instances []config.InstanceInfo, cfg *config.Config, selectedIdx int) AppModel {
	m := NewAppModel(eng, instances)
	m.selected = selectedIdx
	configPath := ""
	if selectedIdx >= 0 && selectedIdx < len(instances) {
		configPath = instances[selectedIdx].Path
	}
	um := NewMaintenanceModel(MaintenanceRequest{Config: cfg, ConfigPath: configPath, Engine: eng, Embedded: true})
	m.maintenanceModel = um
	m.router.Replace(RouteMaintenance)
	return m
}

// Init starts live polling for all instance stats.
func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return statsTickMsg(t) }),
	}
	for i := range m.instances {
		cmds = append(cmds, m.pollStats(i))
	}
	if m.router.Current() == RouteSetup {
		cmds = append(cmds, m.setupModel.Init())
	}
	if m.router.Current() == RouteMaintenance {
		cmds = append(cmds, m.maintenanceModel.Init())
	}
	if m.router.Current() == RouteDoctor {
		cmds = append(cmds, m.doctorSpinner.Tick, m.runDoctorCmd())
	}
	return tea.Batch(cmds...)
}
