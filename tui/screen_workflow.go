package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

// startEmbeddedSetup launches setup as an embedded workflow.
func (m AppModel) startEmbeddedSetup() (AppModel, tea.Cmd) {
	return m.startEmbeddedSetupWithRuntime("")
}

func (m AppModel) startEmbeddedSetupWithRuntime(requestedRuntime string) (AppModel, tea.Cmd) {
	instances := make([]config.InstanceInfo, len(m.instances))
	for i := range m.instances {
		instances[i] = m.instances[i].Info
	}
	cfg := workflow.NewInstanceDefaults(instances)
	mode := SetupAdditionalInstance
	if len(m.instances) == 0 {
		mode = SetupFirstRun
	}
	preferredEngine := requestedRuntime
	if preferredEngine == "" && m.eng != nil {
		preferredEngine = m.eng.Name()
	}
	im := NewSetupModel(SetupRequest{
		Initial:           cfg,
		ExistingInstances: instances,
		Mode:              mode,
		PreferredEngine:   preferredEngine,
		Embedded:          true,
		WindowWidth:       m.width,
		WindowHeight:      m.height,
	})
	m.setupModel = im
	m.router.Push(RouteSetup)
	return m, im.Init()
}

// startEmbeddedUpdate launches Maintenance in update mode.
func (m AppModel) startEmbeddedUpdate() (AppModel, tea.Cmd) {
	return m.startEmbeddedMaintenance(MaintenanceUpdate)
}

func (m AppModel) startEmbeddedRepair() (AppModel, tea.Cmd) {
	return m.startEmbeddedMaintenance(MaintenanceRepair)
}

func (m AppModel) startEmbeddedMaintenance(mode MaintenanceMode) (AppModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil {
		return m, nil
	}
	um := NewMaintenanceModel(MaintenanceRequest{
		Config:     inst.Info.Config,
		ConfigPath: inst.Info.Path,
		Engine:     m.eng,
		Embedded:   true,
		Mode:       mode,
	})
	um.WindowWidth = m.width
	um.WindowHeight = m.height
	m.maintenanceModel = um
	m.router.Push(RouteMaintenance)
	return m, um.Init()
}
