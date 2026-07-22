package tui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// TNView renders setup in Tokyo Night style.
// Called by AppModel.viewSetup() when Embedded == true.
func (m SetupModel) TNView(w, _ int) string {
	switch m.Stage {
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

func (m SetupModel) tnRuntimeSetup(w int) string {
	var sb strings.Builder
	if len(m.runtimePlans) == 0 {
		sb.WriteString("\n")
		name := "Podman or Docker"
		if m.preferredEngine != "" {
			name = runtimeNameForPeople(m.preferredEngine)
		}
		writeTNWrapped(&sb, w, "  ", "  ", "Omnideck still cannot use "+name, styles.TNTextBold)
		message := m.runtimeMessage
		if message == "" {
			message = "Omnideck could not determine another safe setup step. Make sure the app is installed, open it, and wait until it says it is running."
		}
		writeTNWrapped(&sb, w, "  ", "  ", message, styles.TNDimText)
		sb.WriteString("\n")
		if m.runtimeSetupStage == runtimeSetupWorking {
			sb.WriteString("  " + m.quickCheckSpinner.View() + " " + styles.TNDimText.Render("Checking again…") + "\n")
		} else {
			writeTNWrapped(&sb, w, "  ", "  ", "Press Enter to check again, or Esc to leave setup.", styles.TNGreenTxt)
		}
		return sb.String()
	}
	plan := m.runtimePlans[m.runtimeChoice]

	if m.runtimeSetupStage == runtimeSetupWaiting {
		sb.WriteString("\n  " + styles.TNTextBold.Render("Finish this step, then come back here") + "\n\n")
		writeTNWrapped(&sb, w, "  ", "  ", m.runtimeMessage, styles.TNDimText)
		sb.WriteString("\n")
		sb.WriteString("  " + styles.TNTextSub.Render("After you finish the step on the other screen:") + "\n")
		writeTNWrapped(&sb, w, "    1. ", "       ", "Return to this window.", styles.TNDimText)
		writeTNWrapped(&sb, w, "    2. ", "       ", "Press Enter. Omnideck will check everything for you.", styles.TNDimText)
		if plan.URL != "" {
			fallback := "If the page did not open, press o to try again or open this address yourself:"
			if plan.DirectDownload {
				fallback = "If the download did not start, press o to try again or open this address yourself:"
			}
			sb.WriteString("\n")
			writeTNWrapped(&sb, w, "  ", "  ", fallback, styles.TNFaintText)
			writeTNWrapped(&sb, w, "  ", "  ", plan.URL, styles.TNBlueTxt)
		}
		return sb.String()
	}

	if m.runtimeSetupStage == runtimeSetupReview {
		sb.WriteString("\n  " + styles.TNFaintText.Render("STEP 2 OF 2") + "\n")
		writeTNWrapped(&sb, w, "  ", "  ", plan.Action, styles.TNTextBold)
		writeTNWrapped(&sb, w, "  ", "  ", "Nothing starts until you press Enter.", styles.TNGreenTxt)
		sb.WriteString("\n  " + styles.TNTextSub.Render("What happens next") + "\n")
		for i, step := range plan.Steps {
			prefix := fmt.Sprintf("    %d. ", i+1)
			writeTNWrapped(&sb, w, prefix, strings.Repeat(" ", lipgloss.Width(prefix)), step, styles.TNDimText)
		}
		if plan.PermissionNote != "" {
			sb.WriteString("\n  " + styles.TNYellowTxt.Render("Permission or password") + "\n")
			writeTNWrapped(&sb, w, "  ", "  ", plan.PermissionNote, styles.TNDimText)
		}
		if plan.SafetyNote != "" {
			sb.WriteString("\n  " + styles.TNYellowTxt.Render("Before you continue") + "\n")
			writeTNWrapped(&sb, w, "  ", "  ", plan.SafetyNote, styles.TNDimText)
		}
		if m.runtimeShowDetails && m.runtimeDetailsAvailable() {
			heading := "Commands Omnideck will run"
			if m.runtimeLastError != "" {
				heading = "Details for support"
			}
			sb.WriteString("\n  " + styles.TNFaintText.Render(heading) + "\n")
			for _, command := range plan.Commands {
				writeTNWrapped(&sb, w, "    $ ", "      ", command.Display, styles.TNFaintText)
			}
			if m.runtimeLastError != "" {
				writeTNWrapped(&sb, w, "    ", "    ", m.runtimeLastError, styles.TNRedTxt)
			}
		}
		return sb.String()
	}

	sb.WriteString("\n  " + styles.TNFaintText.Render("STEP 1 OF 2") + "\n")
	setupHeading := "Choose Podman or Docker"
	if len(m.runtimePlans) == 1 {
		setupHeading = "Set up " + m.runtimePlans[0].Title
	}
	sb.WriteString("  " + styles.TNTextBold.Render(setupHeading) + "\n")
	sb.WriteString("\n  " + styles.TNTextSub.Render("Why this is needed") + "\n")
	writeTNWrapped(&sb, w, "  ", "  ", "Omnideck runs as a container. This keeps the agent and its software isolated from the rest of your system. Podman or Docker runs that container; you only need one.", styles.TNDimText)
	sb.WriteString("\n")

	sb.WriteString("  " + styles.TNTextSub.Render("This computer") + "\n")
	for _, probe := range m.runtimeProbes {
		name := "Docker"
		if probe.Name == "podman" {
			name = "Podman"
		}
		sb.WriteString("    " + styles.TNDimText.Render(padRight(name, 12)) + styles.TNDimText.Render(engineStateForPeople(probe.State)) + "\n")
	}
	optionHeading := "Choose one"
	if len(m.runtimePlans) == 1 {
		optionHeading = "Next step"
	}
	sb.WriteString("\n  " + styles.TNTextSub.Render(optionHeading) + "\n")

	for i, plan := range m.runtimePlans {
		name := plan.Action
		if plan.Recommended {
			name += " — Recommended"
		}
		prefix := "    "
		style := styles.TNDimText
		if i == m.runtimeChoice {
			prefix = "  " + styles.TNBlueTxt.Render("▸ ")
			style = styles.TNTextBold
		}
		writeTNWrapped(&sb, w, prefix, "    ", name, style)
		if i == m.runtimeChoice {
			writeTNWrapped(&sb, w, "      ", "      ", plan.Description, styles.TNDimText)
			if plan.Recommendation != "" {
				writeTNWrapped(&sb, w, "      ", "      ", plan.Recommendation, styles.TNGreenTxt)
			}
			if m.runtimeShowDetails && m.runtimeDetailsAvailable() {
				heading := "Commands Omnideck will run"
				if m.runtimeLastError != "" {
					heading = "Details for support"
				}
				sb.WriteString("      " + styles.TNFaintText.Render(heading) + "\n")
				for _, command := range plan.Commands {
					writeTNWrapped(&sb, w, "        $ ", "          ", command.Display, styles.TNFaintText)
				}
				if m.runtimeLastError != "" {
					writeTNWrapped(&sb, w, "        ", "        ", m.runtimeLastError, styles.TNRedTxt)
				}
			}
		}
	}
	if m.runtimeMessage != "" {
		style := styles.TNDimText
		if m.runtimeLastError != "" {
			style = styles.TNRedTxt
		}
		sb.WriteString("\n")
		writeTNWrapped(&sb, w, "  ", "  ", m.runtimeMessage, style)
	}
	if m.runtimeSetupStage == runtimeSetupWorking {
		sb.WriteString("\n  " + m.quickCheckSpinner.View() + " " + styles.TNDimText.Render("Working…") + "\n")
	}
	return sb.String()
}

func writeTNWrapped(sb *strings.Builder, width int, firstPrefix, continuationPrefix, value string, style lipgloss.Style) {
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

func engineStateForPeople(state engine.RuntimeState) string {
	return engine.RuntimeStateLabel(state)
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

func (m SetupModel) tnQuickCheck(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	type checkRow struct {
		label, detail  string
		ok, done, warn bool
	}
	var rows []checkRow

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, checkRow{"Podman or Docker", runtimeNameForPeople(m.eng.Name()) + " is ready", true, true, false})
	} else if engDone {
		rows = append(rows, checkRow{"Podman or Docker", "setup needed", false, true, false})
	} else {
		rows = append(rows, checkRow{"Podman or Docker", "", false, false, false})
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
			sb.WriteString("  " + m.quickCheckSpinner.View() + "  " + styles.TNDimText.Render(label+"checking…") + "\n")
		} else if r.warn {
			sb.WriteString("  " + styles.TNYellowTxt.Render("!") + "  " + styles.TNYellowTxt.Render(label+r.detail) + "\n")
		} else if r.ok {
			sb.WriteString("  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNDimText.Render(label+r.detail) + "\n")
		} else {
			sb.WriteString("  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render(label+r.detail) + "\n")
		}
	}
	if m.quickCheckReady && m.preferredEngine == "" && len(m.availableEngines) > 1 {
		sb.WriteString("\n  " + styles.TNTextSub.Render("Both Podman and Docker are ready.") + "\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press Tab to switch, or Enter to continue with "+runtimeNameForPeople(m.eng.Name())+".") + "\n")
	} else if m.quickCheckReady && m.preferredEngine == "" {
		if alternative := m.setupAlternativeRuntime(); alternative != "" {
			currentName := runtimeNameForPeople(m.eng.Name())
			alternativeName := runtimeNameForPeople(alternative)
			sb.WriteString("\n  " + styles.TNTextSub.Render("Choose Podman or Docker") + "\n")
			currentPrefix := "  " + styles.TNBlueTxt.Render("▸ ")
			alternativePrefix := "    "
			currentStyle := styles.TNTextBold
			alternativeStyle := styles.TNDimText
			if m.quickCheckAlternative != "" {
				currentPrefix = "    "
				alternativePrefix = "  " + styles.TNBlueTxt.Render("▸ ")
				currentStyle = styles.TNDimText
				alternativeStyle = styles.TNTextBold
			}
			sb.WriteString(currentPrefix + currentStyle.Render("Use "+currentName+" — Ready") + "\n")
			sb.WriteString(alternativePrefix + alternativeStyle.Render("Set up "+alternativeName+" instead") + "\n")
			sb.WriteString("\n  " + styles.TNFaintText.Render("Press Tab to choose, then Enter to continue.") + "\n")
		}
	}
	return sb.String()
}

func runtimeNameForPeople(name string) string {
	if name == "podman" {
		return "Podman"
	}
	if name == "docker" {
		return "Docker"
	}
	return name
}

func (m SetupModel) tnSettings(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	if !m.settingsAdvanced {
		sb.WriteString("  " + styles.TNTextBold.Render("Recommended settings are ready") + "\n")
		sb.WriteString("  " + styles.TNDimText.Render("Omnideck chose sensible settings for this computer. Most people can continue without changing anything.") + "\n\n")
		sb.WriteString("  " + styles.TNDimText.Render(padRight("Name", 18)) + styles.TNTextSub.Render(m.inputs[inputContainerName].Value()) + "\n")
		sb.WriteString("  " + styles.TNDimText.Render(padRight("Open in browser", 18)) + styles.TNTextSub.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n")
		sb.WriteString("  " + styles.TNDimText.Render(padRight("Memory", 18)) + styles.TNTextSub.Render(m.inputs[inputMemory].Value()+" — chosen for this computer") + "\n")
		sb.WriteString("\n  " + styles.TNGreenTxt.Render("Press Enter to use these settings.") + "\n")
		sb.WriteString("  " + styles.TNFaintText.Render("Choose Customize only if you need a different name, address, or memory limit.") + "\n")
		return sb.String()
	}

	sb.WriteString("  " + styles.TNTextBold.Render("Customize settings") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("The recommended values are already filled in. Press Esc at any time to return to the simple view.") + "\n\n")

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
			sb.WriteString("  " + styles.TNBlueTxt.Render("▸ ") + styles.TNTextBold.Render(label) + inp.View() + "\n")
			sb.WriteString("    " + styles.TNFaintText.Render(fieldDescs[i]) + "\n\n")
		} else {
			sb.WriteString("    " + styles.TNDimText.Render(label) + inp.View() + "\n")
		}
		if m.inputErrs[i] != "" {
			sb.WriteString("    " + styles.TNRedTxt.Render("✗ "+m.inputErrs[i]) + "\n")
		}
	}

	sb.WriteString("\n  ")
	sb.WriteString(styles.TNKeyChip.Render("tab") + " " + styles.TNDimText.Render("next") + "  ")
	sb.WriteString(styles.TNKeyChip.Render("shift+tab") + " " + styles.TNDimText.Render("back") + "  ")
	sb.WriteString(styles.TNKeyChip.Render("esc") + " " + styles.TNDimText.Render("use recommended settings"))
	sb.WriteString("\n")

	_ = maxFieldW
	return sb.String()
}

func (m SetupModel) tnReview(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	kv := func(k, v string) string {
		return "  " + styles.TNDimText.Render(padRight(k, 16)) + styles.TNTextSub.Render(v) + "\n"
	}

	engName := "Unknown"
	if m.eng != nil {
		engName = runtimeNameForPeople(m.eng.Name())
	}

	sb.WriteString("  " + styles.TNTextBold.Render("Ready to set up Omnideck") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("Here is what Omnideck will do after you press Enter:") + "\n\n")
	sb.WriteString("    1. Download the Omnideck app.\n")
	sb.WriteString("    2. Prepare saved space for your files and settings.\n")
	sb.WriteString("    3. Start Omnideck at http://localhost:" + m.inputs[inputWebUIPort].Value() + ".\n")
	sb.WriteString("\n")
	sb.WriteString(kv("Runs with", engName))
	sb.WriteString(kv("Name", m.inputs[inputContainerName].Value()))
	sb.WriteString(kv("Memory", m.inputs[inputMemory].Value()))

	for _, warn := range m.reviewWarnings {
		sb.WriteString("\n  " + styles.TNYellowTxt.Render("⚠  ") + styles.TNDimText.Render(warn) + "\n")
	}
	if m.windowsPodmanOllamaAwaitingCheck() {
		sb.WriteString("\n  " + styles.TNTextSub.Render("Local AI") + "\n")
		writeTNWrapped(&sb, w, "  ", "  ", "Ollama is running on Windows. After Omnideck starts, setup will check the real connection from inside Podman.", styles.TNDimText)
	}

	sb.WriteString("\n  " + styles.TNGreenTxt.Render("Press Enter to start setup. Nothing starts before then.") + "\n")

	_ = w
	return sb.String()
}

func (m SetupModel) tnApplying(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	for _, step := range m.spinnerModel.Steps {
		sb.WriteString("  " + renderTNStep(step, m.spinnerModel) + "\n")
	}
	return sb.String()
}

func (m SetupModel) tnComplete(w int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render("Omnideck is ready!") + "\n\n")

	sb.WriteString("  " + styles.TNDimText.Render("Open Omnideck in your browser:") + "\n")
	sb.WriteString("  " + styles.TNBlueTxt.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n\n")
	sb.WriteString("  " + styles.TNDimText.Render("Your files and settings will be kept when Omnideck updates.") + "\n")
	if m.windowsPodmanOllamaNeedsSetup() {
		writeWindowsPodmanOllamaSteps(&sb, w)
	}
	sb.WriteString("  " + styles.TNDimText.Render("Press any key to return to the dashboard.") + "\n")
	return sb.String()
}

func writeWindowsPodmanOllamaSteps(sb *strings.Builder, w int) {
	sb.WriteString("\n  " + styles.TNYellowTxt.Render("Local AI needs one Windows setting") + "\n")
	writeTNWrapped(sb, w, "  ", "  ", "Omnideck checked from inside Podman and could not connect to Ollama on Windows.", styles.TNDimText)
	steps := []string{
		"Quit Ollama from the small icons near the Windows clock.",
		"Open the Start menu, search for environment variables, and choose Edit environment variables for your account.",
		"Under User variables, select New. Enter OLLAMA_HOST as the name and 0.0.0.0:11434 as the value.",
		"Select OK, then open Ollama again from the Start menu.",
	}
	for i, step := range steps {
		prefix := fmt.Sprintf("    %d. ", i+1)
		writeTNWrapped(sb, w, prefix, strings.Repeat(" ", lipgloss.Width(prefix)), step, styles.TNDimText)
	}
	writeTNWrapped(sb, w, "  ", "  ", "This setting can let other computers reach Ollama if Windows Firewall allows it. Do not allow access on public networks. Online AI works without this setting.", styles.TNFaintText)
	sb.WriteString("\n")
}

func (m SetupModel) tnFailed(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render("Omnideck could not finish setup") + "\n\n")
	if m.errorMsg != "" {
		sb.WriteString("  It stopped while trying to: " + styles.TNTextSub.Render(m.errorMsg) + "\n")
	}
	sb.WriteString("  " + styles.TNDimText.Render("Any saved space already prepared will be reused if you try again.") + "\n\n")
	sb.WriteString("  " + styles.TNTextSub.Render("What you can do") + "\n")
	sb.WriteString("    • Press r to review the setup and try again.\n")
	sb.WriteString("    • Press Esc to return without trying again.\n")
	sb.WriteString("    • Press d to show or hide details for support.\n")
	if m.errorShowDetails && m.errorDetail != "" {
		sb.WriteString("\n  " + styles.TNFaintText.Render("Details for support") + "\n")
		sb.WriteString("  " + styles.TNRedTxt.Render(m.errorDetail) + "\n")
	}
	return sb.String()
}
