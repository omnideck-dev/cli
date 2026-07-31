package tui

import (
	"errors"
	"testing"
)

func TestFetchStatsRunningSucceeds(t *testing.T) {
	eng := &mockEngine{
		containerStatus: "running",
		statsCPU:        "12.3%", statsCPUPct: 0.123,
		statsRAM: "1.2GiB", statsRAMTotal: "2GiB", statsRAMPct: 0.6,
	}
	msg := fetchStats(eng, "demo", 0).(instanceStatsMsg)

	if msg.statsUnavailable {
		t.Fatal("statsUnavailable should be false on a successful running fetch")
	}
	if !msg.sampleOK {
		t.Fatal("sampleOK should be true on a successful running fetch")
	}
	if msg.cpu != "12.3%" || msg.cpuPct != 0.123 || msg.ram != "1.2GiB" {
		t.Fatalf("unexpected msg: %+v", msg)
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
	msg := fetchStats(eng, "demo", 0).(instanceStatsMsg)

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
	msg := fetchStats(eng, "demo", 0).(instanceStatsMsg)

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
