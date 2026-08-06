// Package tui implements Omnideck's routed terminal user interface.
//
// It has two deliberately separate sections:
//
//   - InstallationSection owns first-run setup and runtime repair. The desktop
//     is the canonical walkthrough; this section adapts the same phases and
//     copy to terminal constraints.
//   - ControlPlaneSection owns the dashboard, instance lifecycle, logs,
//     settings, doctor, updates, repairs, and removal after setup.
//
// AppModel is only the shell and route boundary between those sections. Host
// command differences stay in engine setup/platform policy rather than views.
package tui
