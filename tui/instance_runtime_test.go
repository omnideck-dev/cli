package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

func TestDashboardPollIntervalIsThreeSeconds(t *testing.T) {
	if dashboardPollInterval != 3*time.Second {
		t.Fatalf("dashboard poll interval = %s, want 3s", dashboardPollInterval)
	}
}

func TestFetchStatsRunningSucceeds(t *testing.T) {
	eng := &mockEngine{
		containerStatus: "running",
		statsCPU:        "12.3%", statsCPUPct: 0.123,
		statsRAM: "1.2GiB", statsRAMTotal: "2GiB", statsRAMPct: 0.6,
	}
	msg := fetchStats(eng, "demo", 0, 0).(instanceStatsMsg)

	if msg.statsUnavailable {
		t.Fatal("statsUnavailable should be false on a successful running fetch")
	}
	if !msg.sampleOK {
		t.Fatal("sampleOK should be true on a successful running fetch")
	}
	if eng.inspectCalls != 1 || eng.statusCalls != 0 {
		t.Fatalf("poll used inspect=%d status=%d calls, want one consolidated inspect", eng.inspectCalls, eng.statusCalls)
	}
	if msg.cpu != "12.3%" || msg.cpuPct != 0.123 || msg.ram != "1.2GiB" {
		t.Fatalf("unexpected msg: %+v", msg)
	}
}

func TestFetchStatsUsesInspectMetadata(t *testing.T) {
	started := time.Now().Add(-2 * time.Hour)
	created := started.Add(-time.Hour)
	eng := &mockEngine{
		inspectData: engine.InspectData{
			Status:       "running",
			StartedAt:    started,
			CreatedAt:    created,
			RestartCount: 2,
			HealthStatus: "healthy",
		},
	}
	msg := fetchStats(eng, "demo", 0, 0).(instanceStatsMsg)

	if msg.status != "running" || msg.health != "healthy" || msg.restarts != "2" || msg.created != created.Format("2006-01-02") {
		t.Fatalf("inspect metadata was not applied: %+v", msg)
	}
	if msg.uptime == "" {
		t.Fatal("running inspect metadata should produce uptime")
	}
}

// TestFetchStatsPausedStillCallsEngineStats is the regression guard for a
// paused instance silently losing its stats display: a paused container is
// frozen, not exited, and Docker/Podman still return real, non-error stats
// for it — unlike the "running" narrowing this must not treat paused the
// same as stopped.
func TestFetchStatsPausedStillCallsEngineStats(t *testing.T) {
	eng := &mockEngine{
		containerStatus: "paused",
		statsCPU:        "0.00%", statsCPUPct: 0,
		statsRAM: "1.2GiB", statsRAMTotal: "2GiB", statsRAMPct: 0.6,
	}
	msg := fetchStats(eng, "demo", 0, 0).(instanceStatsMsg)

	if eng.statsCalls != 1 {
		t.Fatalf("ContainerStats was called %d times for a paused container, want 1", eng.statsCalls)
	}
	if msg.statsUnavailable {
		t.Fatal("statsUnavailable should be false on a successful paused fetch")
	}
	if !msg.sampleOK {
		t.Fatal("sampleOK should be true on a successful paused fetch")
	}
	if msg.ram != "1.2GiB" {
		t.Fatalf("paused instance should still show its frozen ram, got %q", msg.ram)
	}
}

// TestFetchStatsStoppedNeverCallsEngineStats is the regression guard for the
// misleading "0.00% / 0B" a stopped container used to show: Docker/Podman
// report zeros without erroring for a stopped container, so the fix is to
// never call ContainerStats at all once the container isn't running.
func TestFetchStatsStoppedNeverCallsEngineStats(t *testing.T) {
	eng := &mockEngine{
		containerStatus: "exited",
		statsCPU:        "0.00%", statsRAM: "0B", // what the engine would return if asked
	}
	msg := fetchStats(eng, "demo", 0, 0).(instanceStatsMsg)

	if eng.statsCalls != 0 {
		t.Fatalf("ContainerStats was called %d times for a stopped container, want 0", eng.statsCalls)
	}
	if msg.cpu != "" || msg.ram != "" {
		t.Fatalf("stopped instance should have blank cpu/ram (renders as dash), got cpu=%q ram=%q", msg.cpu, msg.ram)
	}
	if msg.statsUnavailable {
		t.Fatal("a stopped instance is not the same as stats being unavailable")
	}
	if msg.sampleOK {
		t.Fatal("a stopped instance must not produce a history sample")
	}
}

// TestFetchStatsRunningStatsErrorSetsUnavailable is the regression guard for
// the reported bug: a running instance whose engine stats call fails (e.g. a
// stale container network namespace) must be distinguishable from a normal
// "not polled yet" blank state, not silently show dashes with no signal.
func TestFetchStatsRunningStatsErrorSetsUnavailable(t *testing.T) {
	eng := &mockEngine{
		containerStatus: "running",
		statsErr:        errors.New(`unknown FS magic on "/run/user/1000/netns/...": 1021994`),
	}
	msg := fetchStats(eng, "demo", 0, 0).(instanceStatsMsg)

	if !msg.statsUnavailable {
		t.Fatal("statsUnavailable should be true when a running container's stats call fails")
	}
	if msg.sampleOK {
		t.Fatal("a failed stats call must not produce a history sample")
	}
	if msg.cpu != "" || msg.ram != "" {
		t.Fatalf("cpu/ram should stay blank on a failed fetch, got cpu=%q ram=%q", msg.cpu, msg.ram)
	}
}

func TestApplyInstanceStatsOnlyPushesHistoryOnSampleOK(t *testing.T) {
	inst := &InstanceState{}

	applyInstanceStats(inst, instanceStatsMsg{status: "running", cpuPct: 0.5, ramPct: 0.4, sampleOK: true})
	if len(inst.CPUHistory) != 1 || len(inst.RAMHistory) != 1 {
		t.Fatalf("expected one history sample after sampleOK, got cpu=%v ram=%v", inst.CPUHistory, inst.RAMHistory)
	}

	applyInstanceStats(inst, instanceStatsMsg{status: "exited"})
	if len(inst.CPUHistory) != 1 || len(inst.RAMHistory) != 1 {
		t.Fatalf("a stopped poll must not add a history sample, got cpu=%v ram=%v", inst.CPUHistory, inst.RAMHistory)
	}

	applyInstanceStats(inst, instanceStatsMsg{status: "running", statsUnavailable: true})
	if len(inst.CPUHistory) != 1 || len(inst.RAMHistory) != 1 {
		t.Fatalf("an unavailable-stats poll must not add a history sample, got cpu=%v ram=%v", inst.CPUHistory, inst.RAMHistory)
	}
	if !inst.StatsUnavailable {
		t.Fatal("StatsUnavailable should be set on the instance after an unavailable-stats poll")
	}

	applyInstanceStats(inst, instanceStatsMsg{status: "running", cpuPct: 0.1, ramPct: 0.2, sampleOK: true})
	if inst.StatsUnavailable {
		t.Fatal("a subsequent successful poll should clear StatsUnavailable")
	}
	if len(inst.CPUHistory) != 2 {
		t.Fatalf("expected 2 history samples after recovering, got %v", inst.CPUHistory)
	}
}

func TestApplyInstanceStatsPreservesLastKnownGoodFieldsOnUnavailable(t *testing.T) {
	inst := &InstanceState{Uptime: "3h", Restarts: "1", Created: "2026-07-01", Health: "healthy"}
	applyInstanceStats(inst, instanceStatsMsg{status: "running", statsUnavailable: true})

	if inst.Uptime != "3h" || inst.Restarts != "1" || inst.Created != "2026-07-01" || inst.Health != "healthy" {
		t.Fatalf("non-stats fields should be left alone when a poll only reports empty values, got %+v", inst)
	}
	if !inst.StatsUnavailable {
		t.Fatal("StatsUnavailable should be true")
	}
}

func TestStatsResultsApplyByStableInstanceIdentity(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "one", Config: &config.Config{ContainerName: "one"}},
		{Name: "two", Config: &config.Config{ContainerName: "two"}},
	}
	m := NewAppModel(&mockEngine{}, instances)
	m.statsGeneration = 4
	m.statsPending = 1
	m.statsInFlight = true

	updated, _ := m.Update(instanceStatsMsg{id: "two", idx: 0, generation: 4, status: "running", cpu: "10%", sampleOK: true})
	m = updated.(AppModel)
	if m.instances[0].CPU != "" || m.instances[1].CPU != "10%" {
		t.Fatalf("stats applied by stale index: %#v", m.instances)
	}
	if m.statsInFlight || m.statsPending != 0 {
		t.Fatalf("poll bookkeeping pending=%d inFlight=%t", m.statsPending, m.statsInFlight)
	}
}

func TestStatsResultsFromOldGenerationAreIgnored(t *testing.T) {
	m := NewAppModel(&mockEngine{}, []config.InstanceInfo{{Name: "one", Config: &config.Config{ContainerName: "one"}}})
	m.statsGeneration = 3
	updated, _ := m.Update(instanceStatsMsg{id: "one", generation: 2, status: "running", cpu: "99%", sampleOK: true})
	m = updated.(AppModel)
	if m.instances[0].CPU != "" {
		t.Fatalf("stale stats were applied: %#v", m.instances[0])
	}
}
