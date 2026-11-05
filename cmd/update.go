package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/tui"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var updatePlainFlag bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Omnideck while keeping its saved data",
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updatePlainFlag, "plain", false, "non-interactive update (no TUI) — for scripts and CI/CD")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig

	eng, err := detectReadyEngine()
	if err != nil {
		if jsonFlag {
			return writeJSONError(newJSONError(ErrCodeEngineNotFound, err.Error()))
		}
		return err
	}

	// --json implies the same non-interactive path as --plain — a caller
	// never has to remember to pass both. --plain remains independently
	// useful (plain text, no JSON) and may still be combined with --json.
	if updatePlainFlag || jsonFlag {
		if jsonFlag {
			return runUpdateJSON(eng, cfg)
		}
		return runUpdatePlain(eng, cfg)
	}

	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	instances = withLoadedInstance(instances, cfg, ConfigPath)
	selectedIdx := 0
	for i, inst := range instances {
		if inst.Config != nil && inst.Config.ContainerName == cfg.ContainerName {
			selectedIdx = i
			break
		}
	}

	model := tui.NewAppModelForUpdate(eng, instances, cfg, selectedIdx)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// runUpdatePlain performs the same update tui/maintenance.go's interactive
// MaintenanceUpdate flow does (pull, replace and save as one transaction via
// workflow.RecreateAndSave, then verify the MCP sign-in address),
// non-interactively, mirroring runSetupPlain's step-printing style.
func runUpdatePlain(eng engine.Engine, cfg *config.Config) error {
	current, next := workflow.UpdateTarget(cfg)
	var before workflow.ExternalURLCheck

	// A step's detail replaces the default "ok" so outcomes worth reading
	// (such as what happened to the sign-in address) appear on the step line.
	steps := []struct {
		label string
		fn    func() (string, error)
	}{
		{"Download the latest Omnideck version", func() (string, error) {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(next.Image, msgs)
			close(msgs)
			return "", err
		}},
		{"Replace the container and save its settings", func() (string, error) {
			before = workflow.ExternalURLBeforeReplace(eng, &next)
			return "", workflow.RecreateAndSave(eng, &current, &next, ConfigPath)
		}},
		{"MCP sign-in address", func() (string, error) {
			report, err := workflow.VerifyExternalURL(eng, &next, before)
			return report.Summary(), err
		}},
	}

	for _, step := range steps {
		fmt.Printf("  → %s... ", step.label)
		detail, err := step.fn()
		if err != nil {
			fmt.Printf("FAILED\n    %v\n", err)
			return err
		}
		if detail == "" {
			detail = "ok"
		}
		fmt.Println(detail)
	}

	fmt.Printf("\n✓  Omnideck is up to date: %s\n", next.BrowserURL())
	return nil
}

// externalURLStageResult is the "result" of the external_url stage's done
// event: what the update did to the container's OMNIDECK_EXTERNAL_URL.
type externalURLStageResult struct {
	State    string `json:"state"`
	Expected string `json:"expected"`
	Previous string `json:"previous,omitempty"`
}

// runUpdateJSON performs the same update as runUpdatePlain but emits NDJSON
// progress instead of printing text, using the same {"stage","state"}
// convention as add/remove.
//
// Unlike add, a cancelled update doesn't need bespoke cleanup:
// workflow.RecreateAndSave/Recreate already roll back a failed replacement
// or a failed save to the previous, already-saved container — the same
// path a logical failure already takes. See JSON_MODE_SPEC.md's
// "Cancellation and teardown".
func runUpdateJSON(eng engine.Engine, cfg *config.Config) error {
	current, next := workflow.UpdateTarget(cfg)
	nd := newNDJSONEncoder()
	var before workflow.ExternalURLCheck

	steps := []struct {
		stage string
		fn    func() (any, error)
	}{
		{"pull_image", func() (any, error) {
			msgs := make(chan string, 32)
			forwarded := make(chan struct{})
			go func() {
				defer close(forwarded)
				for line := range msgs {
					nd.progress("pull_image", line)
				}
			}()
			err := eng.PullImage(next.Image, msgs)
			close(msgs)
			<-forwarded
			return nil, err
		}},
		{"recreate", func() (any, error) {
			before = workflow.ExternalURLBeforeReplace(eng, &next)
			return nil, workflow.RecreateAndSave(eng, &current, &next, ConfigPath)
		}},
		{"external_url", func() (any, error) {
			report, err := workflow.VerifyExternalURL(eng, &next, before)
			if err != nil {
				return nil, err
			}
			return externalURLStageResult{
				State:    string(report.Change),
				Expected: report.Expected,
				Previous: report.Previous,
			}, nil
		}},
	}

	for _, step := range steps {
		if nd.broken() {
			return errAborted
		}
		nd.start(step.stage)
		result, err := step.fn()
		if err != nil {
			return nd.fail(step.stage, environmentStageJSONError(step.stage, err))
		}
		nd.doneWithResult(step.stage, result)
	}

	if nd.broken() {
		return errAborted
	}
	nd.complete(gatherStatusPayload(loadedInstanceName(), &next, eng))
	if nd.broken() {
		return errAborted
	}
	return nil
}
