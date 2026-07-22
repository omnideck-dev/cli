package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// --- QuickCheck result messages ---

type engineCheckResult struct {
	eng    engine.Engine
	all    []engine.Engine
	probes []engine.ProbeResult
	err    error
}

type runtimeCommandDone struct{ err error }

type permissionCheckResult struct{ err error }

type ollamaCheckResult struct {
	reachable bool
	host      string
}

type memoryCheckResult struct {
	mb      int64
	warning string
}

type allQuickCheckDone struct{}

// SetupMode names the user journey that owns this state machine. Keeping the
// mode explicit prevents runtime recovery from accidentally continuing into
// the new-instance screens.
type SetupMode int

const (
	SetupFirstRun SetupMode = iota
	SetupAdditionalInstance
	SetupRuntimeRepair
)

type runtimeSetupEntry int

const (
	runtimeSetupFromCheck runtimeSetupEntry = iota
	runtimeSetupFromFirstRunChoice
)

type runtimeSetupStage int

const (
	runtimeSetupChoose runtimeSetupStage = iota
	runtimeSetupReview
	runtimeSetupWorking
	runtimeSetupWaiting
)

// --- Setup model ---

// SetupModel is the top-level Bubble Tea model for setup.
type SetupModel struct {
	BaseModel
	Stage SetupStage

	// Embedded, when true, means this model runs inside AppModel.
	// On completion or failure it emits WorkflowExitMsg instead of tea.Quit.
	Embedded  bool
	setupMode SetupMode

	// QuickCheck state.
	eng                    engine.Engine
	engErr                 error
	availableEngines       []engine.Engine // all detected engines (for switching)
	quickCheckReady        bool            // true when checks pass and user must confirm engine
	quickCheckAlternative  string          // missing runtime explicitly selected during first setup
	permChecking           bool            // true while re-checking permission after engine switch
	permErr                error
	ollamaOK               bool
	ollamaContainerChecked bool
	ollamaContainerOK      bool
	ollamaHost             string
	memMB                  int64
	memWarning             string
	memChecked             bool
	quickCheckDone         int // count of completed checks

	// Runtime setup state.
	runtimeProbes       []engine.ProbeResult
	runtimePlans        []engine.SetupPlan
	runtimeChoice       int
	runtimeCommandIndex int
	runtimeSetupStage   runtimeSetupStage
	runtimeShowDetails  bool
	runtimeMessage      string
	runtimeLastError    string
	preferredEngine     string
	runtimeSetupEntry   runtimeSetupEntry
	hostPlatform        engine.HostPlatform

	// Settings inputs.
	inputs           []textinput.Model
	inputFocus       int
	inputErrs        []string
	settingsAdvanced bool

	// Review.
	reviewWarnings []string

	// Apply setup.
	spinnerModel      SpinnerModel
	lastCompletedStep int             // -1 = none; used for rollback on failure
	existingNames     map[string]bool // container names already in use (cached at init)
	existingPorts     map[string]bool // browser ports already reserved by Omnideck

	// Image override (from --image flag, not shown in TUI).
	imageOverride string

	// Failure.
	errorMsg         string
	errorDetail      string
	errorShowDetails bool

	quickCheckSpinner spinner.Model
}

const (
	inputContainerName = iota
	inputMemory
	inputShmSize
	inputWebUIPort
	inputCount
)

// SetupRequest contains every input needed to create a setup workflow. Keeping
// mode and runtime selection here prevents callers from constructing the model
// and then mutating it into a different journey.
type SetupRequest struct {
	Initial           *config.Config
	ImageOverride     string
	ExistingInstances []config.InstanceInfo
	Mode              SetupMode
	PreferredEngine   string
	Embedded          bool
	WindowWidth       int
	WindowHeight      int
}

// NewSetupModel creates and initializes the requested setup workflow.
func NewSetupModel(req SetupRequest) SetupModel {
	inputs := make([]textinput.Model, inputCount)
	defaults := req.Initial
	if defaults == nil {
		defaults = config.DefaultConfig()
	}

	// Calculate memory defaults from total system RAM.
	defaultMem := defaults.Memory
	defaultShm := defaults.ShmSize
	if totalMB, err := checks.TotalMemoryMB(); err == nil && totalMB > 0 {
		defaultMem, defaultShm = checks.DefaultContainerMemory(totalMB)
	}
	if defaultMem == "" {
		defaultMem = "2g"
	}
	if defaultShm == "" {
		defaultShm = "1024m"
	}

	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[inputContainerName].Placeholder = "omnideck"
	inputs[inputContainerName].SetValue(defaults.ContainerName)

	inputs[inputMemory].Placeholder = "2g"
	inputs[inputMemory].SetValue(defaultMem)

	inputs[inputShmSize].Placeholder = "1024m"
	inputs[inputShmSize].SetValue(defaultShm)

	inputs[inputWebUIPort].Placeholder = "2337"
	inputs[inputWebUIPort].SetValue(defaults.WebUIPortOrDefault())

	inputs[inputContainerName].Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	// Cache existing container names to avoid blocking I/O during Update.
	existingNames := map[string]bool{}
	existingPorts := map[string]bool{}
	for _, inst := range req.ExistingInstances {
		if inst.Config != nil {
			existingNames[inst.Config.ContainerName] = true
			existingPorts[inst.Config.WebUIPortOrDefault()] = true
		}
	}

	setupMode := req.Mode
	if setupMode == SetupFirstRun && len(existingNames) > 0 {
		setupMode = SetupAdditionalInstance
	}

	return SetupModel{
		Stage:             SetupStageQuickCheck,
		Embedded:          req.Embedded,
		setupMode:         setupMode,
		inputs:            inputs,
		inputErrs:         make([]string, inputCount),
		quickCheckSpinner: sp,
		lastCompletedStep: -1,
		existingNames:     existingNames,
		existingPorts:     existingPorts,
		imageOverride:     req.ImageOverride,
		preferredEngine:   req.PreferredEngine,
		hostPlatform:      engine.DetectHostPlatform(),
		BaseModel:         BaseModel{WindowWidth: req.WindowWidth, WindowHeight: req.WindowHeight},
	}
}

func (m SetupModel) Init() tea.Cmd {
	return tea.Batch(
		m.quickCheckSpinner.Tick,
		runEngineCheckFor(m.preferredEngine),
		runOllamaCheck,
		runMemoryCheck,
	)
}

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Stage {
	case SetupStageQuickCheck:
		return m.updateQuickCheck(msg)
	case SetupStageRuntime:
		return m.updateRuntimeSetup(msg)
	case SetupStageSettings:
		return m.updateSettings(msg)
	case SetupStageReview:
		return m.updateReview(msg)
	case SetupStageApplying:
		return m.updateApplying(msg)
	case SetupStageComplete:
		if isKeyMsg(msg) {
			return m.exit(WorkflowCompleted)
		}
	case SetupStageFailed:
		return m.updateFailed(msg)
	}
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.HandleWindowSize(msg)
	}
	return m, nil
}

func (m SetupModel) exit(outcome WorkflowOutcome) (tea.Model, tea.Cmd) {
	if m.Embedded {
		return m, func() tea.Msg { return WorkflowExitMsg{Outcome: outcome} }
	}
	return m, tea.Quit
}

// View satisfies tea.Model by using the same canonical renderer hosted by AppModel.
func (m SetupModel) View() string {
	return m.TNView(m.WindowWidth, m.WindowHeight)
}

// isKeyMsg returns true if the message is any key press.
func isKeyMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyMsg)
	return ok
}
