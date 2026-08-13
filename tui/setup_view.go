package tui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

const (
	setupPreparingTitle  = "Preparing your environment"
	setupPreparingDetail = "Setting omnideck up on this computer. This usually takes a few minutes."
	setupReadyTitle      = "omnideck is ready"
	setupReadyDetail     = "Everything is prepared. Open omnideck whenever you’re ready."
)

func writeSetupIntro(sb *strings.Builder, w int, activity string) {
	writeTUIWrapped(sb, w, "  ", "  ", setupPreparingTitle, styles.TUIPrimaryBold)
	writeTUIWrapped(sb, w, "  ", "  ", setupPreparingDetail, styles.TUISecondaryText)
	if activity != "" {
		sb.WriteString("\n")
		writeTUIWrapped(sb, w, "  ", "  ", activity, styles.TUIPrimaryText)
	}
}

// TUIView renders setup with Omnideck's SIGNAL dark theme.
// Called by AppModel.viewSetup() when Embedded == true.
func (m SetupModel) TUIView(w, _ int) string {
	switch m.Stage {
	case SetupStageWelcome:
		return m.tnWelcome(w)
	case SetupStageQuickCheck:
		return m.tnQuickCheck(w)
	case SetupStageRuntime:
		return m.tnRuntimeSetup(w)
	case SetupStageSettings:
		return m.tnSettings(w)
	case SetupStageReview:
		return m.tnReview(w)
	case SetupStageApplying:
		return m.tnApplying(w)
	case SetupStageComplete:
		return m.tnComplete(w)
	case SetupStageFailed:
		return m.tnFailed(w)
	}
	return ""
}

func (m SetupModel) tnWelcome(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	writeTUIWrapped(&sb, w, "  ", "  ", "Welcome to omnideck", styles.TUIPrimaryBold)
	writeTUIWrapped(&sb, w, "  ", "  ", "A one-time setup will prepare everything omnideck needs on this computer.", styles.TUISecondaryText)
	sb.WriteString("\n")
	writeTUIWrapped(&sb, w, "  ", "  ", "Press Enter to set up omnideck.", styles.TUISuccessText)
	writeTUIWrapped(&sb, w, "  ", "  ", "omnideck will detect what this computer needs and guide the entire setup.", styles.TUISubtleText)
	return sb.String()
}

func (m SetupModel) tnRuntimeSetup(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	if m.runtimeSetupStage == runtimeSetupRestart {
		writeTUIWrapped(&sb, w, "  ", "  ", "Restart needed", styles.TUIPrimaryBold)
		writeTUIWrapped(&sb, w, "  ", "  ", "Windows must restart to finish enabling required features. Save any open work, then restart now or later. If you restart now, omnideck reopens after you sign in and continues setup.", styles.TUISecondaryText)
		sb.WriteString("\n")
		writeTUIWrapped(&sb, w, "  > ", "    ", "Press Enter to restart now", styles.TUISuccessText)
		writeTUIWrapped(&sb, w, "    ", "    ", "Press l to restart later", styles.TUISubtleText)
		return sb.String()
	}

	title := setupPreparingTitle
	detail := setupPreparingDetail
	if m.runtimeState == "permission" {
		title = "Waiting for your permission"
		detail = m.runtimeDetail
	} else if m.runtimeState == "waiting" {
		title = "Setup is still working"
		detail = m.runtimeDetail
	}
	writeTUIWrapped(&sb, w, "  ", "  ", title, styles.TUIPrimaryBold)
	writeTUIWrapped(&sb, w, "  ", "  ", detail, styles.TUISecondaryText)
	if m.runtimeActivity != "" {
		sb.WriteString("\n")
		writeTUIWrapped(&sb, w, "  ", "  ", m.runtimeActivity, styles.TUIPrimaryText)
	}
	sb.WriteString("\n")

	softwareDone := m.runtimeStage == engine.SetupStageEnvironment ||
		m.runtimeStage == engine.SetupStageSoftware && m.runtimeState == "done"
	softwareActive := !softwareDone && m.runtimeStage == engine.SetupStageSoftware
	writeSetupPhaseRow(&sb, "Computer setup", softwareDone, softwareActive, m.spinnerModel)
	if m.hostPlatform.OS != "linux" {
		environmentDone := m.runtimeStage == engine.SetupStageEnvironment && m.runtimeState == "done"
		environmentActive := m.runtimeStage == engine.SetupStageEnvironment && !environmentDone
		writeSetupPhaseRow(&sb, "Secure space", environmentDone, environmentActive, m.spinnerModel)
	}
	writeSetupPhaseRow(&sb, "Application files", false, false, m.spinnerModel)
	writeSetupPhaseRow(&sb, "Final checks", false, false, m.spinnerModel)

	writeSetupProgressBar(&sb, runtimeOverallProgress(m), 28)
	if m.runtimeDetail != "" && m.runtimeState != "permission" && m.runtimeState != "waiting" {
		sb.WriteString("\n")
		writeTUIWrapped(&sb, w, "  ", "  ", m.runtimeDetail, styles.TUISubtleText)
	}
	return sb.String()
}

func runtimeOverallProgress(m SetupModel) float64 {
	softwareWeight := 25.0
	environmentWeight := 15.0
	total := 100.0
	if m.hostPlatform.OS == "linux" {
		environmentWeight = 0
		total = 85
	}
	fraction := 0.0
	if m.runtimeProgress != nil {
		fraction = max(0, min(1, *m.runtimeProgress))
	}
	score := 0.0
	if m.runtimeStage == engine.SetupStageEnvironment {
		score = softwareWeight + environmentWeight*fraction
	} else {
		score = softwareWeight * fraction
		if m.runtimeState == "done" {
			score = softwareWeight
		}
	}
	return score / total
}

func writeSetupProgressBar(sb *strings.Builder, fraction float64, width int) {
	fraction = max(0, min(1, fraction))
	filled := int(fraction * float64(width))
	bar := strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
	sb.WriteString("\n  " + styles.TUIAccentText.Render(bar) + " " + styles.TUISubtleText.Render(fmt.Sprintf("%d%%", int(fraction*100))) + "\n")
}

func writeTUIWrapped(sb *strings.Builder, width int, firstPrefix, continuationPrefix, value string, style lipgloss.Style) {
	firstWidth := max(1, width-lipgloss.Width(firstPrefix))
	continuationWidth := max(1, width-lipgloss.Width(continuationPrefix))
	for i, line := range wrapWords(value, firstWidth, continuationWidth) {
		prefix := continuationPrefix
		if i == 0 {
			prefix = firstPrefix
		}
		sb.WriteString(prefix + style.Render(line) + "\n")
	}
}

func friendlyOS(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func (m SetupModel) tnQuickCheck(w int) string {
	if m.setupMode == SetupFirstRun {
		var sb strings.Builder
		sb.WriteString("\n")
		writeSetupIntro(&sb, w, engine.SetupActivitySoftware)
		sb.WriteString("\n")
		spinner := m.spinnerModel
		spinner.spinner = m.quickCheckSpinner
		writeSetupPhaseRow(&sb, "Computer setup", false, true, spinner)
		if m.hostPlatform.OS != "linux" {
			writeSetupPhaseRow(&sb, "Secure space", false, false, spinner)
		}
		writeSetupPhaseRow(&sb, "Application files", false, false, spinner)
		writeSetupPhaseRow(&sb, "Final checks", false, false, spinner)
		writeSetupProgressBar(&sb, 0, 28)
		return sb.String()
	}

	var sb strings.Builder
	sb.WriteString("\n")
	writeSetupIntro(&sb, w, "Getting your computer ready…")
	sb.WriteString("\n")

	type checkRow struct {
		label, detail  string
		ok, done, warn bool
	}
	var rows []checkRow

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, checkRow{"Secure space", "Podman is ready", true, true, false})
	} else if engDone {
		rows = append(rows, checkRow{"Secure space", "computer setup needed", false, true, false})
	} else {
		rows = append(rows, checkRow{"Secure space", "", false, false, false})
	}

	if m.eng != nil {
		permDone := m.quickCheckDone >= 2
		if permDone {
			detail := "your account can use it"
			if m.permErr != nil {
				detail = "your account needs access"
			}
			rows = append(rows, checkRow{"Account access", detail, m.permErr == nil, true, m.permErr != nil})
		} else {
			rows = append(rows, checkRow{"Account access", "", false, false, false})
		}
	}

	ollamaDone := m.ollamaHost != ""
	ollamaDetail := "not found — you can add it later"
	ollamaWarn := !m.ollamaOK && ollamaDone
	if m.ollamaOK {
		ollamaDetail = "Ollama is running on this computer"
		if m.windowsPodmanOllamaAwaitingCheck() {
			ollamaDetail = "running on Windows — connection checked after start"
		}
	}
	rows = append(rows, checkRow{"Local AI (optional)", ollamaDetail, m.ollamaOK && !ollamaWarn, ollamaDone, ollamaWarn})

	memDone := m.memChecked
	memDetail := m.memWarning
	if m.memMB > 0 {
		memDetail = fmt.Sprintf("%d MB", m.memMB)
	} else if memDone && memDetail == "" {
		memDetail = "could not read memory"
	}
	rows = append(rows, checkRow{"Available memory", memDetail, m.memWarning == "", memDone, m.memWarning != "" && memDone})

	rows = append(rows, checkRow{"This computer", friendlyOS(runtime.GOOS), true, true, false})

	const labelW = 20
	for _, r := range rows {
		label := padRight(r.label, labelW)
		if !r.done {
			sb.WriteString("  " + m.quickCheckSpinner.View() + "  " + styles.TUISecondaryText.Render(label+"checking…") + "\n")
		} else if r.warn {
			sb.WriteString("  " + styles.TUIWarningText.Render("!") + "  " + styles.TUIWarningText.Render(label+r.detail) + "\n")
		} else if r.ok {
			sb.WriteString("  " + styles.TUISuccessText.Render("✓") + "  " + styles.TUISecondaryText.Render(label+r.detail) + "\n")
		} else {
			sb.WriteString("  " + styles.TUIDangerText.Render("✗") + "  " + styles.TUIDangerText.Render(label+r.detail) + "\n")
		}
	}
	return sb.String()
}

func runtimeNameForPeople(name string) string {
	if name == "podman" {
		return "Podman"
	}
	return name
}

func (m SetupModel) tnSettings(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	if !m.settingsAdvanced {
		sb.WriteString("  " + styles.TUIPrimaryBold.Render("Recommended settings are ready") + "\n")
		sb.WriteString("  " + styles.TUISecondaryText.Render("Omnideck chose sensible settings for this computer. Most people can continue without changing anything.") + "\n\n")
		sb.WriteString("  " + styles.TUISecondaryText.Render(padRight("Name", 18)) + styles.TUIPrimaryText.Render(m.inputs[inputContainerName].Value()) + "\n")
		sb.WriteString("  " + styles.TUISecondaryText.Render(padRight("Open in browser", 18)) + styles.TUIPrimaryText.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n")
		sb.WriteString("  " + styles.TUISecondaryText.Render(padRight("Memory", 18)) + styles.TUIPrimaryText.Render(m.inputs[inputMemory].Value()+" — chosen for this computer") + "\n")
		sb.WriteString("\n  " + styles.TUISuccessText.Render("Press Enter to use these settings.") + "\n")
		sb.WriteString("  " + styles.TUISubtleText.Render("Choose Customize only if you need a different name, address, or memory limit.") + "\n")
		return sb.String()
	}

	sb.WriteString("  " + styles.TUIPrimaryBold.Render("Customize settings") + "\n")
	sb.WriteString("  " + styles.TUISecondaryText.Render("The recommended values are already filled in. Press Esc at any time to return to the simple view.") + "\n\n")

	fieldNames := []string{"Omnideck name", "Memory limit", "Shared memory", "Browser address number"}
	fieldDescs := []string{
		"A label for this installation. Most people can keep the suggested name.",
		"The most memory Omnideck may use. For example: 2g or 4g.",
		"Temporary working space used by some Omnideck features. The suggested value is recommended.",
		"The number at the end of the local web address. Change it only if another app already uses it.",
	}

	maxFieldW := w - 22
	if maxFieldW < 20 {
		maxFieldW = 20
	}

	for i, inp := range m.inputs {
		active := i == m.inputFocus
		label := padRight(fieldNames[i], 18)
		if active {
			sb.WriteString("  " + styles.TUIAccentText.Render("▸ ") + styles.TUIPrimaryBold.Render(label) + inp.View() + "\n")
			sb.WriteString("    " + styles.TUISubtleText.Render(fieldDescs[i]) + "\n\n")
		} else {
			sb.WriteString("    " + styles.TUISecondaryText.Render(label) + inp.View() + "\n")
		}
		if m.inputErrs[i] != "" {
			sb.WriteString("    " + styles.TUIDangerText.Render("✗ "+m.inputErrs[i]) + "\n")
		}
	}

	sb.WriteString("\n  ")
	sb.WriteString(styles.TUIKeyChip.Render("tab") + " " + styles.TUISecondaryText.Render("next") + "  ")
	sb.WriteString(styles.TUIKeyChip.Render("shift+tab") + " " + styles.TUISecondaryText.Render("back") + "  ")
	sb.WriteString(styles.TUIKeyChip.Render("esc") + " " + styles.TUISecondaryText.Render("use recommended settings"))
	sb.WriteString("\n")

	_ = maxFieldW
	return sb.String()
}

func (m SetupModel) tnReview(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	kv := func(k, v string) string {
		return "  " + styles.TUISecondaryText.Render(padRight(k, 16)) + styles.TUIPrimaryText.Render(v) + "\n"
	}

	engName := "Unknown"
	if m.eng != nil {
		engName = runtimeNameForPeople(m.eng.Name())
	}

	sb.WriteString("  " + styles.TUIPrimaryBold.Render("Ready to set up Omnideck") + "\n")
	sb.WriteString("  " + styles.TUISecondaryText.Render("Here is what Omnideck will do after you press Enter:") + "\n\n")
	sb.WriteString("    1. Download the Omnideck app.\n")
	sb.WriteString("    2. Prepare saved space for your files and settings.\n")
	sb.WriteString("    3. Start Omnideck at http://localhost:" + m.inputs[inputWebUIPort].Value() + ".\n")
	sb.WriteString("\n")
	sb.WriteString(kv("Runs with", engName))
	sb.WriteString(kv("Name", m.inputs[inputContainerName].Value()))
	sb.WriteString(kv("Memory", m.inputs[inputMemory].Value()))

	for _, warn := range m.reviewWarnings {
		sb.WriteString("\n  " + styles.TUIWarningText.Render("⚠  ") + styles.TUISecondaryText.Render(warn) + "\n")
	}
	if m.windowsPodmanOllamaAwaitingCheck() {
		sb.WriteString("\n  " + styles.TUIPrimaryText.Render("Local AI") + "\n")
		writeTUIWrapped(&sb, w, "  ", "  ", "Ollama is running on Windows. After Omnideck starts, setup will check the real connection from inside Podman.", styles.TUISecondaryText)
	}

	sb.WriteString("\n  " + styles.TUISuccessText.Render("Press Enter to start setup. Nothing starts before then.") + "\n")

	_ = w
	return sb.String()
}

func (m SetupModel) tnApplying(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	activity := "Downloading omnideck’s files…"
	if m.spinnerModel.CurrentStep >= setupStepContainer {
		activity = "Almost ready…"
	}
	writeSetupIntro(&sb, w, activity)
	sb.WriteString("\n")

	applicationDone := m.spinnerModel.CurrentStep > setupStepImage
	writeSetupPhaseRow(&sb, "Computer setup", true, false, m.spinnerModel)
	if m.hostPlatform.OS != "linux" {
		writeSetupPhaseRow(&sb, "Secure space", true, false, m.spinnerModel)
	}
	writeSetupPhaseRow(&sb, "Application files", applicationDone, !applicationDone, m.spinnerModel)
	writeSetupPhaseRow(&sb, "Final checks", false, applicationDone, m.spinnerModel)
	writeSetupProgressBar(&sb, applyingOverallProgress(m), 28)

	if current := m.spinnerModel.CurrentStep; current >= 0 && current < len(m.spinnerModel.Steps) {
		sb.WriteString("\n")
		writeTUIWrapped(&sb, w, "  ", "  ", m.spinnerModel.Steps[current].Label, styles.TUISubtleText)
	}
	return sb.String()
}

func applyingOverallProgress(m SetupModel) float64 {
	softwareAndEnvironment := 0.40
	downloadDone := 0.90
	if m.hostPlatform.OS == "linux" {
		softwareAndEnvironment = 25.0 / 85.0
		downloadDone = 75.0 / 85.0
	}
	current := m.spinnerModel.CurrentStep
	if current <= setupStepImage {
		return softwareAndEnvironment
	}
	startupSteps := float64(setupStepSave - setupStepContainer + 1)
	completed := float64(current - setupStepContainer)
	return min(1, downloadDone+(1-downloadDone)*(completed/startupSteps))
}

func writeSetupPhaseRow(sb *strings.Builder, label string, done, active bool, spinner SpinnerModel) {
	switch {
	case done:
		sb.WriteString("  " + styles.TUISuccessText.Render("✓") + "  " + styles.TUISecondaryText.Render(padRight(label, 22)+"Done") + "\n")
	case active:
		sb.WriteString("  " + spinner.spinner.View() + "  " + styles.TUIAccentText.Render(label) + "\n")
	default:
		sb.WriteString("  " + styles.TUISubtleText.Render("○  "+label+"  Not started") + "\n")
	}
}

func (m SetupModel) tnComplete(w int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TUISuccessText.Render("✓") + "  " + styles.TUIPrimaryBold.Render(setupReadyTitle) + "\n")
	writeTUIWrapped(&sb, w, "  ", "  ", setupReadyDetail, styles.TUISecondaryText)
	sb.WriteString("\n")

	sb.WriteString("  " + styles.TUISecondaryText.Render("Open Omnideck in your browser:") + "\n")
	sb.WriteString("  " + styles.TUIAccentText.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n\n")
	sb.WriteString("  " + styles.TUISecondaryText.Render("Your files and settings will be kept when Omnideck updates.") + "\n")
	if m.windowsPodmanOllamaNeedsSetup() {
		writeWindowsPodmanOllamaSteps(&sb, w)
	}
	sb.WriteString("  " + styles.TUISecondaryText.Render("Press any key to return to the dashboard.") + "\n")
	return sb.String()
}

func writeWindowsPodmanOllamaSteps(sb *strings.Builder, w int) {
	sb.WriteString("\n  " + styles.TUIWarningText.Render("Local AI needs one Windows setting") + "\n")
	writeTUIWrapped(sb, w, "  ", "  ", "Omnideck checked from inside Podman and could not connect to Ollama on Windows.", styles.TUISecondaryText)
	steps := []string{
		"Quit Ollama from the small icons near the Windows clock.",
		"Open the Start menu, search for environment variables, and choose Edit environment variables for your account.",
		"Under User variables, select New. Enter OLLAMA_HOST as the name and 0.0.0.0:11434 as the value.",
		"Select OK, then open Ollama again from the Start menu.",
	}
	for i, step := range steps {
		prefix := fmt.Sprintf("    %d. ", i+1)
		writeTUIWrapped(sb, w, prefix, strings.Repeat(" ", lipgloss.Width(prefix)), step, styles.TUISecondaryText)
	}
	writeTUIWrapped(sb, w, "  ", "  ", "This setting can let other computers reach Ollama if Windows Firewall allows it. Do not allow access on public networks. Online AI works without this setting.", styles.TUISubtleText)
	sb.WriteString("\n")
}

func (m SetupModel) tnFailed(w int) string {
	var sb strings.Builder
	title := "Setup didn’t finish"
	detail := "Something stopped setup before it completed. Try again, or review the diagnostic details if it keeps happening."
	if m.spinnerModel.CurrentStep == setupStepImage {
		title = "The download didn’t finish"
		detail = "Check your internet connection and try again. Anything already downloaded is kept."
	} else if m.spinnerModel.CurrentStep >= setupStepContainer {
		title = "omnideck didn’t finish starting"
		detail = "Everything installed, but omnideck did not answer in time. Trying again runs the startup checks."
	}
	sb.WriteString("\n  " + styles.TUIDangerText.Render("✗") + "  " + styles.TUIDangerText.Render(title) + "\n")
	writeTUIWrapped(&sb, w, "  ", "  ", detail, styles.TUISecondaryText)
	sb.WriteString("\n")
	if m.errorMsg != "" {
		sb.WriteString("  It stopped while trying to: " + styles.TUIPrimaryText.Render(m.errorMsg) + "\n")
	}
	if m.errorShowDetails && m.errorDetail != "" {
		sb.WriteString("\n  " + styles.TUISubtleText.Render("Details to share when reporting this problem") + "\n")
		writeTUIWrapped(&sb, w, "  ", "  ", m.errorDetail, styles.TUIDangerText)
	}
	sb.WriteString("  " + styles.TUISecondaryText.Render("Any saved space already prepared will be reused if you try again.") + "\n\n")
	sb.WriteString("  " + styles.TUIPrimaryText.Render("What you can do") + "\n")
	sb.WriteString("    • Press r to review the setup and try again.\n")
	sb.WriteString("    • Press Esc to return without trying again.\n")
	if m.errorDetail != "" {
		sb.WriteString("    • Press d to hide or show the details above.\n")
	}
	return sb.String()
}
