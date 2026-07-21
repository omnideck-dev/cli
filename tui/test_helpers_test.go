package tui

import (
	"io"

	"github.com/omnideck-dev/cli/engine"
)

// mockEngine is the shared container-engine stub for TUI workflow tests.
type mockEngine struct {
	name            string
	containerExists bool
	containerNames  map[string]bool
	containerStatus string
	startErr        error
	stopErr         error
	removeErr       error
	runErr          error
	runErrors       []error
	fetchLines      []string
	fetchErr        error
	volumes         map[string]bool
	lastRunOptions  engine.RunOptions
}

func (m *mockEngine) Name() string {
	if m.name != "" {
		return m.name
	}
	return "docker"
}
func (m *mockEngine) IsAvailable() bool                     { return true }
func (m *mockEngine) HasPermission() bool                   { return true }
func (m *mockEngine) Version() string                       { return "1.0" }
func (m *mockEngine) ImageDigest(string) string             { return "" }
func (m *mockEngine) PullImage(string, chan<- string) error { return nil }
func (m *mockEngine) RunContainer(opts engine.RunOptions) error {
	m.lastRunOptions = opts
	if len(m.runErrors) > 0 {
		err := m.runErrors[0]
		m.runErrors = m.runErrors[1:]
		if err != nil {
			return err
		}
		m.containerExists = true
		m.containerStatus = "running"
		return nil
	}
	if m.runErr == nil {
		m.containerExists = true
		m.containerStatus = "running"
	}
	return m.runErr
}
func (m *mockEngine) RemoveContainer(string) error {
	if m.removeErr == nil {
		m.containerExists = false
	}
	return m.removeErr
}
func (m *mockEngine) TailLogs(string, bool, int) error { return nil }
func (m *mockEngine) ContainerExists(name string) (bool, error) {
	if m.containerNames != nil {
		return m.containerNames[name], nil
	}
	return m.containerExists, nil
}
func (m *mockEngine) CreateVolume(string) error { return nil }
func (m *mockEngine) VolumeExists(name string) (bool, error) {
	return m.volumes[name], nil
}
func (m *mockEngine) RemoveVolume(string) error            { return nil }
func (m *mockEngine) ExportVolume(string, io.Writer) error { return nil }
func (m *mockEngine) StartContainer(string) error {
	if m.startErr == nil {
		m.containerStatus = "running"
	}
	return m.startErr
}
func (m *mockEngine) StopContainer(string) error {
	if m.stopErr == nil {
		m.containerStatus = "exited"
	}
	return m.stopErr
}
func (m *mockEngine) ContainerStatus(string) (string, error) {
	return m.containerStatus, nil
}
func (m *mockEngine) ContainerStats(string) (string, float64, string, string, float64, error) {
	return "", 0, "", "", 0, nil
}
func (m *mockEngine) FetchLogs(string, int) ([]string, error) {
	return m.fetchLines, m.fetchErr
}
func (m *mockEngine) ContainerInspect(string) (engine.InspectData, error) {
	return engine.InspectData{}, nil
}
