package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// --- Doctor screen ---

func (m AppModel) viewDoctor() string {
	w := m.width

	innerW := 92
	if w-4 < innerW {
		innerW = w - 4
	}
	if innerW < 20 {
		innerW = 20
	}
	header := styles.TNCyanTxt.Render("✚") + "  " + styles.TNTextBold.Render("Doctor") + "  " + styles.TNFaintText.Render("checks what Omnideck needs")
	sep := styles.TNFaintText.Render(strings.Repeat("─", innerW))

	var bodyLines []string
	if m.doctorStage != doctorStageResults {
		message := "Checking Omnideck…"
		if m.doctorMessage != "" {
			message = m.doctorMessage
		}
		bodyLines = append(bodyLines, m.doctorSpinner.View()+" "+styles.TNDimText.Render(message))
	} else {
		problems, warnings := 0, 0
		for _, result := range m.doctorResults {
			if result.Status == CheckFail {
				problems++
			} else if result.Status == CheckWarn {
				warnings++
			}
		}
		summary := "Everything required is working"
		summaryStyle := styles.TNGreenTxt
		if problems == 1 {
			summary = "1 problem needs attention"
			summaryStyle = styles.TNRedTxt
		} else if problems > 1 {
			summary = fmt.Sprintf("%d problems need attention", problems)
			summaryStyle = styles.TNRedTxt
		} else if warnings == 1 {
			summary += " · 1 helpful note"
		} else if warnings > 1 {
			summary += fmt.Sprintf(" · %d helpful notes", warnings)
		}
		bodyLines = append(bodyLines, summaryStyle.Render(summary), "")

		for i, result := range m.doctorResults {
			var icon string
			lineStyle := styles.TNDimText
			switch result.Status {
			case CheckPass:
				icon = styles.TNGreenTxt.Render("✓")
				lineStyle = styles.TNTextMid
			case CheckFail:
				icon = styles.TNRedTxt.Render("✗")
				lineStyle = styles.TNRedTxt
			case CheckWarn:
				icon = styles.TNYellowTxt.Render("!")
				lineStyle = styles.TNYellowTxt
			case CheckInfo:
				icon = styles.TNFaintText.Render("·")
			}
			cursor := "  "
			if i == m.doctorFocus {
				cursor = styles.TNBlueTxt.Render("▸ ")
			}
			prefix := cursor + icon + "  "
			continuation := "     "
			for lineIndex, line := range wrapWords(result.Label+" — "+result.Detail, max(1, innerW-lipgloss.Width(prefix)), max(1, innerW-lipgloss.Width(continuation))) {
				linePrefix := continuation
				if lineIndex == 0 {
					linePrefix = prefix
				}
				bodyLines = append(bodyLines, linePrefix+lineStyle.Render(line))
			}
			if result.Hint != "" && (result.Status == CheckFail || result.Status == CheckWarn) {
				for _, line := range wrapWords("What you can do: "+result.Hint, max(1, innerW-5), max(1, innerW-5)) {
					bodyLines = append(bodyLines, "     "+styles.TNFaintText.Render(line))
				}
			}
			if i == m.doctorFocus && result.Action != DoctorActionNone {
				for _, line := range wrapWords("Press Enter to "+strings.ToLower(result.ActionLabel)+".", max(1, innerW-5), max(1, innerW-5)) {
					bodyLines = append(bodyLines, "     "+styles.TNGreenTxt.Render(line))
				}
			}
		}
		if m.doctorMessage != "" {
			bodyLines = append(bodyLines, "", styles.TNRedTxt.Render(m.doctorMessage))
		}
	}

	inner := header + "\n" + sep + "\n" + strings.Join(bodyLines, "\n")
	return m.renderScreen(inner)
}
