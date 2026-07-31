package cmd

import (
	"context"
	"errors"
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

	eng, err := engineFromConfig(cfg.Engine)
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

// updateSteps mirrors tui/maintenance.go's MaintenanceUpdate step sequence
// exactly (pull the possibly-migrated image, then replace the container and
// save its settings as one transaction) so the interactive and
// non-interactive paths can never disagree about what an update does.
// workflow.RecreateAndSave already contains its own rollback for a failed
// replacement or a failed save — neither runUpdatePlain nor runUpdateJSON
// need to reimplement that.
func updateTarget(cfg *config.Config) (current, next config.Config) {
	current = *cfg
	next = current
	next.MigrateImage()
	return current, next
}

// runUpdatePlain performs the same update tui/maintenance.go's interactive
// MaintenanceUpdate flow does, non-interactively, mirroring runSetupPlain's
// step-printing style.
func runUpdatePlain(eng engine.Engine, cfg *config.Config) error {
	current, next := updateTarget(cfg)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"Download the latest Omnideck version", func() error {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(next.Image, msgs)
			close(msgs)
			return err
		}},
		{"Replace the container and save its settings", func() error {
			return workflow.RecreateAndSave(eng, &current, &next, ConfigPath)
		}},
	}

	for _, step := range steps {
		fmt.Printf("  → %s... ", step.label)
		if err := step.fn(); err != nil {
			fmt.Printf("FAILED\n    %v\n", err)
			return err
		}
		fmt.Println("ok")
	}

	fmt.Printf("\n✓  Omnideck is up to date: http://localhost:%s\n", next.WebUIPortOrDefault())
	return nil
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
	current, next := updateTarget(cfg)
	nd := newNDJSONEncoder()

	steps := []struct {
		stage string
		fn    func() error
	}{
		{"pull_image", func() error {
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
			return err
		}},
		{"recreate", func() error {
			return workflow.RecreateAndSave(eng, &current, &next, ConfigPath)
		}},
	}

	for _, step := range steps {
		nd.start(step.stage)
		if err := step.fn(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nd.fail(step.stage, newJSONError(ErrCodeCancelled, err.Error()))
			}
			return nd.fail(step.stage, err)
		}
		nd.done(step.stage)
	}

	nd.complete(gatherStatusPayload(&next, eng))
	return nil
}
