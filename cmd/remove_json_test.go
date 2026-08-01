package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func TestResolveRemoveOptionsRequiresYes(t *testing.T) {
	orig := removeYesFlag
	removeYesFlag = false
	defer func() { removeYesFlag = orig }()

	_, err := resolveRemoveOptions()
	if err == nil {
		t.Fatal("expected an error when --yes is not set")
	}
	var jerr *jsonCmdError
	if !errors.As(err, &jerr) || jerr.payload.Code != ErrCodeMissingRequiredFlag {
		t.Fatalf("expected MISSING_REQUIRED_FLAG, got %v", err)
	}
}

func TestResolveRemoveOptionsRequiresExactlyOneVolumeChoice(t *testing.T) {
	setRemoveFlags(t, true, false, false, false, false)
	if _, err := resolveRemoveOptions(); err == nil {
		t.Fatal("expected an error when neither --keep-volumes nor --delete-volumes is set")
	}
	setRemoveFlags(t, true, true, true, false, false)
	if _, err := resolveRemoveOptions(); err == nil {
		t.Fatal("expected an error when both --keep-volumes and --delete-volumes are set")
	}
}

func TestResolveRemoveOptionsRequiresBackupChoiceOnlyWithDeleteVolumes(t *testing.T) {
	setRemoveFlags(t, true, true, false, false, false)
	opts, err := resolveRemoveOptions()
	if err != nil {
		t.Fatalf("--keep-volumes without a backup choice should be valid: %v", err)
	}
	if opts.DeleteData {
		t.Fatal("DeleteData should be false with --keep-volumes")
	}

	setRemoveFlags(t, true, false, true, false, false)
	if _, err := resolveRemoveOptions(); err == nil {
		t.Fatal("expected an error: --delete-volumes requires --backup or --no-backup")
	}

	setRemoveFlags(t, true, false, true, true, false)
	opts, err = resolveRemoveOptions()
	if err != nil {
		t.Fatalf("resolveRemoveOptions: %v", err)
	}
	if !opts.DeleteData || !opts.BackupData {
		t.Fatalf("opts = %+v, want DeleteData and BackupData true", opts)
	}
}

func setRemoveFlags(t *testing.T, yes, keep, del, backup, noBackup bool) {
	t.Helper()
	origYes, origKeep, origDel, origBackup, origNoBackup :=
		removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag, removeBackupFlag, removeNoBackupFlag
	t.Cleanup(func() {
		removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag, removeBackupFlag, removeNoBackupFlag =
			origYes, origKeep, origDel, origBackup, origNoBackup
	})
	removeYesFlag, removeKeepVolumesFlag, removeDeleteVolumesFlag, removeBackupFlag, removeNoBackupFlag =
		yes, keep, del, backup, noBackup
}

func removeTestInstance(t *testing.T) config.InstanceInfo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.yaml")
	cfg := config.DefaultConfig()
	cfg.ContainerName = "demo"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return config.InstanceInfo{Name: "demo", Path: path, Config: cfg}
}

func TestRunRemoveJSONSuccessEmitsStagesAndCompletes(t *testing.T) {
	instance := removeTestInstance(t)
	eng := &mockEngine{
		containerExists: map[string]bool{"demo": true},
		containerStatus: map[string]string{"demo": "running"},
		volumes:         map[string]bool{"demo-home": true, "demo-state": true},
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runRemoveJSON(eng, instance, workflow.RemoveInstanceOptions{
			DeleteData: true, BackupData: true, BackupDir: t.TempDir(),
		})
	})
	if runErr != nil {
		t.Fatalf("runRemoveJSON: %v\noutput=%s", runErr, out)
	}

	events := decodeNDJSON(t, out)
	// Each applicable stage emits a start/done pair, in order, then complete.
	wantStagesInOrder := []string{"prepare", "stop_container", "backup", "remove_container", "delete_volumes"}
	if len(events) != len(wantStagesInOrder)*2+1 {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantStagesInOrder)*2+1, events)
	}
	for i, stage := range wantStagesInOrder {
		start, done := events[i*2], events[i*2+1]
		if start["stage"] != stage || start["state"] != "start" {
			t.Fatalf("event[%d] = %+v, want start of %s", i*2, start, stage)
		}
		if done["stage"] != stage || done["state"] != "done" {
			t.Fatalf("event[%d] = %+v, want done of %s", i*2+1, done, stage)
		}
	}
	last := events[len(events)-1]
	result, ok := last["result"].(map[string]any)
	if !ok {
		t.Fatalf("final event missing result: %+v", last)
	}
	volumes, ok := result["removedVolumes"].([]any)
	if !ok || len(volumes) != 2 {
		t.Fatalf("removedVolumes = %v, want 2 entries", result["removedVolumes"])
	}
}

func TestRunRemoveJSONKeepDataOmitsBackupAndDeleteStages(t *testing.T) {
	instance := removeTestInstance(t)
	eng := &mockEngine{
		containerExists: map[string]bool{"demo": true},
		containerStatus: map[string]string{"demo": "running"},
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runRemoveJSON(eng, instance, workflow.RemoveInstanceOptions{})
	})
	if runErr != nil {
		t.Fatalf("runRemoveJSON: %v\noutput=%s", runErr, out)
	}
	events := decodeNDJSON(t, out)
	for _, e := range events {
		if e["stage"] == "backup" || e["stage"] == "delete_volumes" {
			t.Fatalf("unexpected stage %v when keeping data: %+v", e["stage"], events)
		}
	}
}

func TestRunRemoveJSONCancellationReportsCancelled(t *testing.T) {
	instance := removeTestInstance(t)
	eng := &mockEngine{
		containerExists: map[string]bool{"demo": true},
		containerStatus: map[string]string{"demo": "running"},
		stopErr:         fmt.Errorf("docker stop: %w", context.Canceled),
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runRemoveJSON(eng, instance, workflow.RemoveInstanceOptions{})
	})
	if runErr != errAborted {
		t.Fatalf("expected errAborted, got %v", runErr)
	}
	events := decodeNDJSON(t, out)
	last := events[len(events)-1]
	errPayload := last["error"].(map[string]any)
	if errPayload["code"] != ErrCodeCancelled {
		t.Fatalf("code = %v, want %s", errPayload["code"], ErrCodeCancelled)
	}
}
