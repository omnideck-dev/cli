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
	Status  statusPayload `json:"status"`
}

func desiredEnvironmentConfig(current *config.Config) (*config.Config, error) {
	desired := config.DefaultConfig()
	if current != nil {
		copy := *current
		desired = &copy
	}
	if nameFlag != "" {
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
	if err := workflow.ValidateInstanceConfig(desired); err != nil {
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

func validateEnvironmentPort(desired *config.Config) error {
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("checking existing installations: %w", err)
	}
	for _, instance := range instances {
		if instance.Config == nil || instance.Config.ContainerName == desired.ContainerName {
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
	if errors.Is(err, workflow.ErrContainerConflict) || strings.Contains(lower, "already uses the name") {
		return newJSONError(ErrCodeContainerConflict, message).withHint("Choose another instance name or remove the unrelated container.")
	}
	return newJSONError(ErrCodeInternal, message)
}

func environmentStageJSONError(stage string, err error) *jsonCmdError {
	structured := environmentJSONError(err)
	if structured.payload.Code == ErrCodeInternal && (errors.Is(err, workflow.ErrImageDownload) || stage == "pull_image") {
		return newJSONError(ErrCodeDownloadFailed, err.Error()).withHint("Check the internet connection, then retry. Anything already prepared will be kept.")
	}
	return structured
}

func runEnvironmentEnsure(_ *cobra.Command, _ []string) error {
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
		err = validateEnvironmentPort(desired)
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

	var nd *ndjsonEncoder
	if jsonFlag {
		nd = newNDJSONEncoder()
	}
	lastStage := "check_environment"
	opts := workflow.EnsureInstanceOptions{
		OnStage: func(stage string) {
			if nd != nil && lastStage != "" && lastStage != stage {
				nd.done(lastStage)
			}
			lastStage = stage
			if nd != nil {
				nd.start(stage)
			}
		},
		OnPullProgress: func(line string) {
			if nd != nil {
				nd.progress("pull_image", line)
			}
		},
	}

	result, ensureErr := workflow.EnsureInstance(eng, current, desired, func() error {
		if err := config.SaveRuntime(eng.Name()); err != nil {
			return err
		}
		return config.Save(path, desired)
	}, opts)
	if ensureErr != nil {
		if nd != nil {
			return nd.fail(lastStage, environmentStageJSONError(lastStage, ensureErr))
		}
		return ensureErr
	}
	payload := environmentEnsurePayload{
		Changed: result.Changed,
		Action:  result.Action,
		Status:  gatherStatusPayload(desired, eng),
	}
	if nd != nil {
		nd.done(lastStage)
		nd.complete(payload)
		return nil
	}
	fmt.Printf("Omnideck is ready at http://127.0.0.1:%s (%s).\n", desired.WebUIPortOrDefault(), result.Action)
	return nil
}
