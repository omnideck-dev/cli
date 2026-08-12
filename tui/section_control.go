package tui

import (
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// ControlPlaneSection owns the day-to-day dashboard and every workflow that
// manages an installation that already exists. It is embedded in AppModel so
// the existing screen methods remain compact, while ownership stays explicit.
type ControlPlaneSection struct {
	DashboardScreenState
	LogsScreenState
	SettingsScreenState
	DoctorScreenState

	eng             engine.Engine
	instances       []InstanceState
	inventoryIssues []config.InstanceIssue
	selected        int
	toast           string
	statsGeneration int
	statsPending    int
	statsInFlight   bool

	maintenanceModel MaintenanceModel
	removalModel     RemovalModel
}
