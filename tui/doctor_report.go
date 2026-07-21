package tui

import (
	"fmt"

	"github.com/omnideck-dev/cli/styles"
	"github.com/omnideck-dev/cli/workflow"
)

type CheckStatus = workflow.CheckStatus
type CheckAction = workflow.CheckAction
type CheckResult = workflow.CheckResult

const (
	CheckPass = workflow.CheckPass
	CheckFail = workflow.CheckFail
	CheckWarn = workflow.CheckWarn
	CheckInfo = workflow.CheckInfo

	DoctorActionNone           = workflow.DoctorActionNone
	DoctorActionRuntimeSetup   = workflow.DoctorActionRuntimeSetup
	DoctorActionStartInstance  = workflow.DoctorActionStartInstance
	DoctorActionSetupInstance  = workflow.DoctorActionSetupInstance
	DoctorActionRepairInstance = workflow.DoctorActionRepairInstance
)

// RenderDoctorReport renders a static report for `omnideck doctor`.
func RenderDoctorReport(results []CheckResult) (string, bool) {
	allPass := true
	problems := 0
	out := "\n" + styles.Title.Render("  Omnideck Doctor") + "\n"
	out += "  " + styles.Dim.Render("────────────────────────────────────────") + "\n\n"

	for _, result := range results {
		var icon, labelStyle, detailStyle string
		switch result.Status {
		case CheckPass:
			icon = styles.CheckMark
			labelStyle = styles.Success.Render(result.Label)
			detailStyle = styles.Dim.Render(result.Detail)
		case CheckFail:
			icon = styles.CrossMark
			labelStyle = styles.Error.Render(result.Label)
			detailStyle = styles.Error.Render(result.Detail)
			allPass = false
			problems++
		case CheckWarn:
			icon = styles.Warning.Render("!")
			labelStyle = styles.Warning.Render(result.Label)
			detailStyle = styles.Warning.Render(result.Detail)
		case CheckInfo:
			icon = styles.Dim.Render("·")
			labelStyle = styles.Dim.Render(result.Label)
			detailStyle = styles.Dim.Render(result.Detail)
		}

		out += fmt.Sprintf("  %s  %-26s %s\n", icon, labelStyle, detailStyle)
		if result.Hint != "" && result.Status != CheckPass {
			out += fmt.Sprintf("       %s\n", styles.Dim.Render("→ "+result.Hint))
		}
		if command := doctorActionCommand(result); command != "" {
			out += fmt.Sprintf("       %s\n", styles.Dim.Render("Next: "+command))
		}
	}

	out += "\n"
	if problems == 0 {
		out += "  " + styles.Success.Render("Everything required for Omnideck is working.") + "\n"
	} else if problems == 1 {
		out += "  " + styles.Error.Render("Doctor found 1 problem that needs attention.") + "\n"
	} else {
		out += "  " + styles.Error.Render(fmt.Sprintf("Doctor found %d problems that need attention.", problems)) + "\n"
	}
	return out, allPass
}

func doctorActionCommand(result CheckResult) string {
	switch result.Action {
	case DoctorActionRuntimeSetup:
		return "run `omnideck` to open guided container setup"
	case DoctorActionStartInstance:
		return "run `omnideck start --name " + result.ActionValue + "`"
	case DoctorActionSetupInstance:
		return "run `omnideck setup`"
	case DoctorActionRepairInstance:
		return "run `omnideck` and choose Doctor, then Repair this installation"
	default:
		return ""
	}
}
