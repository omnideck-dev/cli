package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// StepStatus represents the state of a single installation step.
type StepStatus int

const (
	StepPending StepStatus = iota
	StepActive
	StepDone
	StepFailed
)

// Step is one item in the spinner list.
type Step struct {
	Label  string
	Status StepStatus
	Detail string
	Err    error
}

// StepStartedMsg signals that a step has begun.
type StepStartedMsg struct{ Index int }

// StepDoneMsg signals that a step completed successfully.
type StepDoneMsg struct {
	Index  int
	Detail string
}

// StepFailedMsg signals that a step failed.
type StepFailedMsg struct {
	Index int
	Err   error
}

// flavorTickMsg drives flavor message rotation.
type flavorTickMsg struct{}

// progressTickMsg drives the progress bar animation.
type progressTickMsg struct{}

func flavorTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return flavorTickMsg{} })
}

func progressTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return progressTickMsg{} })
}

// progressWaveFrames is an animated "wave" bar that loops while a step is active.
var progressWaveFrames = []string{
	"▱▱▱▱▱▱▱▱▱▱▱▱",
	"▰▱▱▱▱▱▱▱▱▱▱▱",
	"▰▰▱▱▱▱▱▱▱▱▱▱",
	"▰▰▰▱▱▱▱▱▱▱▱▱",
	"▰▰▰▰▱▱▱▱▱▱▱▱",
	"▰▰▰▰▰▱▱▱▱▱▱▱",
	"▱▰▰▰▰▰▱▱▱▱▱▱",
	"▱▱▰▰▰▰▰▱▱▱▱▱",
	"▱▱▱▰▰▰▰▰▱▱▱▱",
	"▱▱▱▱▰▰▰▰▰▱▱▱",
	"▱▱▱▱▱▰▰▰▰▰▱▱",
	"▱▱▱▱▱▱▰▰▰▰▰▱",
	"▱▱▱▱▱▱▱▰▰▰▰▰",
	"▱▱▱▱▱▱▱▱▰▰▰▰",
	"▱▱▱▱▱▱▱▱▱▰▰▰",
	"▱▱▱▱▱▱▱▱▱▱▰▰",
	"▱▱▱▱▱▱▱▱▱▱▱▰",
	"▱▱▱▱▱▱▱▱▱▱▱▱",
}

// SpinnerModel is a reusable Bubble Tea component for displaying a list of
// sequential steps with a spinner on the active step and faded completed steps.
type SpinnerModel struct {
	Steps         []Step
	CurrentStep   int
	FlavorMessages []string
	FlavorIndex   int
	progressIndex int
	spinner       spinner.Model
	done          bool
	err           error
}

// NewSpinnerModel creates a SpinnerModel with the given step labels and optional flavor messages.
func NewSpinnerModel(labels []string, flavors []string) SpinnerModel {
	steps := make([]Step, len(labels))
	for i, l := range labels {
		steps[i] = Step{Label: l, Status: StepPending}
	}
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styles.Accent
	return SpinnerModel{
		Steps:          steps,
		FlavorMessages: flavors,
		spinner:        sp,
	}
}

// Init starts the spinner tick.
func (m SpinnerModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, flavorTick(), progressTick())
}

// Update handles messages for the spinner component.
func (m SpinnerModel) Update(msg tea.Msg) (SpinnerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case flavorTickMsg:
		if len(m.FlavorMessages) > 0 {
			m.FlavorIndex = (m.FlavorIndex + 1) % len(m.FlavorMessages)
		}
		return m, flavorTick()

	case progressTickMsg:
		m.progressIndex = (m.progressIndex + 1) % len(progressWaveFrames)
		return m, progressTick()

	case StepStartedMsg:
		if msg.Index < len(m.Steps) {
			m.Steps[msg.Index].Status = StepActive
			m.CurrentStep = msg.Index
		}
		return m, nil

	case StepDoneMsg:
		if msg.Index < len(m.Steps) {
			m.Steps[msg.Index].Status = StepDone
			m.Steps[msg.Index].Detail = msg.Detail
		}
		if msg.Index+1 < len(m.Steps) {
			m.CurrentStep = msg.Index + 1
		} else {
			m.done = true
		}
		return m, nil

	case StepFailedMsg:
		if msg.Index < len(m.Steps) {
			m.Steps[msg.Index].Status = StepFailed
			m.Steps[msg.Index].Err = msg.Err
			m.err = msg.Err
		}
		return m, nil
	}
	return m, nil
}

const stepLabelWidth = 34

// View renders the spinner step list.
func (m SpinnerModel) View() string {
	var sb strings.Builder
	for _, step := range m.Steps {
		switch step.Status {
		case StepDone:
			label := padRight(step.Label, stepLabelWidth)
			line := "   " + styles.CheckMark + "  " + styles.Dim.Render(label)
			if step.Detail != "" {
				line += styles.Dim.Render(step.Detail)
			}
			sb.WriteString(line + "\n")

		case StepFailed:
			errText := ""
			if step.Err != nil {
				errText = step.Err.Error()
			}
			label := padRight(step.Label, stepLabelWidth)
			sb.WriteString("   " + styles.CrossMark + "  " + styles.Error.Render(label+errText) + "\n")

		case StepActive:
			flavor := step.Label
			if len(m.FlavorMessages) > 0 {
				flavor = m.FlavorMessages[m.FlavorIndex%len(m.FlavorMessages)]
			}
			sb.WriteString("   " + m.spinner.View() + "  " + styles.Active.Render(flavor) + "\n")
			// Animated progress bar on a second line for long-running steps.
			bar := progressWaveFrames[m.progressIndex%len(progressWaveFrames)]
			sb.WriteString("       " + styles.Accent.Render(bar) + "\n")

		case StepPending:
			// Pending steps are not shown; they appear once they become active.
		}
	}
	return sb.String()
}

// IsDone returns true when all steps have completed.
func (m SpinnerModel) IsDone() bool { return m.done }

// renderTNStep renders one wizard step in Tokyo Night style.
// Pending steps are shown dim so the user can see what is coming.
func renderTNStep(step Step, sm SpinnerModel) string {
	switch step.Status {
	case StepDone:
		label := styles.TNDimText.Render(step.Label)
		detail := ""
		if step.Detail != "" {
			detail = "  " + styles.TNFaintText.Render(step.Detail)
		}
		return styles.TNGreenTxt.Render("✓") + "  " + label + detail
	case StepFailed:
		errStr := ""
		if step.Err != nil {
			errStr = "  " + step.Err.Error()
		}
		return styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render(step.Label+errStr)
	case StepActive:
		wave := progressWaveFrames[sm.progressIndex%len(progressWaveFrames)]
		return sm.spinner.View() + "  " + styles.TNBlueTxt.Render(step.Label) + "\n" +
			"     " + styles.TNBlueTxt.Render(wave)
	default: // StepPending
		return styles.TNFaintText.Render("○  " + step.Label)
	}
}

// Error returns the first step error, if any.
func (m SpinnerModel) Error() error { return m.err }

// ActiveFlavor returns the current flavor message.
func (m SpinnerModel) ActiveFlavor() string {
	if len(m.FlavorMessages) == 0 {
		return ""
	}
	return m.FlavorMessages[m.FlavorIndex%len(m.FlavorMessages)]
}

// StepCount returns the number of steps.
func (m SpinnerModel) StepCount() int { return len(m.Steps) }

// StepCmd wraps a single step function into a tea.Cmd.
func StepCmd(index int, fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		detail, err := fn()
		if err != nil {
			return StepFailedMsg{Index: index, Err: err}
		}
		return StepDoneMsg{Index: index, Detail: detail}
	}
}

// defaultFlavorMessages are the cycling messages shown during image pulls.
var defaultFlavorMessages = []string{
	"Pulling from the mothership...",
	"Downloading the good stuff...",
	"This might take a minute...",
	"Almost there...",
	"Unpacking intelligence...",
}

func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s + "  "
	}
	return s + strings.Repeat(" ", n-w)
}
