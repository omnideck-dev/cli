package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
	"github.com/spf13/cobra"
)

var (
	environmentImageFlag       string
	environmentPortFlag        string
	environmentMemoryFlag      string
	environmentShmFlag         string
	environmentHomeVolumeFlag  string
	environmentStateVolumeFlag string
)

var environmentCmd = &cobra.Command{
	Use:   "environment",
	Short: "Reconcile an Omnideck application environment",
}

var environmentEnsureCmd = &cobra.Command{
	Use:          "ensure",
	Short:        "Create, repair, start, or update an application environment",
	SilenceUsage: true,
	RunE:         runEnvironmentEnsure,
}

func init() {
	rootCmd.AddCommand(environmentCmd)
	environmentCmd.AddCommand(environmentEnsureCmd)
	environmentEnsureCmd.Flags().StringVar(&environmentImageFlag, "image", "", "desired application image")
	environmentEnsureCmd.Flags().StringVar(&environmentPortFlag, "port", "", "desired local web UI port")
	environmentEnsureCmd.Flags().StringVar(&environmentMemoryFlag, "memory", "", "desired container memory limit")
	environmentEnsureCmd.Flags().StringVar(&environmentShmFlag, "shm-size", "", "desired shared memory size")
	environmentEnsureCmd.Flags().StringVar(&environmentHomeVolumeFlag, "home-volume", "", "persistent home volume name")
	environmentEnsureCmd.Flags().StringVar(&environmentStateVolumeFlag, "state-volume", "", "persistent state volume name")
}

type environmentEnsurePayload struct {
	Changed bool          `json:"changed"`
	Action  string        `json:"action"`
	Reason  string        `json:"reason,omitempty"`
	Status  statusPayload `json:"status"`
}

func desiredEnvironmentConfig(current *config.Config) (*config.Config, error) {
	desired := config.DefaultConfig()
	if current != nil {
		copy := *current
		desired = &copy
	}
	// --name names the instance. A new instance's container takes the same
	// name; an existing one keeps its saved container, which may differ (for
	// example instance "desktop" with container "omnideck-desktop").
	if current == nil && nameFlag != "" {
		desired.ContainerName = nameFlag
	}
	if environmentImageFlag != "" {
		desired.Image = environmentImageFlag
	}
	if environmentPortFlag != "" {
		desired.WebUIPort = environmentPortFlag
	}
	if environmentMemoryFlag != "" {
		desired.Memory = environmentMemoryFlag
	}
	if environmentShmFlag != "" {
		desired.ShmSize = environmentShmFlag
	}
	if environmentHomeVolumeFlag != "" {
		desired.HomeVolume = environmentHomeVolumeFlag
	}
	if environmentStateVolumeFlag != "" {
		desired.StateVolume = environmentStateVolumeFlag
	}
	desired.LayoutVersion = config.CurrentContainerLayout
	if err := workflow.ValidateInstanceChange(current, desired); err != nil {
		return nil, setupValidationError(err)
	}
	if desired.InstalledAt.IsZero() {
		desired.InstalledAt = time.Now()
	}
	desired.Engine = ""
	return desired, nil
}

func loadEnvironmentConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	return nil, fmt.Errorf("reading the saved environment: %w", err)
}

// validateEnvironmentOwnership refuses a container, data volume, or port that
// belongs to another saved installation. The instance being ensured is
// skipped by its instance name: skipping by container name would let a new
// instance adopt another installation's container and data.
func validateEnvironmentOwnership(desired *config.Config) error {
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("checking existing installations: %w", err)
	}
	if err := workflow.CheckInstanceOwnership(instances, nameFlag, desired); err != nil {
		return err
	}
	for _, instance := range instances {
		if instance.Config == nil || instance.Name == nameFlag {
			continue
		}
		if instance.Config.WebUIPortOrDefault() == desired.WebUIPortOrDefault() {
			return fmt.Errorf("another Omnideck installation already uses port %s", desired.WebUIPortOrDefault())
		}
	}
	return nil
}

func environmentJSONError(err error) *jsonCmdError {
	if errors.Is(err, context.Canceled) {
		return newJSONError(ErrCodeCancelled, err.Error())
	}
	var startup *workflow.StartupFailure
	if errors.As(err, &startup) {
		return newJSONError(startup.Code, startup.Error()).withDetail(startup.Detail()).withHint("Run omnideck logs for technical details. Retry after correcting the problem.")
	}
	var validation *workflow.ConfigValidationError
	if errors.As(err, &validation) {
		return newJSONError(ErrCodeInvalidSetting, err.Error()).withSetting(settingKey(validation.Field)).withHint("Correct this setting and retry. Nothing was changed.")
	}
	if errors.Is(err, workflow.ErrMissingStorage) {
		return newJSONError(ErrCodeMissingStorage, err.Error()).withHint("Do not create replacement volumes. Restore the named volume, or ask for support with this message.")
	}
	if errors.Is(err, workflow.ErrContainerNotFound) {
		return newJSONError(ErrCodeContainerNotFound, err.Error())
	}
	message := err.Error()
	lower := strings.ToLower(message)
	if errors.Is(err, workflow.ErrPortInUse) ||
		strings.Contains(lower, "already using port") ||
		strings.Contains(lower, "already uses port") ||
		strings.Contains(lower, "address already in use") ||
		strings.Contains(lower, "already allocated") ||
		strings.Contains(lower, "cannot listen on the tcp port") ||
		strings.Contains(lower, "ports are not available") {
		return newJSONError(ErrCodePortInUse, message).withHint("Choose another local port and retry.")
	}
	if errors.Is(err, workflow.ErrExternalURL) {
		return newJSONError(ErrCodeInternal, message).withHint("The update was applied and saved. Retry it; if the address is still wrong, run omnideck doctor.")
	}
	if errors.Is(err, workflow.ErrInstanceNameTaken) {
		return newJSONError(ErrCodeContainerConflict, message).withHint("Choose another instance name. That name or its data belongs to an existing Omnideck installation; do not remove it.")
	}
	if errors.Is(err, workflow.ErrContainerConflict) {
		return newJSONError(ErrCodeContainerConflict, message).withHint("Choose another instance name or remove the unrelated container.")
	}
	return newJSONError(ErrCodeInternal, message)
}

// settingKey maps a workflow.ConfigValidationError field to the saved-settings
// key (as `config set` and `config --json` name it), the one vocabulary the
// INVALID_SETTING "setting" field uses for every command.
func settingKey(field string) string {
	switch field {
	case "name":
		return "container_name"
	case "port":
		return "web_ui_port"
	default:
		return field
	}
}

func environmentStageJSONError(stage string, err error) *jsonCmdError {
	structured := environmentJSONError(err)
	if structured.payload.Code == ErrCodeInternal && (errors.Is(err, workflow.ErrImageDownload) || stage == "pull_image") {
		return newJSONError(ErrCodeDownloadFailed, err.Error()).withHint("Check the internet connection, then retry. Anything already prepared will be kept.")
	}
	return structured
}

func runEnvironmentEnsure(cmd *cobra.Command, _ []string) error {
	if nameFlag == "" {
		err := newJSONError(ErrCodeMissingRequiredFlag, "environment ensure requires --name")
		if jsonFlag {
			return writeJSONError(err)
		}
		return err
	}
	path := config.InstancePath(nameFlag)
	current, err := loadEnvironmentConfig(path)
	if err != nil {
		if jsonFlag {
			return writeJSONError(environmentJSONError(err))
		}
		return err
	}
	desired, err := desiredEnvironmentConfig(current)
	if err == nil {
		err = validateEnvironmentOwnership(desired)
	}
	if err != nil {
		if jsonFlag {
			return writeJSONError(environmentJSONError(err))
		}
		return err
	}

	eng, err := engine.Detect()
	if err != nil {
		structured := newJSONError(ErrCodeEngineNotFound, err.Error()).withAction(workflow.DoctorActionRuntimeSetup, "podman")
		if jsonFlag {
			return writeJSONError(structured)
		}
		return structured
	}

	stream := newEnsureStream(jsonFlag)
	result, ensureErr := workflow.EnsureInstance(eng, current, desired, func() error {
		return workflow.SaveInstanceConfig(eng.Name(), path, desired)
	}, stream.options())
	if ensureErr != nil {
		return stream.fail(ensureErr)
	}
	stream.startup()
	if err := workflow.WaitForApplication(cmd.Context(), eng, desired); err != nil {
		return stream.failStartup(err)
	}
	if stream.complete(result, gatherStatusPayload(nameFlag, desired, eng)) {
		return nil
	}
	fmt.Printf("Omnideck is ready at http://127.0.0.1:%s (%s).\n", desired.WebUIPortOrDefault(), result.Action)
	return nil
}

// ensureStream reports workflow.EnsureInstance stages as NDJSON for
// `environment ensure --json` and `repair --json`; without --json it is
// silent and leaves text output to the caller.
type ensureStream struct {
	nd        *ndjsonEncoder
	lastStage string
	started   bool
}

func newEnsureStream(json bool) *ensureStream {
	stream := &ensureStream{lastStage: "check_environment"}
	if json {
		stream.nd = newNDJSONEncoder()
	}
	return stream
}

func (s *ensureStream) options() workflow.EnsureInstanceOptions {
	return workflow.EnsureInstanceOptions{
		OnStage: s.stage,
		OnPullProgress: func(line string) {
			if s.nd != nil {
				s.nd.progress("pull_image", line)
			}
		},
	}
}

// stage finishes the running stage and starts the next one.
func (s *ensureStream) stage(stage string) {
	// A repeated stage (repair announces check_environment before
	// EnsureInstance does) continues the running one.
	if s.started && s.lastStage == stage {
		return
	}
	if s.nd != nil && s.started {
		s.nd.done(s.lastStage)
	}
	s.lastStage = stage
	s.started = true
	if s.nd != nil {
		s.nd.start(stage)
	}
}

// startup starts the stage that spans the wait for the application to
// answer, so the last workflow stage is not left open during it.
func (s *ensureStream) startup() {
	s.stage("startup")
}

// fail ends the stream with err on the stage that was running, or returns
// err unchanged without --json.
func (s *ensureStream) fail(err error) error {
	if s.nd == nil {
		return err
	}
	// Every error event follows a start for the same stage.
	if !s.started {
		s.nd.start(s.lastStage)
		s.started = true
	}
	return s.nd.fail(s.lastStage, environmentStageJSONError(s.lastStage, err))
}

func (s *ensureStream) failStartup(err error) error {
	if s.nd == nil {
		return startupTextError(err)
	}
	// Every error event follows a start for the same stage.
	s.startup()
	return s.nd.fail("startup", environmentJSONError(err))
}

// complete emits the final result and reports whether it did (under --json).
func (s *ensureStream) complete(result workflow.EnsureInstanceResult, status statusPayload) bool {
	if s.nd == nil {
		return false
	}
	s.nd.done(s.lastStage)
	s.nd.complete(environmentEnsurePayload{
		Changed: result.Changed,
		Action:  result.Action,
		Reason:  result.Reason,
		Status:  status,
	})
	return true
}
