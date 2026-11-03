package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// StartupEngine can observe startup without changing the container or its data.
type StartupEngine interface {
	ContainerInspect(string) (engine.InspectData, error)
	FetchLogs(string, int) ([]string, error)
}

// StartupFailure is a safe, structured error; raw container logs are never copied
// into it. Phase and step are code-owned identifiers, not saved records or paths.
type StartupFailure struct {
	Code  string
	Phase string
	Step  string
}

func (e *StartupFailure) Error() string {
	if e.Code == "MIGRATION_FAILED" {
		return "omnideck couldn’t start. Your saved settings couldn’t be upgraded. Startup stopped before agents or integrations were started."
	}
	return "omnideck didn’t become ready. Check the container logs for details."
}

func (e *StartupFailure) Detail() string {
	if e.Phase != "" {
		detail := "Failed startup phase: " + e.Phase
		if e.Step != "" {
			detail += " (step: " + e.Step + ")"
		}
		return detail
	}
	return "Application startup did not complete."
}

var startupIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,100}$`)

// startupFailure reads migration events since this readiness check began.
// Older app images have no events; their readiness is still checked over HTTP.
func startupFailure(lines []string, startedAt time.Time) *StartupFailure {
	if startedAt.IsZero() {
		return nil
	}
	failures := make(map[string]*StartupFailure)
	for _, line := range lines {
		// Engine.FetchLogs uses podman logs --timestamps. Use its timestamp to
		// bind the record to the current boot, not application-controlled data.
		stamp, message, ok := strings.Cut(line, " ")
		loggedAt, err := time.Parse(time.RFC3339Nano, stamp)
		if !ok || err != nil || loggedAt.Before(startedAt) || !strings.HasPrefix(message, "OMNIDECK_STARTUP ") {
			continue
		}
		var event struct {
			Version   int       `json:"version"`
			Timestamp time.Time `json:"timestamp"`
			Status    string    `json:"status"`
			Phase     string    `json:"phase"`
			Code      string    `json:"code"`
			Step      string    `json:"step"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(message, "OMNIDECK_STARTUP ")), &event) != nil || event.Version != 1 || event.Timestamp.IsZero() || !startupIdentifier.MatchString(event.Phase) || (event.Step != "" && !startupIdentifier.MatchString(event.Step)) {
			continue
		}
		switch event.Status {
		case "complete":
			delete(failures, event.Phase)
		case "failed":
			code := "APP_STARTUP_FAILED"
			if event.Phase == "migrations" && event.Code == "MIGRATION_FAILED" {
				code = event.Code
			}
			failures[event.Phase] = &StartupFailure{Code: code, Phase: event.Phase, Step: event.Step}
		}
	}
	// Return a deterministic failure if more than one phase reported an error.
	phases := make([]string, 0, len(failures))
	for phase := range failures {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	if len(phases) > 0 {
		return failures[phases[0]]
	}
	return nil
}

// WaitForApplication observes the initial boot and subsequent automatic restarts.
// A failure never removes the container, resets data, or retries an operation.
func WaitForApplication(ctx context.Context, eng StartupEngine, cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	var attemptStart time.Time
	for {
		if ctx.Err() != nil {
			if ctx.Err() == context.Canceled {
				return ctx.Err()
			}
			return &StartupFailure{Code: "APP_STARTUP_FAILED"}
		}
		inspect, err := eng.ContainerInspect(cfg.ContainerName)
		if err != nil {
			return fmt.Errorf("checking application startup: %w", err)
		}
		if attemptStart.IsZero() {
			attemptStart = inspect.StartedAt
		}
		lines, err := eng.FetchLogs(cfg.ContainerName, 200)
		if err != nil {
			return fmt.Errorf("reading startup diagnostics: %w", err)
		}
		if failure := startupFailure(lines, attemptStart); failure != nil {
			// Podman's restart=always may begin another boot before the next
			// poll. It must not erase a failure observed during this check.
			// A new user retry starts a new check/cutoff; complete clears failure.
			return failure
		}
		if inspect.Status == "running" {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+cfg.WebUIPortOrDefault()+"/", nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err == nil {
				response.Body.Close()
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					return nil
				}
			}
		} else if inspect.Status == "exited" || inspect.Status == "dead" {
			return &StartupFailure{Code: "APP_STARTUP_FAILED"}
		}
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return ctx.Err()
			}
			return &StartupFailure{Code: "APP_STARTUP_FAILED"}
		case <-time.After(500 * time.Millisecond):
		}
	}
}
