package tui

// InstallationSection owns the guided first-run and runtime-repair journey.
// Its SetupModel deliberately receives plans from engine rather than carrying
// Windows, macOS, or Linux command policy in the TUI.
type InstallationSection struct {
	setupModel SetupModel
}
