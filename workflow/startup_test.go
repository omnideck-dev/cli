package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

const migrationFailureLine = `2026-10-31T12:00:01.123456789Z OMNIDECK_STARTUP {"version":1,"timestamp":"2026-10-31T12:00:01Z","phase":"migrations","status":"failed","code":"MIGRATION_FAILED","step":"broker:001_operation_grants"}`

type startupEngine struct {
	inspect engine.InspectData
	lines   []string
}

func (e *startupEngine) ContainerInspect(string) (engine.InspectData, error) {
	return e.inspect, nil
}
func (e *startupEngine) FetchLogs(string, int) ([]string, error) { return e.lines, nil }

func TestStartupFailureRejectsStaleMalformedAndUnsafeEvents(t *testing.T) {
	boot := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		lines   []string
		started time.Time
		want    bool
	}{
		{"current failure", []string{migrationFailureLine}, boot, true},
		{"missing phase", []string{strings.Replace(migrationFailureLine, `"phase":"migrations",`, "", 1)}, boot, false},
		{"unsafe phase", []string{strings.Replace(migrationFailureLine, `"phase":"migrations"`, `"phase":"private / secret"`, 1)}, boot, false},
		{"optional step", []string{strings.Replace(migrationFailureLine, `,"step":"broker:001_operation_grants"`, "", 1)}, boot, true},
		{"other phase completion cannot hide failure", []string{migrationFailureLine, strings.ReplaceAll(strings.Replace(migrationFailureLine, `"failed"`, `"complete"`, 1), `"phase":"migrations"`, `"phase":"server"`)}, boot, true},
		{"previous boot", []string{migrationFailureLine}, boot.Add(time.Hour), false},
		{"unknown boot", []string{migrationFailureLine}, time.Time{}, false},
		{"missing engine timestamp", []string{strings.SplitN(migrationFailureLine, " ", 2)[1]}, boot, false},
		{"old log with new application timestamp", []string{strings.Replace(migrationFailureLine, `"2026-10-31T12:00:01Z"`, `"2026-10-31T14:00:01Z"`, 1)}, boot.Add(time.Hour), false},
		{"completed retry", []string{migrationFailureLine, strings.Replace(migrationFailureLine, `"failed"`, `"complete"`, 1)}, boot, false},
		{"automatic restart cannot hide failure", []string{migrationFailureLine, strings.Replace(migrationFailureLine, `"failed"`, `"starting"`, 1)}, boot, true},
		{"malformed", []string{`OMNIDECK_STARTUP private-secret`}, boot, false},
		{"unsafe step", []string{strings.Replace(migrationFailureLine, "broker:001_operation_grants", "private record / secret", 1)}, boot, false},
		{"unknown contract", []string{strings.Replace(migrationFailureLine, `"version":1`, `"version":2`, 1)}, boot, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := startupFailure(tc.lines, tc.started); (got != nil) != tc.want {
				t.Fatalf("failure = %v", got)
			}
		})
	}
}

func TestWaitReportsMigrationFailureWithoutHTTPOrMutation(t *testing.T) {
	eng := &startupEngine{inspect: engine.InspectData{Status: "exited", StartedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}, lines: []string{migrationFailureLine}}
	err := WaitForApplication(context.Background(), eng, config.DefaultConfig())
	var failure *StartupFailure
	if !errors.As(err, &failure) || failure.Code != "MIGRATION_FAILED" || failure.Step != "broker:001_operation_grants" {
		t.Fatalf("error = %v", err)
	}
}

func TestWaitAcceptsHealthyRetryAndIgnoresFailureFromPreviousBoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.WebUIPort = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	boot := time.Date(2026, 10, 31, 13, 0, 0, 0, time.UTC)
	eng := &startupEngine{inspect: engine.InspectData{Status: "running", StartedAt: boot}, lines: []string{migrationFailureLine}}
	if err := WaitForApplication(context.Background(), eng, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestWaitDoesNotTreatUnhealthyHTTPAsReadyAndHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.WebUIPort = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	eng := &startupEngine{inspect: engine.InspectData{Status: "running", StartedAt: time.Now()}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitForApplication(ctx, eng, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if err := WaitForApplication(ctx, eng, cfg); err == nil {
		t.Fatal("503 was treated as ready")
	}
}

type restartingStartupEngine struct{ polls int }

func (e *restartingStartupEngine) ContainerInspect(string) (engine.InspectData, error) {
	e.polls++
	boot := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if e.polls > 1 {
		boot = boot.Add(time.Minute)
	}
	return engine.InspectData{Status: "running", StartedAt: boot}, nil
}

func (e *restartingStartupEngine) FetchLogs(string, int) ([]string, error) {
	if e.polls == 1 {
		return nil, nil
	}
	return []string{migrationFailureLine, `2026-10-31T12:01:01Z OMNIDECK_STARTUP {"version":1,"timestamp":"2026-10-31T12:01:01Z","phase":"migrations","status":"starting","step":"migration_history"}`}, nil
}

func TestWaitKeepsFailureAcrossAutomaticRestartsBetweenPolls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.WebUIPort = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	eng := &restartingStartupEngine{}
	err := WaitForApplication(ctx, eng, cfg)
	var failure *StartupFailure
	if !errors.As(err, &failure) || failure.Code != "MIGRATION_FAILED" || eng.polls != 2 {
		t.Fatalf("error=%v polls=%d; automatic restart hid the failure", err, eng.polls)
	}
}

func TestWaitSupportsAppWithoutStartupEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.WebUIPort = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	eng := &startupEngine{inspect: engine.InspectData{Status: "running", StartedAt: time.Now()}, lines: []string{"ordinary application log"}}
	if err := WaitForApplication(context.Background(), eng, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestPhaseCompletionDoesNotReplaceHTTPReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.WebUIPort = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	eng := &startupEngine{inspect: engine.InspectData{Status: "running", StartedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}, lines: []string{strings.Replace(migrationFailureLine, `"failed"`, `"complete"`, 1)}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := WaitForApplication(ctx, eng, cfg)
	var failure *StartupFailure
	if !errors.As(err, &failure) || failure.Code != "APP_STARTUP_FAILED" {
		t.Fatalf("phase completion bypassed HTTP: %v", err)
	}
}

func TestOtherStartupPhaseProducesSafeGenericFailure(t *testing.T) {
	line := strings.ReplaceAll(migrationFailureLine, `"phase":"migrations"`, `"phase":"server"`)
	line = strings.ReplaceAll(line, `"code":"MIGRATION_FAILED"`, `"code":"SERVER_FAILED"`)
	line = strings.ReplaceAll(line, `"step":"broker:001_operation_grants"`, `"step":"listen"`)
	failure := startupFailure([]string{line}, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if failure == nil || failure.Code != "APP_STARTUP_FAILED" || failure.Phase != "server" || failure.Step != "listen" {
		t.Fatalf("failure=%v", failure)
	}
	if failure.Detail() != "Failed startup phase: server (step: listen)" {
		t.Fatal(failure.Detail())
	}
}
