package tui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/omnideck-dev/cli/styles"
)

// TNView renders the install wizard in Tokyo Night style.
// Called by DashboardModel.viewInstall() when Embedded == true.
func (m InstallModel) TNView(w, _ int) string {
	switch m.Phase {
	case PhasePreflight:
		return m.tnPreflight(w)
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

func (m InstallModel) tnPreflight(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	type checkRow struct {
		label, detail string
		ok, done, warn bool
	}
	var rows []checkRow

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, checkRow{"Container engine", m.eng.Name(), true, true, false})
	} else if engDone {
		rows = append(rows, checkRow{"Container engine", "not found", false, true, false})
	} else {
		rows = append(rows, checkRow{"Container engine", "", false, false, false})
	}

	permDone := m.preflightDone >= 2
	if permDone {
		rows = append(rows, checkRow{"Permissions", "ok", m.permErr == nil, true, m.permErr != nil})
	} else if m.eng != nil {
		rows = append(rows, checkRow{"Permissions", "", false, false, false})
	}

	ollamaDone := m.ollamaHost != ""
	ollamaDetail := "not found — install later"
	if m.ollamaOK {
		ollamaDetail = "reachable at " + m.ollamaHost
	}
	rows = append(rows, checkRow{"Ollama :11434", ollamaDetail, m.ollamaOK, ollamaDone, !m.ollamaOK && ollamaDone})

	memDone := m.memMB > 0
	memDetail := "checking…"
	if memDone {
		memDetail = fmt.Sprintf("%d MB", m.memMB)
	}
	rows = append(rows, checkRow{"Memory", memDetail, m.memWarning == "", memDone, m.memWarning != "" && memDone})

	rows = append(rows, checkRow{"OS / Arch", runtime.GOOS + " / " + runtime.GOARCH, true, true, false})

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
	return sb.String()
}

func (m InstallModel) tnConfig(w int) string {
	var sb strings.Builder
	sb.WriteString("\n")

	fieldNames := []string{"Container name", "Shared directory", "Memory limit", "SHM size", "Web UI port"}
	fieldDescs := []string{
		"Docker/Podman container name",
		"Host path mounted into container",
		"RAM limit  e.g. 2g, 4g",
		"Shared memory  e.g. 512m",
		"Host port  e.g. 2337",
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

	engName := "unknown"
	if m.eng != nil {
		engName = m.eng.Name()
	}

	sharedDir := m.inputs[inputSharedDir].Value()
	sb.WriteString(kv("Engine", engName))
	sb.WriteString(kv("OS / Arch", runtime.GOOS+" / "+runtime.GOARCH))
	sb.WriteString("\n")
	sb.WriteString(kv("Container name", m.inputs[inputContainerName].Value()))
	sb.WriteString(kv("Shared dir", sharedDir))
	sb.WriteString(kv("State dir", expandTilde(sharedDir)+"/.state"))
	sb.WriteString(kv("Memory", m.inputs[inputMemory].Value()))
	sb.WriteString(kv("SHM size", m.inputs[inputShmSize].Value()))
	sb.WriteString(kv("Web UI port", ":"+m.inputs[inputWebUIPort].Value()))

	for _, warn := range m.confirmWarnings {
		sb.WriteString("\n  " + styles.TNYellowTxt.Render("⚠  ") + styles.TNDimText.Render(warn) + "\n")
	}

	sb.WriteString("\n  ")
	sb.WriteString(styles.TNKeyChip.Render("i") + " " + styles.TNGreenTxt.Render("install") + "  ")
	sb.WriteString(styles.TNKeyChip.Render("b") + " " + styles.TNDimText.Render("back") + "  ")
	sb.WriteString(styles.TNKeyChip.Render("q") + " " + styles.TNDimText.Render("cancel"))
	sb.WriteString("\n")

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

	kv := func(k, v string) string {
		return "  " + styles.TNDimText.Render(padRight(k, 12)) + styles.TNTextSub.Render(v) + "\n"
	}
	sb.WriteString(kv("Web UI", "http://localhost:"+m.inputs[inputWebUIPort].Value()))
	sb.WriteString(kv("Shared dir", m.inputs[inputSharedDir].Value()))
	sb.WriteString(kv("Config", m.configPath))

	sb.WriteString("\n  " + styles.TNDimText.Render("Press any key to return to dashboard.") + "\n")
	return sb.String()
}

func (m InstallModel) tnError(_ int) string {
	var sb strings.Builder
	sb.WriteString("\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNRedTxt.Render("Installation failed") + "\n\n")
	if m.errorMsg != "" {
		sb.WriteString("     " + styles.TNDimText.Render(m.errorMsg) + "\n\n")
	}
	sb.WriteString("  " + styles.TNDimText.Render("Press any key to return.") + "\n")
	return sb.String()
}
