package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// --- Preflight result messages ---

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

type allPreflightDone struct{}

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

// --- Install model ---

// InstallModel is the top-level Bubble Tea model for the install wizard.
type InstallModel struct {
	BaseModel

	// Embedded, when true, means this model runs inside DashboardModel.
	// On done/error it emits WizardExitMsg instead of tea.Quit.
	Embedded  bool
	setupMode SetupMode

	// Preflight state.
	eng                  engine.Engine
	engErr               error
	availableEngines     []engine.Engine // all detected engines (for switching)
	preflightReady       bool            // true when checks pass and user must confirm engine
	preflightAlternative string          // missing runtime explicitly selected during first setup
	permChecking         bool            // true while re-checking permission after engine switch
	permErr              error
	ollamaOK             bool
	ollamaHost           string
	memMB                int64
	memWarning           string
	preflightDone        int // count of completed checks

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

	// Config inputs.
	inputs         []textinput.Model
	inputFocus     int
	inputErrs      []string
	configAdvanced bool

	// Confirm.
	confirmWarnings    []string
	confirmShowDetails bool

	// Install.
	spinnerModel      SpinnerModel
	installErr        error
	lastCompletedStep int             // -1 = none; used for rollback on failure
	existingNames     map[string]bool // container names already in use (cached at init)
	existingPorts     map[string]bool // browser ports already reserved by Omnideck

	// Image override (from --image flag, not shown in TUI).
	imageOverride string

	// Done.
	configPath string
	savedCfg   *config.Config

	// Error.
	errorMsg         string
	errorDetail      string
	errorShowDetails bool

	preflightSpinner spinner.Model
}

const (
	inputContainerName = iota
	inputMemory
	inputShmSize
	inputWebUIPort
	inputCount
)

// NewInstallModel creates and initialises the install wizard model.
// If initial is non-nil its values are used instead of DefaultConfig().
// imageOverride, if non-empty, replaces the default container image (not shown in TUI).
func NewInstallModel(configPath string, initial *config.Config, imageOverride string) InstallModel {
	inputs := make([]textinput.Model, inputCount)
	defaults := initial
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
	if instances, err := config.ListInstances(); err == nil {
		for _, inst := range instances {
			if inst.Config != nil {
				existingNames[inst.Config.ContainerName] = true
				existingPorts[inst.Config.WebUIPortOrDefault()] = true
			}
		}
	}

	setupMode := SetupFirstRun
	if len(existingNames) > 0 {
		setupMode = SetupAdditionalInstance
	}

	return InstallModel{
		BaseModel:         BaseModel{Phase: PhasePreflight},
		setupMode:         setupMode,
		inputs:            inputs,
		inputErrs:         make([]string, inputCount),
		preflightSpinner:  sp,
		configPath:        configPath,
		lastCompletedStep: -1,
		existingNames:     existingNames,
		existingPorts:     existingPorts,
		imageOverride:     imageOverride,
		hostPlatform:      engine.DetectHostPlatform(),
	}
}

func (m InstallModel) Init() tea.Cmd {
	return tea.Batch(
		m.preflightSpinner.Tick,
		runEngineCheckFor(m.preferredEngine),
		runOllamaCheck,
		runMemoryCheck,
	)
}

func (m InstallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Phase {
	case PhasePreflight:
		return m.updatePreflight(msg)
	case PhaseRuntimeSetup:
		return m.updateRuntimeSetup(msg)
	case PhaseConfig:
		return m.updateConfig(msg)
	case PhaseConfirm:
		return m.updateConfirm(msg)
	case PhaseInstall:
		return m.updateInstall(msg)
	case PhaseDone:
		if isKeyMsg(msg) {
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
	case PhaseError:
		return m.updateError(msg)
	}
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.HandleWindowSize(msg)
	}
	return m, nil
}

func (m InstallModel) updatePreflight(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.preflightSpinner, cmd = m.preflightSpinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if !m.preflightReady || m.permChecking {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		case "enter", " ":
			if m.preflightAlternative != "" {
				m.preferredEngine = m.preflightAlternative
				m.runtimeSetupEntry = runtimeSetupFromFirstRunChoice
				m.configureRuntimeSetup()
				return m, nil
			}
			if m.permErr != nil {
				return m, nil // block advance if new engine has no permission
			}
			return m.afterRuntimeReady()
		case "tab", "s":
			if alternative := m.setupAlternativeRuntime(); alternative != "" {
				if m.preflightAlternative == alternative {
					m.preflightAlternative = ""
				} else {
					m.preflightAlternative = alternative
				}
				return m, nil
			}
			if m.preferredEngine != "" || len(m.availableEngines) < 2 {
				return m, nil
			}
			for i, e := range m.availableEngines {
				if e.Name() == m.eng.Name() {
					m.eng = m.availableEngines[(i+1)%len(m.availableEngines)]
					break
				}
			}
			m.permErr = nil
			m.permChecking = true
			return m, runPermissionCheck(m.eng)
		}

	case engineCheckResult:
		m.preflightDone++
		m.eng = msg.eng
		m.engErr = msg.err
		m.availableEngines = msg.all
		m.runtimeProbes = msg.probes
		m.preflightAlternative = ""
		// Run permission check only after engine is known.
		if msg.eng != nil {
			return m, runPermissionCheck(msg.eng)
		}
		// Engine not found — permission check will never fire; count it as done.
		m.preflightDone++
		return m, m.maybeAdvancePreflight()

	case permissionCheckResult:
		if m.preflightReady {
			// Engine was switched — update result without touching the counter.
			m.permChecking = false
			m.permErr = msg.err
			return m, nil
		}
		m.preflightDone++
		m.permErr = msg.err
		return m, m.maybeAdvancePreflight()

	case ollamaCheckResult:
		m.preflightDone++
		m.ollamaOK = msg.reachable
		m.ollamaHost = msg.host
		return m, m.maybeAdvancePreflight()

	case memoryCheckResult:
		m.preflightDone++
		m.memMB = msg.mb
		m.memWarning = msg.warning
		return m, m.maybeAdvancePreflight()

	case allPreflightDone:
		if m.engErr != nil {
			m.runtimeSetupEntry = runtimeSetupFromCheck
			m.configureRuntimeSetup()
			return m, nil
		}
		if m.permErr != nil && len(m.availableEngines) <= 1 {
			if m.eng != nil {
				for i := range m.runtimeProbes {
					if m.runtimeProbes[i].Name == m.eng.Name() {
						m.runtimeProbes[i].State = engine.RuntimePermissionDenied
					}
				}
			}
			m.runtimeSetupEntry = runtimeSetupFromCheck
			m.configureRuntimeSetup()
			return m, nil
		}
		// On a fresh setup, pause whenever there is a meaningful runtime
		// choice—even if one option is ready and the other still needs setup.
		if m.preferredEngine == "" && (len(m.availableEngines) > 1 || m.setupAlternativeRuntime() != "") {
			m.preflightReady = true
			return m, nil
		}
		return m.afterRuntimeReady()
	}
	return m, nil
}

// setupAlternativeRuntime returns a runtime that is not ready yet but can be
// explicitly chosen during a fresh setup. Existing instances stay on their one
// shared runtime so their containers and volumes are never silently stranded.
func (m InstallModel) setupAlternativeRuntime() string {
	if m.setupMode != SetupFirstRun || m.preferredEngine != "" || len(m.existingNames) != 0 || m.eng == nil || len(m.availableEngines) != 1 {
		return ""
	}
	for _, probe := range m.runtimeProbes {
		if probe.Name != m.eng.Name() && !probe.Ready() {
			return probe.Name
		}
	}
	return ""
}

func (m *InstallModel) configureRuntimeSetup() {
	m.Phase = PhaseRuntimeSetup
	m.runtimeSetupStage = runtimeSetupChoose
	m.runtimeShowDetails = false
	m.runtimeLastError = ""
	host := m.hostPlatform
	if host.OS == "" {
		host = engine.DetectHostPlatform()
	}
	m.runtimePlans = engine.BuildSetupPlans(m.runtimeProbes, host)
	if m.preferredEngine != "" {
		filtered := m.runtimePlans[:0]
		for _, plan := range m.runtimePlans {
			if plan.Runtime == m.preferredEngine {
				plan.Recommended = true
				plan.Recommendation = "Omnideck will use " + plan.Title + " for all your installations on this computer."
				filtered = append(filtered, plan)
			}
		}
		m.runtimePlans = filtered
	}
	m.runtimeChoice = 0
	for i, plan := range m.runtimePlans {
		if plan.Recommended {
			m.runtimeChoice = i
			break
		}
	}
}

func (m InstallModel) updateRuntimeSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.preflightSpinner, cmd = m.preflightSpinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
		if m.runtimeSetupStage == runtimeSetupWorking {
			return m, nil
		}
		if len(m.runtimePlans) == 0 {
			switch msg.String() {
			case "r", "enter", " ":
				m.runtimeSetupStage = runtimeSetupWorking
				m.runtimeMessage = "Checking again…"
				return m, runEngineCheckFor(m.preferredEngine)
			case "b", "esc":
				if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
					m.runtimeSetupEntry = runtimeSetupFromCheck
					m.preferredEngine = ""
					m.preflightAlternative = ""
					m.Phase = PhasePreflight
					m.preflightReady = true
					m.runtimeMessage = ""
				}
			}
			return m, nil
		}
		if msg.String() == "esc" {
			if m.runtimeSetupStage == runtimeSetupReview || m.runtimeSetupStage == runtimeSetupWaiting {
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeMessage = ""
				return m, nil
			}
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
		if m.runtimeSetupStage == runtimeSetupWaiting {
			switch msg.String() {
			case "r", "enter", " ":
				m.runtimeSetupStage = runtimeSetupWorking
				m.runtimeMessage = "Checking whether Podman or Docker is ready…"
				return m, runEngineCheckFor(m.preferredEngine)
			case "b":
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeMessage = ""
			case "o":
				plan := m.runtimePlans[m.runtimeChoice]
				if plan.URL != "" {
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		if m.runtimeSetupStage == runtimeSetupReview {
			switch msg.String() {
			case "b":
				m.runtimeSetupStage = runtimeSetupChoose
				m.runtimeShowDetails = false
			case "d":
				m.runtimeShowDetails = !m.runtimeShowDetails
			case "enter", " ":
				plan := m.runtimePlans[m.runtimeChoice]
				if len(plan.Commands) > 0 {
					m.runtimeSetupStage = runtimeSetupWorking
					m.runtimeCommandIndex = 0
					m.runtimeMessage = "Starting the first step…"
					return m, execRuntimeCommand(plan.Commands[0])
				}
				if plan.URL != "" {
					m.runtimeSetupStage = runtimeSetupWaiting
					m.runtimeMessage = runtimeWaitingMessage(plan)
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "up", "left", "shift+tab":
			m.runtimeChoice = (m.runtimeChoice - 1 + len(m.runtimePlans)) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "down", "right", "tab":
			m.runtimeChoice = (m.runtimeChoice + 1) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "d":
			m.runtimeShowDetails = !m.runtimeShowDetails
		case "b":
			if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
				m.runtimeSetupEntry = runtimeSetupFromCheck
				m.preferredEngine = ""
				m.preflightAlternative = ""
				m.Phase = PhasePreflight
				m.preflightReady = true
				m.runtimeMessage = ""
				m.runtimeLastError = ""
			}
		case "r":
			m.runtimeSetupStage = runtimeSetupWorking
			m.runtimeMessage = "Checking whether Podman or Docker is ready…"
			return m, runEngineCheckFor(m.preferredEngine)
		case "enter", " ":
			m.runtimeSetupStage = runtimeSetupReview
			m.runtimeShowDetails = false
			m.runtimeMessage = ""
		}
	case runtimeCommandDone:
		if msg.err != nil {
			m.runtimeSetupStage = runtimeSetupChoose
			m.runtimeLastError = msg.err.Error()
			m.runtimeMessage = "That step did not finish. You can review it and try again. Press d if you want to see the technical error."
			if len(m.runtimePlans) > 1 {
				m.runtimeMessage = "That step did not finish. You can try again or choose the other option. Press d if you want to see the technical error."
			}
			return m, nil
		}
		plan := m.runtimePlans[m.runtimeChoice]
		m.runtimeCommandIndex++
		if m.runtimeCommandIndex < len(plan.Commands) {
			command := plan.Commands[m.runtimeCommandIndex]
			m.runtimeMessage = fmt.Sprintf("Starting step %d of %d…", m.runtimeCommandIndex+1, len(plan.Commands))
			return m, execRuntimeCommand(command)
		}
		if plan.RequiresRestart {
			m.runtimeSetupStage = runtimeSetupWaiting
			m.runtimeMessage = plan.Title + " is starting. Wait until it says it is ready, then return here and press Enter."
			return m, nil
		}
		m.runtimeMessage = "That step finished. Checking that everything is ready…"
		return m, runEngineCheckFor(m.preferredEngine)
	case engineCheckResult:
		m.runtimeSetupStage = runtimeSetupChoose
		m.runtimeProbes = msg.probes
		if msg.eng != nil {
			m.eng = msg.eng
			m.engErr = nil
			m.availableEngines = msg.all
			m.permErr = nil
			return m.afterRuntimeReady()
		}
		m.engErr = msg.err
		m.configureRuntimeSetup()
		m.runtimeMessage = runtimeNotReadyMessage(m.runtimePlans, m.preferredEngine)
	}
	return m, nil
}

func runtimeWaitingMessage(plan engine.SetupPlan) string {
	name := plan.Title
	if plan.DirectDownload {
		return "The official " + name + " installer should now be downloading. Open it from Downloads, finish the installer, then return here."
	}
	switch plan.State {
	case engine.RuntimePermissionDenied, engine.RuntimeBroken:
		return "The official " + name + " help page should now be open. Follow the guidance there, then return here."
	case engine.RuntimeUnsupportedVersion:
		return "The official Docker update instructions should now be open. Update Docker and make sure it is running, then return here."
	case engine.RuntimeStopped, engine.RuntimeMachineStopped:
		return "The official " + name + " help page should now be open. Follow its instructions to start " + name + ", then return here."
	default:
		return "The official " + name + " installation page should now be open. Finish the installation and make sure " + name + " is running, then return here."
	}
}

func runtimeNotReadyMessage(plans []engine.SetupPlan, preferred string) string {
	if len(plans) == 0 {
		name := "Podman or Docker"
		if preferred != "" {
			name = runtimeNameForPeople(preferred)
		}
		return "Omnideck still cannot use " + name + ". Make sure it is installed and running, then check again."
	}
	if len(plans) > 1 {
		return "Neither Podman nor Docker is ready yet. Choose one of the setup options below, or check again if you just finished a step."
	}
	plan := plans[0]
	switch plan.State {
	case engine.RuntimeMissing:
		return "Omnideck still cannot find " + plan.Title + ". Make sure the installation finished, then open " + plan.Title + " and wait until it is running. You can review the installation step below or check again."
	case engine.RuntimeStopped, engine.RuntimeMachineStopped:
		return plan.Title + " is installed, but it is not running yet. Review the start step below or check again after you start it."
	case engine.RuntimeMachineMissing:
		return "Podman is installed, but its one-time setup is not finished. Review the step below to finish it."
	case engine.RuntimePermissionDenied:
		return plan.Title + " is installed, but your account cannot use it yet. Review the help step below."
	case engine.RuntimeUnsupportedVersion:
		return "Docker is installed, but it must be updated before Omnideck can use it. Review the update step below."
	default:
		return plan.Title + " is installed, but it still needs attention. Review the help step below or check again."
	}
}

func (m InstallModel) afterRuntimeReady() (tea.Model, tea.Cmd) {
	if m.setupMode == SetupRuntimeRepair {
		if m.eng == nil {
			m.Phase = PhaseError
			m.errorMsg = "Omnideck could not find a ready container runtime"
			return m, nil
		}
		if err := config.SaveRuntime(m.eng.Name()); err != nil {
			m.Phase = PhaseError
			m.errorMsg = "Omnideck could not remember the container runtime"
			m.errorDetail = err.Error()
			return m, nil
		}
		if m.Embedded {
			return m, func() tea.Msg { return WizardExitMsg{} }
		}
		return m, tea.Quit
	}
	m.ensureRecommendedSettingsAvailable()
	m.Phase = PhaseConfig
	return m, nil
}

// ensureRecommendedSettingsAvailable quietly advances default names and ports
// when another container or app already uses the first suggestion. The user
// still sees and confirms the final values before setup starts.
func (m *InstallModel) ensureRecommendedSettingsAvailable() {
	if m.eng != nil {
		candidate := strings.TrimSpace(m.inputs[inputContainerName].Value())
		exists, err := m.eng.ContainerExists(candidate)
		if err == nil && (exists || m.existingNames[candidate]) {
			for suffix := 2; suffix < 10000; suffix++ {
				candidate = "omnideck" + strconv.Itoa(suffix)
				exists, err = m.eng.ContainerExists(candidate)
				if err == nil && !exists && !m.existingNames[candidate] {
					break
				}
			}
		}
		m.inputs[inputContainerName].SetValue(candidate)
	}

	port := strings.TrimSpace(m.inputs[inputWebUIPort].Value())
	if !m.existingPorts[port] && checks.PortAvailable(port) {
		return
	}
	start, err := strconv.Atoi(port)
	if err != nil {
		start = 2337
	}
	if available, ok := checks.NextAvailablePort(start+1, m.existingPorts); ok {
		m.inputs[inputWebUIPort].SetValue(available)
	}
}

func execRuntimeCommand(command engine.SetupCommand) tea.Cmd {
	cmd := exec.Command(command.Name, command.Args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return runtimeCommandDone{err: err}
	})
}

// maybeAdvancePreflight returns a Cmd that fires allPreflightDone once all
// 4 checks (engine, permission counted together, ollama, memory) are done.
// Engine check fires permission check as a follow-up, so we wait for 4 total.
func (m *InstallModel) maybeAdvancePreflight() tea.Cmd {
	// We expect: engine(1) + permission(1) + ollama(1) + memory(1) = 4
	if m.preflightDone >= 4 {
		return func() tea.Msg { return allPreflightDone{} }
	}
	return nil
}

func (m InstallModel) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		if !m.configAdvanced {
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				return m, tea.Quit
			case "enter", " ":
				if m.validateAllInputs() {
					m.Phase = PhaseConfirm
					m.confirmShowDetails = false
					m.buildConfirmWarnings()
				} else {
					m.configAdvanced = true
					for i := range m.inputs {
						m.inputs[i].Blur()
					}
					m.inputs[m.inputFocus].Focus()
				}
			case "c":
				m.configAdvanced = true
				m.inputFocus = 0
				m.inputs[m.inputFocus].Focus()
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.inputs[m.inputFocus].Blur()
			m.configAdvanced = false
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if m.validateCurrentInput() {
				m.inputs[m.inputFocus].Blur()
				m.inputFocus++
				if m.inputFocus >= inputCount {
					m.inputFocus = inputCount - 1
					m.Phase = PhaseConfirm
					m.buildConfirmWarnings()
					return m, nil
				}
				m.inputs[m.inputFocus].Focus()
			}
		case tea.KeyShiftTab:
			if m.inputFocus > 0 {
				m.inputs[m.inputFocus].Blur()
				m.inputFocus--
				m.inputs[m.inputFocus].Focus()
			}
		default:
			var cmd tea.Cmd
			m.inputs[m.inputFocus], cmd = m.inputs[m.inputFocus].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *InstallModel) validateCurrentInput() bool {
	val := strings.TrimSpace(m.inputs[m.inputFocus].Value())
	if val == "" {
		m.inputErrs[m.inputFocus] = "cannot be empty"
		return false
	}
	switch m.inputFocus {
	case inputContainerName:
		if !validContainerName(val) {
			m.inputErrs[m.inputFocus] = "only letters, digits, underscores, hyphens, and dots allowed"
			return false
		}
		if m.existingNames[val] {
			m.inputErrs[m.inputFocus] = "an instance named '" + val + "' already exists"
			return false
		}
		if m.eng != nil {
			exists, err := m.eng.ContainerExists(val)
			if err != nil {
				m.inputErrs[m.inputFocus] = "Omnideck could not check whether this name is available"
				return false
			}
			if exists {
				m.inputErrs[m.inputFocus] = "another container already uses this name; choose a different name"
				return false
			}
		}
	case inputMemory, inputShmSize:
		if !validMemSize(val) {
			m.inputErrs[m.inputFocus] = "must be a number + unit (e.g. 512m, 2g)"
			return false
		}
	case inputWebUIPort:
		if !validPort(val) {
			m.inputErrs[m.inputFocus] = "must be a number between 1 and 65535"
			return false
		}
		if m.existingPorts[val] {
			m.inputErrs[m.inputFocus] = "another Omnideck installation already uses this browser address number"
			return false
		}
		if !checks.PortAvailable(val) {
			m.inputErrs[m.inputFocus] = "another app is already using this browser address number"
			return false
		}
	}
	m.inputErrs[m.inputFocus] = ""
	return true
}

func (m *InstallModel) validateAllInputs() bool {
	originalFocus := m.inputFocus
	for i := range m.inputs {
		m.inputFocus = i
		if !m.validateCurrentInput() {
			return false
		}
	}
	m.inputFocus = originalFocus
	return true
}

// expandTilde replaces a leading ~ with the user's home directory for validation.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func validMemSize(s string) bool {
	return checks.ValidMemorySize(s)
}

func validContainerName(s string) bool {
	return checks.ValidContainerName(s)
}

func validPort(s string) bool {
	return checks.ValidPort(s)
}

func (m *InstallModel) buildConfirmWarnings() {
	m.confirmWarnings = nil
	if !m.ollamaOK {
		m.confirmWarnings = append(m.confirmWarnings,
			"Optional local AI was not found. That is okay—you can use online AI services now and add Ollama later.")
	}
	if m.memWarning != "" {
		m.confirmWarnings = append(m.confirmWarnings, m.memWarning)
	}
	if m.eng != nil {
		for _, probe := range m.runtimeProbes {
			if probe.Name == m.eng.Name() && probe.Warning != "" {
				m.confirmWarnings = append(m.confirmWarnings, probe.Warning)
			}
		}
	}
}

func (m InstallModel) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.Phase = PhaseConfig
			m.configAdvanced = false
			m.confirmShowDetails = false
			return m, nil
		case "d":
			m.confirmShowDetails = !m.confirmShowDetails
		case "i", "enter", " ":
			if !m.validateAllInputs() {
				m.Phase = PhaseConfig
				m.configAdvanced = true
				for i := range m.inputs {
					m.inputs[i].Blur()
				}
				m.inputs[m.inputFocus].Focus()
				return m, nil
			}
			m.Phase = PhaseInstall
			m.lastCompletedStep = -1
			m.errorMsg = ""
			m.errorDetail = ""
			m.errorShowDetails = false
			m.spinnerModel = NewSpinnerModel(installStepLabels, defaultFlavorMessages)
			return m, tea.Batch(m.spinnerModel.Init(), m.startInstallStep(0))
		}
	}
	return m, nil
}

var installStepLabels = []string{
	"Check that the name and browser address are available",
	"Prepare space for your files",
	"Prepare space for Omnideck data",
	"Download Omnideck",
	"Start Omnideck",
	"Remember these settings",
}

func (m InstallModel) updateInstall(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)

	case StepDoneMsg:
		m.lastCompletedStep = msg.Index
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		next := msg.Index + 1
		if next < len(installStepLabels) {
			return m, tea.Batch(cmd, m.startInstallStep(next))
		}
		// All steps done.
		m.Phase = PhaseDone
		return m, cmd

	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Phase = PhaseError
		m.errorMsg = installStepLabels[msg.Index]
		m.errorDetail = msg.Err.Error()
		m.errorShowDetails = false
		if msg.Index == 0 {
			m.Phase = PhaseConfig
			m.configAdvanced = true
			_ = m.validateAllInputs()
			for i := range m.inputs {
				m.inputs[i].Blur()
			}
			m.inputs[m.inputFocus].Focus()
			return m, cmd
		}
		// Attempt rollback: if the container was run (step 4 completed) but
		// config was not saved (step 5 failed), remove the orphaned container.
		if m.lastCompletedStep >= 4 {
			cfg := m.buildConfig()
			eng := m.eng
			rollbackCmd := func() tea.Msg {
				_ = eng.StopContainer(cfg.ContainerName)
				_ = eng.RemoveContainer(cfg.ContainerName)
				return nil
			}
			return m, tea.Batch(cmd, rollbackCmd)
		}
		return m, cmd

	default:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m InstallModel) updateError(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "d":
			m.errorShowDetails = !m.errorShowDetails
		case "r":
			m.Phase = PhaseConfirm
			m.confirmShowDetails = false
		case "b", "enter", "esc", "q", "ctrl+c":
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

// startInstallStep returns the tea.Cmd for step i, sending StepStartedMsg
// immediately and running the work asynchronously.
func (m *InstallModel) startInstallStep(i int) tea.Cmd {
	cfg := m.buildConfig()
	eng := m.eng

	// Fire StepStartedMsg first.
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }

	var workCmd tea.Cmd
	switch i {
	case 0: // Recheck name and port immediately before making changes.
		workCmd = StepCmd(i, func() (string, error) {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil {
				return "", fmt.Errorf("checking the name %q: %w", cfg.ContainerName, err)
			}
			if exists {
				return "", fmt.Errorf("another container already uses the name %q; go back and choose a different name", cfg.ContainerName)
			}
			if !checks.PortAvailable(cfg.WebUIPortOrDefault()) {
				return "", fmt.Errorf("another app is already using browser address number %s; go back and choose a different number", cfg.WebUIPortOrDefault())
			}
			return "available", nil
		})
	case 1: // Create home volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.HomeVolumeName())
		})
	case 2: // Create state volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.StateVolumeName())
		})
	case 3: // Pull image.
		workCmd = StepCmd(i, func() (string, error) {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(cfg.Image, msgs)
			close(msgs)
			return "", err
		})
	case 4: // Run container.
		workCmd = StepCmd(i, func() (string, error) {
			opts := engine.RunOptions{
				Name:        cfg.ContainerName,
				Image:       cfg.Image,
				Memory:      cfg.Memory,
				ShmSize:     cfg.ShmSize,
				HomeVolume:  cfg.HomeVolumeName(),
				StateVolume: cfg.StateVolumeName(),
				Restart:     "always",
				WebUIPort:   cfg.WebUIPortOrDefault(),
				Platform:    runtime.GOOS,
			}
			return "", eng.RunContainer(opts)
		})
	case 5: // Save config.
		workCmd = StepCmd(i, func() (string, error) {
			cfg.InstalledAt = time.Now()
			cfg.Engine = ""
			if err := config.SaveRuntime(eng.Name()); err != nil {
				return "", err
			}
			// Always save to the instances dir keyed by container name.
			savePath := config.InstancePath(cfg.ContainerName)
			m.configPath = savePath
			return savePath, config.Save(savePath, cfg)
		})
	}

	return tea.Sequence(startCmd, workCmd)
}

func (m *InstallModel) buildConfig() *config.Config {
	image := config.DefaultConfig().Image
	if m.imageOverride != "" {
		image = m.imageOverride
	}
	return &config.Config{
		ContainerName: strings.TrimSpace(m.inputs[inputContainerName].Value()),
		Memory:        strings.TrimSpace(m.inputs[inputMemory].Value()),
		ShmSize:       strings.TrimSpace(m.inputs[inputShmSize].Value()),
		WebUIPort:     strings.TrimSpace(m.inputs[inputWebUIPort].Value()),
		Image:         image,
	}
}

// View renders the current wizard phase.
func (m InstallModel) View() string {
	switch m.Phase {
	case PhasePreflight:
		return m.viewPreflight()
	case PhaseRuntimeSetup:
		return m.viewRuntimeSetup()
	case PhaseConfig:
		return m.viewConfig()
	case PhaseConfirm:
		return m.viewConfirm()
	case PhaseInstall:
		return m.viewInstall()
	case PhaseDone:
		return m.viewDone()
	case PhaseError:
		return m.viewError()
	}
	return ""
}

func (m InstallModel) viewPreflight() string {
	out := styles.Header("OMNIDECK", "Quick check", m.WindowWidth)

	type row struct {
		label, detail string
		ok, done      bool
		warn          bool
	}
	var rows []row

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, row{"Podman or Docker", runtimeNameForPeople(m.eng.Name()) + " is ready", true, true, false})
	} else if engDone {
		rows = append(rows, row{"Podman or Docker", "setup needed", false, true, false})
	} else {
		rows = append(rows, row{"Podman or Docker", "", false, false, false})
	}

	if m.eng != nil {
		permDone := m.preflightDone >= 2
		if permDone {
			permOK := m.permErr == nil
			detail := "your account can use it"
			if !permOK {
				detail = "your account needs access"
			}
			rows = append(rows, row{"Account access", detail, permOK, true, false})
		} else {
			rows = append(rows, row{"Account access", "", false, false, false})
		}
	}

	ollamaDone := m.ollamaHost != ""
	ollamaDetail := "not found — you can add it later"
	if m.ollamaOK {
		ollamaDetail = "Ollama is ready"
	}
	rows = append(rows, row{"Local AI (optional)", ollamaDetail, m.ollamaOK, ollamaDone, !m.ollamaOK && ollamaDone})

	memDone := m.memMB > 0
	memDetail := "checking..."
	if memDone {
		memDetail = fmt.Sprintf("%d MB available", m.memMB)
	}
	memWarn := m.memWarning != "" && memDone
	rows = append(rows, row{"Available memory", memDetail, !memWarn, memDone, memWarn})

	rows = append(rows, row{"This computer", friendlyOS(runtime.GOOS), true, true, false})

	const labelW = 22
	for _, r := range rows {
		label := padRight(r.label, labelW)
		if !r.done {
			out += "   " + m.preflightSpinner.View() + "  " +
				styles.Dim.Render(label) +
				styles.Dim.Render("checking...") + "\n"
			continue
		}
		if r.warn {
			out += "   " + styles.WarnMark + "  " + styles.Warning.Render(label) +
				styles.Warning.Render(r.detail) + "\n"
		} else if r.ok {
			out += "   " + styles.CheckMark + "  " + styles.Dim.Render(label) +
				styles.Dim.Render(r.detail) + "\n"
		} else {
			out += "   " + styles.CrossMark + "  " + styles.Error.Render(label) +
				styles.Error.Render(r.detail) + "\n"
		}
	}

	if m.preflightReady && m.preferredEngine == "" {
		out += "\n"
		if m.permChecking {
			out += "   " + m.preflightSpinner.View() + "  " + styles.Dim.Render("Checking whether your account can use "+runtimeNameForPeople(m.eng.Name())+"...") + "\n"
		} else if m.permErr != nil {
			out += "   " + styles.CrossMark + "  " + styles.Error.Render("Your account cannot use "+runtimeNameForPeople(m.eng.Name())+" yet. Choose the other option to continue.") + "\n"
		}
		if len(m.availableEngines) > 1 {
			other := m.availableEngines[0]
			for _, e := range m.availableEngines {
				if e.Name() != m.eng.Name() {
					other = e
					break
				}
			}
			out += "\n   " + styles.Accent.Render("[tab]") + styles.Dim.Render(" switch to "+runtimeNameForPeople(other.Name())+"    ") +
				styles.Accent.Render("[enter]") + styles.Dim.Render(" continue with "+runtimeNameForPeople(m.eng.Name())) + "\n"
		} else if alternative := m.setupAlternativeRuntime(); alternative != "" {
			currentName := runtimeNameForPeople(m.eng.Name())
			alternativeName := runtimeNameForPeople(alternative)
			selected := "Use " + currentName + " — Ready"
			if m.preflightAlternative != "" {
				selected = "Set up " + alternativeName + " instead"
			}
			out += "\n   " + styles.Active.Render(selected) + "\n"
			out += "   " + styles.Accent.Render("[tab]") + styles.Dim.Render(" choose ") +
				styles.Accent.Render("[enter]") + styles.Dim.Render(" continue") + "\n"
		} else {
			out += "\n   " + styles.Accent.Render("[enter]") + styles.Dim.Render(" continue") + "\n"
		}
	}
	return out
}

func (m InstallModel) viewConfig() string {
	out := styles.Header("OMNIDECK", "Settings", m.WindowWidth)
	if !m.configAdvanced {
		out += "  " + styles.Active.Render("Recommended settings are ready") + "\n"
		out += "  " + styles.Dim.Render("Omnideck chose sensible settings for this computer. Most people can continue without changing anything.") + "\n\n"
		out += "  " + styles.Dim.Render(padRight("Name", 20)) + styles.Active.Render(m.inputs[inputContainerName].Value()) + "\n"
		out += "  " + styles.Dim.Render(padRight("Open in browser", 20)) + styles.Active.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n"
		out += "  " + styles.Dim.Render(padRight("Memory", 20)) + styles.Active.Render(m.inputs[inputMemory].Value()+" — chosen for this computer") + "\n"
		out += "\n  " + styles.Success.Render("Press Enter to use these settings.") + "\n"
		out += "  " + styles.Dim.Render("Press c to customize them.")
		return out
	}

	fieldNames := []string{"Omnideck name", "Memory limit", "Shared memory", "Browser address number"}
	fieldDescs := []string{
		"A label for this installation. Most people can keep the suggested name.",
		"The most memory Omnideck may use. For example: 2g or 4g.",
		"Temporary working space used by some features. The suggested value is recommended.",
		"The number at the end of the local web address. Change it only if another app already uses it.",
	}

	for i, inp := range m.inputs {
		active := i == m.inputFocus
		label := padRight(fieldNames[i], 20)
		if active {
			out += "\n   " + styles.Accent.Render("▶ ") + styles.Active.Render(label) +
				inp.View() + "\n"
			out += "     " + styles.Dim.Render(fieldDescs[i]) + "\n"
		} else {
			out += "\n   " + styles.Dim.Render("  "+label) + inp.View() + "\n"
		}
		if m.inputErrs[i] != "" {
			out += "     " + styles.Error.Render("✗ "+m.inputErrs[i]) + "\n"
		}
	}

	out += "\n\n   " + styles.Dim.Render("Tab / Enter  ─  next field      Shift+Tab  ─  previous      Esc  ─  recommended settings")
	return out
}

func (m InstallModel) viewConfirm() string {
	out := styles.Header("OMNIDECK", "Ready to set up", m.WindowWidth)

	kv := func(k, v string) string {
		return "  " + styles.Dim.Render(padRight(k, 18)) + styles.Active.Render(v) + "\n"
	}

	cfg := m.buildConfig()
	out += "  Here is what Omnideck will do after you press Enter:\n\n"
	out += "    1. Download the Omnideck app.\n"
	out += "    2. Prepare saved space for your files and settings.\n"
	out += "    3. Start Omnideck at http://localhost:" + m.inputs[inputWebUIPort].Value() + ".\n\n"
	out += kv("Runs with", runtimeNameForPeople(m.eng.Name()))
	out += kv("Name", m.inputs[inputContainerName].Value())
	out += kv("Memory", m.inputs[inputMemory].Value())

	for _, w := range m.confirmWarnings {
		out += "\n   " + styles.Warning.Render(w) + "\n"
	}

	if m.confirmShowDetails {
		out += "\n   " + styles.Dim.Render("Technical details") + "\n"
		out += kv("Computer", runtime.GOOS+" / "+runtime.GOARCH)
		out += kv("File storage", cfg.HomeVolumeName())
		out += kv("App storage", cfg.StateVolumeName())
		out += kv("Shared memory", m.inputs[inputShmSize].Value())
		out += kv("Download", cfg.Image)
	}

	out += "\n   " + styles.Accent.Render("[enter]") + styles.Dim.Render(" start setup    ") +
		styles.Accent.Render("[b]") + styles.Dim.Render(" back    ") +
		styles.Accent.Render("[d]") + styles.Dim.Render(" technical details    ") +
		styles.Accent.Render("[q]") + styles.Dim.Render(" quit")
	return out
}

func (m InstallModel) viewInstall() string {
	out := styles.Header("OMNIDECK", "Installing", m.WindowWidth)
	out += m.spinnerModel.View()
	return out
}

func (m InstallModel) viewDone() string {
	out := "\n" + styles.Success.Render("  ✓  Omnideck is ready!") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"

	out += "  " + styles.Dim.Render("Open Omnideck in your browser:") + "\n"
	out += "  " + styles.Active.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n\n"
	out += "  " + styles.Dim.Render("Your files and settings will be kept when Omnideck updates.") + "\n"
	out += styles.Dim.Render("  Press any key to exit.")
	return out
}

func (m InstallModel) viewRuntimeSetup() string {
	out := styles.Header("OMNIDECK", "Container setup", m.WindowWidth)
	if len(m.runtimePlans) == 0 {
		name := "Podman or Docker"
		if m.preferredEngine != "" {
			name = runtimeNameForPeople(m.preferredEngine)
		}
		out += "  " + styles.Active.Render("Omnideck still cannot use "+name) + "\n\n"
		message := m.runtimeMessage
		if message == "" {
			message = "Omnideck could not determine another safe setup step. Make sure the app is installed, open it, and wait until it says it is running."
		}
		out += "  " + styles.Dim.Render(message) + "\n\n"
		if m.runtimeSetupStage == runtimeSetupWorking {
			out += "  " + m.preflightSpinner.View() + " " + styles.Dim.Render("Checking again…") + "\n"
		} else {
			out += "  " + styles.Success.Render("Press R to check again. You can also press Q to leave setup.") + "\n"
		}
		return out
	}
	plan := m.runtimePlans[m.runtimeChoice]
	if m.runtimeSetupStage == runtimeSetupWaiting {
		out += "  " + styles.Active.Render("Finish this step, then come back here") + "\n\n"
		out += "  " + styles.Dim.Render(m.runtimeMessage) + "\n\n"
		out += "  After you finish the step on the other screen:\n"
		out += "  1. Return to this window.\n"
		out += "  2. Press Enter. Omnideck will check everything for you.\n"
		if plan.URL != "" {
			fallback := "If the page did not open, press o to try again or open this address yourself:"
			if plan.DirectDownload {
				fallback = "If the download did not start, press o to try again or open this address yourself:"
			}
			out += "\n  " + fallback + "\n"
			out += "  " + styles.Accent.Render(plan.URL) + "\n"
		}
		hints := "Enter — check again"
		if plan.URL != "" {
			if plan.DirectDownload {
				hints += "    o — download again"
			} else {
				hints += "    o — open page again"
			}
		}
		hints += "    b — back"
		out += "\n  " + styles.Dim.Render(hints) + "\n"
		return out
	}
	if m.runtimeSetupStage == runtimeSetupReview {
		out += "  " + styles.Dim.Render("STEP 2 OF 2") + "\n"
		out += "  " + styles.Active.Render("Review what will happen") + "\n\n"
		out += "  " + styles.Accent.Render(plan.Action) + "\n"
		out += "  " + styles.Dim.Render(plan.Description) + "\n\n"
		if len(plan.Commands) > 0 {
			out += "  After you press Enter, Omnideck will ask your computer to do these steps:\n"
		} else if plan.DirectDownload {
			out += "  After you press Enter, Omnideck will start the official download. Then you will:\n"
		} else {
			out += "  After you press Enter, Omnideck will open the official page. Then you will:\n"
		}
		for i, step := range plan.Steps {
			out += fmt.Sprintf("    %d. %s\n", i+1, step)
		}
		if plan.PermissionNote != "" {
			out += "\n  " + styles.Warning.Render("About password requests") + "\n"
			out += "  " + styles.Dim.Render(plan.PermissionNote) + "\n"
		}
		if plan.SafetyNote != "" {
			out += "\n  " + styles.Warning.Render("Good to know") + "\n"
			out += "  " + styles.Dim.Render(plan.SafetyNote) + "\n"
		}
		out += "\n  " + styles.Success.Render("Nothing will run until you press Enter.") + "\n"
		if m.runtimeShowDetails {
			out += "\n  " + styles.Dim.Render("Technical details") + "\n"
			for _, command := range plan.Commands {
				out += "    " + styles.Dim.Render("$ "+command.Display) + "\n"
			}
			if plan.URL != "" {
				out += "    " + styles.Dim.Render(plan.URL) + "\n"
			}
		}
		action := "start these steps"
		if len(plan.Commands) == 0 {
			action = "open official page"
			if plan.DirectDownload {
				action = "download installer"
			}
		}
		out += "\n  " + styles.Dim.Render("Enter — "+action+"    b — back    d — technical details") + "\n"
		return out
	}

	out += "  " + styles.Dim.Render("STEP 1 OF 2") + "\n"
	setupHeading := "Choose Podman or Docker"
	if len(m.runtimePlans) == 1 {
		setupHeading = "Set up " + m.runtimePlans[0].Title
	}
	out += "  " + styles.Active.Render(setupHeading) + "\n"
	out += "  " + styles.Dim.Render("Omnideck runs inside a container. The container keeps the agent and its software") + "\n"
	out += "  " + styles.Dim.Render("isolated from the rest of your system. Podman or Docker runs the container. You only need one.") + "\n\n"
	out += "  " + styles.Active.Render("What we found") + "\n"
	for _, probe := range m.runtimeProbes {
		name := "Docker"
		if probe.Name == "podman" {
			name = "Podman"
		}
		out += "    " + styles.Dim.Render(padRight(name, 12)+engine.RuntimeStateLabel(probe.State)) + "\n"
	}
	optionHeading := "Choose how to continue"
	if len(m.runtimePlans) == 1 {
		optionHeading = "Next step"
	}
	out += "\n  " + styles.Active.Render(optionHeading) + "\n"
	for i, option := range m.runtimePlans {
		cursor := "  "
		label := option.Action
		if option.Recommended {
			label += " — Recommended"
		}
		if i == m.runtimeChoice {
			cursor = "▸ "
			label = styles.Active.Render(label)
		} else {
			label = styles.Dim.Render(label)
		}
		out += "  " + styles.Accent.Render(cursor) + label + "\n"
		if i == m.runtimeChoice {
			out += "       " + styles.Dim.Render(option.Description) + "\n"
			if option.Recommendation != "" {
				out += "       " + styles.Success.Render(option.Recommendation) + "\n"
			}
		}
	}
	if m.runtimeMessage != "" {
		out += "\n  " + styles.Dim.Render(m.runtimeMessage) + "\n"
	}
	hints := "Enter — review    d — technical details    r — check again"
	if len(m.runtimePlans) > 1 {
		hints = "↑/↓ — choose    " + hints
	}
	if m.runtimeSetupEntry == runtimeSetupFromFirstRunChoice {
		hints += "    b — back"
	}
	out += "\n  " + styles.Dim.Render(hints) + "\n"
	return out
}

func (m InstallModel) viewError() string {
	out := "\n" + styles.Error.Render("  ✗  Omnideck could not finish setup") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
	out += "  It stopped while trying to: " + styles.Active.Render(m.errorMsg) + "\n"
	out += "  " + styles.Dim.Render("Any saved space already prepared will be reused if you try again.") + "\n\n"
	out += "  What you can do:\n"
	out += "    • Press r to review the setup and try again.\n"
	out += "    • Press b to return without trying again.\n"
	out += "    • Press d to show or hide details for support.\n"
	if m.errorShowDetails && m.errorDetail != "" {
		out += "\n  " + styles.Dim.Render("Details for support") + "\n"
		out += "  " + styles.Error.Render(m.errorDetail) + "\n"
	}
	return out
}

// isKeyMsg returns true if the message is any key press.
func isKeyMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyMsg)
	return ok
}

// --- Preflight commands ---

func runEngineCheck() tea.Msg {
	return runEngineCheckFor("")()
}

func runEngineCheckFor(preferred string) tea.Cmd {
	return func() tea.Msg {
		probes := engine.ProbeAll()
		usable := engine.ReadyEngines(probes)
		if preferred != "" {
			for _, eng := range usable {
				if eng.Name() == preferred {
					return engineCheckResult{eng: eng, all: usable, probes: probes}
				}
			}
			return engineCheckResult{all: usable, probes: probes, err: fmt.Errorf("%s is not ready", preferred)}
		}
		if len(usable) == 0 {
			return engineCheckResult{probes: probes, err: fmt.Errorf("neither Podman nor Docker is ready")}
		}
		return engineCheckResult{eng: readyEngineForSetup(usable, engine.DetectHostPlatform()), all: usable, probes: probes}
	}
}

func readyEngineForSetup(ready []engine.Engine, host engine.HostPlatform) engine.Engine {
	recommended := engine.RecommendedRuntime(host)
	for _, candidate := range ready {
		if candidate.Name() == recommended {
			return candidate
		}
	}
	if len(ready) > 0 {
		return ready[0]
	}
	return nil
}

func runPermissionCheck(eng engine.Engine) tea.Cmd {
	return func() tea.Msg {
		if !eng.HasPermission() {
			return permissionCheckResult{err: fmt.Errorf("permission denied")}
		}
		return permissionCheckResult{}
	}
}

func runOllamaCheck() tea.Msg {
	ok, host := checks.CheckOllama()
	return ollamaCheckResult{reachable: ok, host: host}
}

func runMemoryCheck() tea.Msg {
	mb, err := checks.AvailableMemoryMB()
	if err != nil {
		return memoryCheckResult{mb: 0, warning: "could not read memory"}
	}
	return memoryCheckResult{mb: mb, warning: checks.MemoryWarning(mb)}
}
