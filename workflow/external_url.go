package workflow

import (
	"fmt"
	"strings"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// ExternalURLEnvVar is the container variable the app uses to build OAuth
// callbacks for MCP and Slack integrations. The app never infers it from
// request headers, so a container without it cannot offer those integrations.
const ExternalURLEnvVar = "OMNIDECK_EXTERNAL_URL"

// ContainerEnvEngine can read a container's configured environment.
type ContainerEnvEngine interface {
	ContainerEnv(name string) ([]string, error)
}

// ExternalURLState classifies a container's OMNIDECK_EXTERNAL_URL.
type ExternalURLState string

const (
	ExternalURLMissing  ExternalURLState = "missing"
	ExternalURLMismatch ExternalURLState = "mismatch"
	ExternalURLOK       ExternalURLState = "ok"
	// ExternalURLUnknown means the container could not be inspected.
	ExternalURLUnknown ExternalURLState = "unknown"
)

// ExternalURLCheck is the result of comparing a container's sign-in address
// with the instance's browser address.
type ExternalURLCheck struct {
	State    ExternalURLState
	Actual   string // empty when missing
	Expected string // cfg.BrowserURL()
}

// CheckExternalURL inspects the container named by cfg and classifies its
// OMNIDECK_EXTERNAL_URL. An empty value counts as missing; a single trailing
// slash and surrounding whitespace are ignored.
func CheckExternalURL(eng ContainerEnvEngine, cfg *config.Config) (ExternalURLCheck, error) {
	check := ExternalURLCheck{State: ExternalURLMissing, Expected: cfg.BrowserURL()}
	env, err := eng.ContainerEnv(cfg.ContainerName)
	if err != nil {
		return check, fmt.Errorf("reading the environment of container %q: %w", cfg.ContainerName, err)
	}
	actual, _ := engine.EnvValue(env, ExternalURLEnvVar)
	check.Actual = actual
	switch normalized := normalizeExternalURL(actual); {
	case normalized == "":
		check.State = ExternalURLMissing
	case normalized == normalizeExternalURL(check.Expected):
		check.State = ExternalURLOK
	default:
		check.State = ExternalURLMismatch
	}
	return check, nil
}

func normalizeExternalURL(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), "/")
}

// ExternalURLBeforeReplace records a container's sign-in address before an
// update replaces it. An inspection failure (including a missing container)
// is reported as unknown rather than stopping the update: the replacement
// writes the correct value anyway.
func ExternalURLBeforeReplace(eng ContainerEnvEngine, cfg *config.Config) ExternalURLCheck {
	check, err := CheckExternalURL(eng, cfg)
	if err != nil {
		return ExternalURLCheck{State: ExternalURLUnknown, Expected: cfg.BrowserURL()}
	}
	return check
}

// ExternalURLChange describes what an update did to the sign-in address.
type ExternalURLChange string

const (
	ExternalURLSet       ExternalURLChange = "set"
	ExternalURLCorrected ExternalURLChange = "corrected"
	ExternalURLConfirmed ExternalURLChange = "confirmed"
	// ExternalURLVerified means the new container is correct but the old
	// one could not be read, so whether anything changed is unknown.
	ExternalURLVerified ExternalURLChange = "verified"
)

// ExternalURLReport is the verified outcome of an update for the sign-in address.
type ExternalURLReport struct {
	Change   ExternalURLChange
	Previous string // the replaced value, set only when Change is corrected
	Expected string
}

// Summary is the plain-language outcome shown after an update.
func (r ExternalURLReport) Summary() string {
	switch r.Change {
	case ExternalURLSet:
		return "set to " + r.Expected
	case ExternalURLCorrected:
		return "corrected " + r.Previous + " → " + r.Expected
	case ExternalURLVerified:
		return "verified " + r.Expected + " (the previous value could not be read)"
	default:
		return "confirmed " + r.Expected
	}
}

// VerifyExternalURL confirms the replacement container has the instance's
// sign-in address and reports how it changed relative to before. A wrong
// value after Omnideck configured the container is a real defect, so it is
// returned as ErrExternalURL rather than tolerated.
func VerifyExternalURL(eng ContainerEnvEngine, cfg *config.Config, before ExternalURLCheck) (ExternalURLReport, error) {
	after, err := CheckExternalURL(eng, cfg)
	if err != nil {
		return ExternalURLReport{}, classifyError(ErrExternalURL, fmt.Errorf(
			"the update was applied and its settings saved, but Omnideck could not confirm its MCP sign-in address: %w. Run `omnideck doctor` to check it", err))
	}
	if after.State != ExternalURLOK {
		found := "has no MCP sign-in address"
		if after.Actual != "" {
			found = "uses the MCP sign-in address " + after.Actual
		}
		return ExternalURLReport{}, classifyError(ErrExternalURL, fmt.Errorf(
			"the update was applied and its settings saved, but the new container %s instead of %s, so MCP and Slack integrations will be unavailable. Run the update again; if this keeps happening, report it with the output of `omnideck doctor`",
			found, after.Expected,
		))
	}
	report := ExternalURLReport{Expected: after.Expected}
	switch before.State {
	case ExternalURLOK:
		report.Change = ExternalURLConfirmed
	case ExternalURLMismatch:
		report.Change = ExternalURLCorrected
		report.Previous = before.Actual
	case ExternalURLMissing:
		report.Change = ExternalURLSet
	default:
		report.Change = ExternalURLVerified
	}
	return report, nil
}

// UpdateTarget returns the saved configuration and the configuration an
// update applies: the possibly-migrated image and the current container
// wiring, so a later `environment ensure` sees the container as current.
func UpdateTarget(cfg *config.Config) (current, next config.Config) {
	current = *cfg
	next = current
	next.MigrateImage()
	next.LayoutVersion = config.CurrentContainerLayout
	return current, next
}
