package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

type runtimeSetupStage int

const (
	runtimeSetupWorking runtimeSetupStage = iota
	runtimeSetupRestart
)

type runtimeSetupEventMsg struct{ event engine.RuntimeSetupEvent }

type runtimeSetupDoneMsg struct {
	probe engine.ProbeResult
	err   error
}

type restartRequestedMsg struct{ err error }

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
	availableEngines       []engine.Engine // ready Podman engine, when available
	quickCheckReady        bool            // true when checks pass and user must confirm engine
	permChecking           bool            // true while re-checking account access
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
	runtimeProbes      []engine.ProbeResult
	runtimeSetupStage  runtimeSetupStage
	hostPlatform       engine.HostPlatform
	runtimeEvents      chan tea.Msg
	runtimeStage       string
	runtimeState       string
	runtimeActivity    string
	runtimeDetail      string
	runtimeProgress    *float64
	failureFromRuntime bool
	autoStart          bool

	// Settings inputs.
	inputs           []textinput.Model
	inputFocus       int
	inputErrs        []string
	settingsAdvanced bool

	// Review.
	reviewWarnings []string

	// Apply setup.
	spinnerModel       SpinnerModel
	setupEvents        chan tea.Msg
	lastCompletedStep  int             // -1 = none; used for rollback on failure
	homeVolumeCreated  bool            // true only when this setup created the volume
	stateVolumeCreated bool            // true only when this setup created the volume
	existingNames      map[string]bool // container names already in use (cached at init)
	existingPorts      map[string]bool // browser ports already reserved by Omnideck

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

// SetupRequest contains every input needed to create a setup workflow.
type SetupRequest struct {
	Initial           *config.Config
	ImageOverride     string
	ExistingInstances []config.InstanceInfo
	Mode              SetupMode
	Embedded          bool
	WindowWidth       int
	WindowHeight      int
	// AutoStart is used only by the private post-restart resume flag. A normal
	// bare invocation still shows the same one-button welcome as Desktop.
	AutoStart bool
}

// NewSetupModel creates and initializes the requested setup workflow.
func NewSetupModel(req SetupRequest) SetupModel {
	inputs := make([]textinput.Model, inputCount)
	defaults := req.Initial
	if defaults == nil {
		defaults = config.DefaultConfig()
	}

	hostPlatform := engine.DetectHostPlatform()
	// Use the same resource policy Desktop receives through the runtime JSON
	// contract. Explicit saved values remain the fallback if host capacity
	// cannot be read.
	defaultMem := defaults.Memory
	defaultShm := defaults.ShmSize
	if hostPlatform.TotalMemoryMB > 0 {
		resources := engine.DefaultRuntimeResources(hostPlatform)
		defaultMem, defaultShm = resources.ContainerMemory, resources.ContainerSHMSize
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

	stage := SetupStageQuickCheck
	if setupMode == SetupFirstRun && !req.AutoStart {
		stage = SetupStageWelcome
	}

	return SetupModel{
		Stage:             stage,
		Embedded:          req.Embedded,
		setupMode:         setupMode,
		inputs:            inputs,
		inputErrs:         make([]string, inputCount),
		quickCheckSpinner: sp,
		lastCompletedStep: -1,
		existingNames:     existingNames,
		existingPorts:     existingPorts,
		imageOverride:     req.ImageOverride,
		hostPlatform:      hostPlatform,
		autoStart:         req.AutoStart,
		BaseModel:         BaseModel{WindowWidth: req.WindowWidth, WindowHeight: req.WindowHeight},
	}
}

func (m SetupModel) Init() tea.Cmd {
	if m.Stage == SetupStageWelcome {
		return nil
	}
	return m.startQuickChecks()
}

func (m SetupModel) startQuickChecks() tea.Cmd {
	return tea.Batch(
		m.quickCheckSpinner.Tick,
		runEngineCheck,
		runOllamaCheck,
		runMemoryCheck,
	)
}

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Stage {
	case SetupStageWelcome:
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.HandleWindowSize(size)
			return m, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "enter", " ":
				m.Stage = SetupStageQuickCheck
				return m, m.startQuickChecks()
			case "q", "esc", "ctrl+c":
				return m.exit(WorkflowCanceled)
			}
		}
		return m, nil
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
