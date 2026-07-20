package tui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// TNView renders the install wizard in Tokyo Night style.
// Called by DashboardModel.viewInstall() when Embedded == true.
func (m InstallModel) TNView(w, _ int) string {
	switch m.Phase {
	case PhasePreflight:
		return m.tnPreflight(w)
	case PhaseRuntimeSetup:
		return m.tnRuntimeSetup(w)
	case PhaseConfig:
		return m.tnConfig(w)
	case PhaseConfirm:
		return m.tnConfirm(w)
	case PhaseInstall:
		return m.tnInstall(w)
	case PhaseDone:
		return m.tnDone(w)
	case PhaseError:
		return m.tnError(w)
	}
	return ""
}

func (m InstallModel) tnRuntimeSetup(_ int) string {
	var sb strings.Builder
	if len(m.runtimePlans) == 0 {
		return "\n  " + styles.TNRedTxt.Render("Omnideck could not find a setup option for this computer.") + "\n  " + styles.TNDimText.Render("Press q to leave, then visit https://podman.io/docs/installation for help.")
	}
	plan := m.runtimePlans[m.runtimeChoice]

	if m.runtimeWaiting {
		sb.WriteString("\n  " + styles.TNTextBold.Render("Finish this step, then come back here") + "\n\n")
		sb.WriteString("  " + styles.TNDimText.Render(m.runtimeMessage) + "\n\n")
		sb.WriteString("  " + styles.TNTextSub.Render("When the installer, Podman, or Docker says it is ready:") + "\n")
		sb.WriteString("    1. Return to this window.\n")
		sb.WriteString("    2. Press Enter. Omnideck will check everything for you.\n")
		if plan.URL != "" {
			fallback := "If the page did not open, press o to try again or open this address yourself:"
			if plan.DirectDownload {
				fallback = "If the download did not start, press o to try again or open this address yourself:"
			}
			sb.WriteString("\n  " + styles.TNFaintText.Render(fallback) + "\n")
			sb.WriteString("  " + styles.TNBlueTxt.Render(plan.URL) + "\n")
		}
		return sb.String()
	}

	if m.runtimeConfirm {
		sb.WriteString("\n  " + styles.TNFaintText.Render("STEP 2 OF 2") + "\n")
		sb.WriteString("  " + styles.TNTextBold.Render("Review what will happen") + "\n\n")
		sb.WriteString("  " + styles.TNBlueTxt.Render(plan.Action) + "\n")
		sb.WriteString("  " + styles.TNDimText.Render(plan.Description) + "\n\n")
		if len(plan.Commands) > 0 {
			sb.WriteString("  " + styles.TNTextSub.Render("After you press Enter, Omnideck will ask your computer to do these steps:") + "\n")
		} else if plan.DirectDownload {
			sb.WriteString("  " + styles.TNTextSub.Render("After you press Enter, Omnideck will start the official download. Then you will:") + "\n")
		} else {
			sb.WriteString("  " + styles.TNTextSub.Render("After you press Enter, Omnideck will open the official page. Then you will:") + "\n")
		}
		for i, step := range plan.Steps {
			sb.WriteString(fmt.Sprintf("    %d. %s\n", i+1, step))
		}
		if plan.PermissionNote != "" {
			sb.WriteString("\n  " + styles.TNYellowTxt.Render("About password requests") + "\n")
			sb.WriteString("  " + styles.TNDimText.Render(plan.PermissionNote) + "\n")
		}
		if plan.SafetyNote != "" {
			sb.WriteString("\n  " + styles.TNYellowTxt.Render("Good to know") + "\n")
			sb.WriteString("  " + styles.TNDimText.Render(plan.SafetyNote) + "\n")
		}
		sb.WriteString("\n  " + styles.TNGreenTxt.Render("Nothing will run until you press Enter.") + "\n")
		if m.runtimeShowDetails {
			sb.WriteString("\n  " + styles.TNFaintText.Render("Technical details") + "\n")
			for _, command := range plan.Commands {
				sb.WriteString("    " + styles.TNFaintText.Render("$ "+command.Display) + "\n")
			}
			if plan.URL != "" {
				sb.WriteString("    " + styles.TNFaintText.Render(plan.URL) + "\n")
			}
		}
		return sb.String()
	}

	sb.WriteString("\n  " + styles.TNFaintText.Render("STEP 1 OF 2") + "\n")
	sb.WriteString("  " + styles.TNTextBold.Render("Choose Podman or Docker") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("Omnideck runs inside a container. The container keeps the agent and its") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("software isolated from the rest of your system. Podman or Docker runs") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("the container. You only need one of them.") + "\n\n")

	sb.WriteString("  " + styles.TNTextSub.Render("What we found on this computer") + "\n")
	for _, probe := range m.runtimeProbes {
		name := "Docker"
		if probe.Name == "podman" {
			name = "Podman"
		}
		sb.WriteString("    " + styles.TNDimText.Render(padRight(name, 12)) + styles.TNDimText.Render(engineStateForPeople(probe.State)) + "\n")
	}
	sb.WriteString("\n  " + styles.TNTextSub.Render("Choose how to continue") + "\n")

	for i, plan := range m.runtimePlans {
		cursor := "  "
		name := plan.Action
		if plan.Recommended {
			name += " — Recommended"
		}
		if i == m.runtimeChoice {
			cursor = styles.TNBlueTxt.Render("▸ ")
			name = styles.TNTextBold.Render(name)
		} else {
			name = styles.TNDimText.Render(name)
		}
		sb.WriteString("  " + cursor + name + "\n")
		if i == m.runtimeChoice {
			sb.WriteString("       " + styles.TNDimText.Render(plan.Description) + "\n")
			if plan.Recommendation != "" {
				sb.WriteString("       " + styles.TNGreenTxt.Render(plan.Recommendation) + "\n")
			}
			if m.runtimeShowDetails {
				sb.WriteString("       " + styles.TNFaintText.Render("Technical details") + "\n")
				for _, command := range plan.Commands {
					sb.WriteString("         " + styles.TNFaintText.Render("$ "+command.Display) + "\n")
				}
				if plan.URL != "" {
					sb.WriteString("         " + styles.TNFaintText.Render(plan.URL) + "\n")
				}
				if m.runtimeLastError != "" {
					sb.WriteString("         " + styles.TNRedTxt.Render(m.runtimeLastError) + "\n")
				}
			}
		}
	}
	if m.runtimeMessage != "" {
		style := styles.TNDimText
		if m.runtimeLastError != "" {
			style = styles.TNRedTxt
		}
		sb.WriteString("\n  " + style.Render(m.runtimeMessage) + "\n")
	}
	if m.runtimeBusy {
		sb.WriteString("\n  " + m.preflightSpinner.View() + " " + styles.TNDimText.Render("Working…") + "\n")
	}
	return sb.String()
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

func (m InstallModel) tnPreflight(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	type checkRow struct {
		label, detail  string
		ok, done, warn bool
	}
	var rows []checkRow

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, checkRow{"Podman or Docker", m.eng.Name() + " is ready", true, true, false})
	} else if engDone {
		rows = append(rows, checkRow{"Podman or Docker", "setup needed", false, true, false})
	} else {
		rows = append(rows, checkRow{"Podman or Docker", "", false, false, false})
	}

	if m.eng != nil {
		permDone := m.preflightDone >= 2
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
	if m.ollamaOK {
		ollamaDetail = "Ollama is ready"
	}
	rows = append(rows, checkRow{"Local AI (optional)", ollamaDetail, m.ollamaOK, ollamaDone, !m.ollamaOK && ollamaDone})

	memDone := m.memMB > 0
	memDetail := "checking…"
	if memDone {
		memDetail = fmt.Sprintf("%d MB", m.memMB)
	}
	rows = append(rows, checkRow{"Available memory", memDetail, m.memWarning == "", memDone, m.memWarning != "" && memDone})

	rows = append(rows, checkRow{"This computer", friendlyOS(runtime.GOOS), true, true, false})

	const labelW = 20
	for _, r := range rows {
		label := padRight(r.label, labelW)
		if !r.done {
			sb.WriteString("  " + m.preflightSpinner.View() + "  " + styles.TNDimText.Render(label+"checking…") + "\n")
		} else if r.warn {
			sb.WriteString("  " + styles.TNYellowTxt.Render("!") + "  " + styles.TNYellowTxt.Render(label+r.detail) + "\n")
		} else if r.ok {
			sb.WriteString("  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNDimText.Render(label+r.detail) + "\n")
		} else {
			sb.WriteString("  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render(label+r.detail) + "\n")
		}
	}
	if m.preflightReady && len(m.availableEngines) > 1 {
		sb.WriteString("\n  " + styles.TNTextSub.Render("Both Podman and Docker are ready.") + "\n")
		sb.WriteString("  " + styles.TNDimText.Render("Press Tab to switch, or Enter to continue with "+m.eng.Name()+".") + "\n")
	}
	return sb.String()
}

func (m InstallModel) tnConfig(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	if !m.configAdvanced {
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
	sb.WriteString(styles.TNKeyChip.Render("esc") + " " + styles.TNDimText.Render("cancel"))
	sb.WriteString("\n")

	_ = maxFieldW
	return sb.String()
}

func (m InstallModel) tnConfirm(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	kv := func(k, v string) string {
		return "  " + styles.TNDimText.Render(padRight(k, 16)) + styles.TNTextSub.Render(v) + "\n"
	}

	engName := "Unknown"
	if m.eng != nil {
		engName = m.eng.Name()
	}

	cfg := m.buildConfig()
	sb.WriteString("  " + styles.TNTextBold.Render("Ready to install Omnideck") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("Here is what Omnideck will do after you press Enter:") + "\n\n")
	sb.WriteString("    1. Download the Omnideck app.\n")
	sb.WriteString("    2. Prepare saved space for your files and settings.\n")
	sb.WriteString("    3. Start Omnideck at http://localhost:" + m.inputs[inputWebUIPort].Value() + ".\n")
	sb.WriteString("\n")
	sb.WriteString(kv("Runs with", engName))
	sb.WriteString(kv("Name", m.inputs[inputContainerName].Value()))
	sb.WriteString(kv("Memory", m.inputs[inputMemory].Value()))

	for _, warn := range m.confirmWarnings {
		sb.WriteString("\n  " + styles.TNYellowTxt.Render("⚠  ") + styles.TNDimText.Render(warn) + "\n")
	}

	if m.confirmShowDetails {
		sb.WriteString("\n  " + styles.TNFaintText.Render("Technical details") + "\n")
		sb.WriteString(kv("Computer", runtime.GOOS+" / "+runtime.GOARCH))
		sb.WriteString(kv("File storage", cfg.HomeVolumeName()))
		sb.WriteString(kv("App storage", cfg.StateVolumeName()))
		sb.WriteString(kv("Shared memory", m.inputs[inputShmSize].Value()))
		sb.WriteString(kv("Download", cfg.Image))
	}

	sb.WriteString("\n  " + styles.TNGreenTxt.Render("Press Enter to install. Nothing starts before then.") + "\n")

	_ = w
	return sb.String()
}

func (m InstallModel) tnInstall(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n")
	for _, step := range m.spinnerModel.Steps {
		sb.WriteString("  " + renderTNStep(step, m.spinnerModel) + "\n")
	}
	return sb.String()
}

func (m InstallModel) tnDone(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render("Installed successfully!") + "\n\n")

	sb.WriteString("  " + styles.TNDimText.Render("Open Omnideck in your browser:") + "\n")
	sb.WriteString("  " + styles.TNBlueTxt.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n\n")
	sb.WriteString("  " + styles.TNDimText.Render("Your files and settings will be kept when Omnideck updates.") + "\n")
	sb.WriteString("  " + styles.TNDimText.Render("Press any key to return to the dashboard.") + "\n")
	return sb.String()
}

func (m InstallModel) tnError(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render("Omnideck could not finish installing") + "\n\n")
	if m.errorMsg != "" {
		sb.WriteString("  It stopped while trying to: " + styles.TNTextSub.Render(m.errorMsg) + "\n")
	}
	sb.WriteString("  " + styles.TNDimText.Render("Any saved space already prepared will be reused if you try again.") + "\n\n")
	sb.WriteString("  " + styles.TNTextSub.Render("What you can do") + "\n")
	sb.WriteString("    • Press r to review the installation and try again.\n")
	sb.WriteString("    • Press b to return without trying again.\n")
	sb.WriteString("    • Press d to show or hide details for support.\n")
	if m.errorShowDetails && m.errorDetail != "" {
		sb.WriteString("\n  " + styles.TNFaintText.Render("Details for support") + "\n")
		sb.WriteString("  " + styles.TNRedTxt.Render(m.errorDetail) + "\n")
	}
	return sb.String()
}
