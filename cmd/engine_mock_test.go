package cmd

import (
	"io"

	"github.com/omnideck-dev/cli/engine"
)

// mockEngine is a configurable engine.Engine stub shared by cmd package
// --json tests. Fields are keyed by container name where a test needs to
// distinguish between instances (e.g. list --json); otherwise a single
// scalar default is used.
type mockEngine struct {
	name string

	containerStatus    map[string]string
	containerStatusErr map[string]error
	containerExists    map[string]bool
	containerExistsErr error

	volumes           map[string]bool
	volumeExistsErr   error
	createVolumeErr   error
	removeVolumeErr   error
	exportVolumeErr   error
	removeVolumeCalls []string

	stats    map[string]mockStats
	statsErr error

	inspect    map[string]engine.InspectData
	inspectErr error

	fetchLines []string
	fetchErr   error
	tailErr    error
	tailLines  []string

	pullErr              error
	pullMsgs             []string
	runErr               error
	runCalls             []engine.RunOptions
	stopErr              error
	startErr             error
	removeErr            error
	removeContainerCalls int
	ollamaErr            error
	versionStr           string
	imageDigest          string
}

type mockStats struct {
	cpu, ram, ramTotal string
	cpuPct, ramPct     float64
}

func (m *mockEngine) Name() string {
	if m.name != "" {
		return m.name
	}
	return "podman"
}

func (m *mockEngine) IsAvailable() bool         { return true }
func (m *mockEngine) HasPermission() bool       { return true }
func (m *mockEngine) Version() string           { return m.versionStr }
func (m *mockEngine) ImageDigest(string) string { return m.imageDigest }

func (m *mockEngine) ContainerExists(name string) (bool, error) {
	if m.containerExistsErr != nil {
		return false, m.containerExistsErr
	}
	return m.containerExists[name], nil
}

func (m *mockEngine) CreateVolume(string) error { return m.createVolumeErr }

func (m *mockEngine) VolumeExists(name string) (bool, error) {
	if m.volumeExistsErr != nil {
		return false, m.volumeExistsErr
	}
	return m.volumes[name], nil
}

func (m *mockEngine) RemoveVolume(name string) error {
	m.removeVolumeCalls = append(m.removeVolumeCalls, name)
	return m.removeVolumeErr
}
func (m *mockEngine) ExportVolume(string, io.Writer) error { return m.exportVolumeErr }

func (m *mockEngine) PullImage(_ string, msgs chan<- string) error {
	if msgs != nil {
		for _, line := range m.pullMsgs {
			msgs <- line
		}
	}
	return m.pullErr
}

func (m *mockEngine) RunContainer(opts engine.RunOptions) error {
	m.runCalls = append(m.runCalls, opts)
	return m.runErr
}

func (m *mockEngine) CheckOllamaConnection(string) error { return m.ollamaErr }

func (m *mockEngine) StopContainer(string) error { return m.stopErr }

func (m *mockEngine) StartContainer(string) error { return m.startErr }

func (m *mockEngine) RemoveContainer(string) error {
	m.removeContainerCalls++
	return m.removeErr
}

func (m *mockEngine) ContainerStatus(name string) (string, error) {
	if err := m.containerStatusErr[name]; err != nil {
		return "", err
	}
	return m.containerStatus[name], nil
}

func (m *mockEngine) TailLogs(name string, follow bool, tail int, stdout, _ io.Writer) error {
	if m.tailErr != nil {
		return m.tailErr
	}
	for _, line := range m.tailLines {
		if _, err := stdout.Write([]byte(line + "\n")); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockEngine) ContainerStats(name string) (string, float64, string, string, float64, error) {
	if m.statsErr != nil {
		return "", 0, "", "", 0, m.statsErr
	}
	s := m.stats[name]
	return s.cpu, s.cpuPct, s.ram, s.ramTotal, s.ramPct, nil
}

func (m *mockEngine) FetchLogs(string, int) ([]string, error) {
	return m.fetchLines, m.fetchErr
}

func (m *mockEngine) ContainerInspect(name string) (engine.InspectData, error) {
	if m.inspectErr != nil {
		return engine.InspectData{}, m.inspectErr
	}
	return m.inspect[name], nil
}
