package workflow

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
)

type fakeRemovalEngine struct {
	containerExists bool
	status          string
	volumes         map[string]bool
	stopErr         error
	removeErr       error
	volumeErr       map[string]error
	exports         map[string]string
	removedVolumes  []string
}

func (f *fakeRemovalEngine) ContainerExists(string) (bool, error) { return f.containerExists, nil }
func (f *fakeRemovalEngine) ContainerStatus(string) (string, error) {
	return f.status, nil
}
func (f *fakeRemovalEngine) StopContainer(string) error {
	if f.stopErr == nil {
		f.status = "exited"
	}
	return f.stopErr
}
func (f *fakeRemovalEngine) RemoveContainer(string) error {
	if f.removeErr == nil {
		f.containerExists = false
	}
	return f.removeErr
}
func (f *fakeRemovalEngine) VolumeExists(name string) (bool, error) {
	return f.volumes[name], nil
}
func (f *fakeRemovalEngine) RemoveVolume(name string) error {
	if err := f.volumeErr[name]; err != nil {
		return err
	}
	f.removedVolumes = append(f.removedVolumes, name)
	delete(f.volumes, name)
	return nil
}
func (f *fakeRemovalEngine) ExportVolume(name string, w io.Writer) error {
	_, err := io.WriteString(w, f.exports[name])
	return err
}

func removalFixture(t *testing.T) (config.InstanceInfo, *fakeRemovalEngine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.yaml")
	cfg := config.DefaultConfig()
	cfg.ContainerName = "demo"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return config.InstanceInfo{Name: "demo", Path: path, Config: cfg}, &fakeRemovalEngine{
		containerExists: true,
		status:          "running",
		volumes:         map[string]bool{"demo-home": true, "demo-state": true},
		volumeErr:       map[string]error{},
		exports:         map[string]string{"demo-home": "home-data", "demo-state": "state-data"},
	}
}

func TestRemoveInstanceKeepsDataByDefault(t *testing.T) {
	instance, eng := removalFixture(t)
	result, err := RemoveInstance(eng, instance, RemoveInstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ContainerStopped || !result.ContainerRemoved || len(result.RemovedVolumes) != 0 || result.BackupPath != "" {
		t.Fatalf("result = %#v", result)
	}
	if len(eng.volumes) != 2 {
		t.Fatalf("kept volumes = %#v", eng.volumes)
	}
	if _, err := os.Stat(instance.Path); !os.IsNotExist(err) {
		t.Fatalf("saved config still exists: %v", err)
	}
}

func TestRemoveInstanceCanBackUpAndDeleteData(t *testing.T) {
	instance, eng := removalFixture(t)
	backupDir := t.TempDir()
	result, err := RemoveInstance(eng, instance, RemoveInstanceOptions{
		DeleteData: true,
		BackupData: true,
		BackupDir:  backupDir,
		Now: func() time.Time {
			return time.Date(2026, 7, 22, 12, 0, 0, 123, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath == "" || filepath.Dir(result.BackupPath) != backupDir {
		t.Fatalf("backup path = %q", result.BackupPath)
	}
	if !reflect.DeepEqual(result.RemovedVolumes, []string{"demo-home", "demo-state"}) {
		t.Fatalf("removed volumes = %#v", result.RemovedVolumes)
	}
	archive, err := os.Open(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewReader(gz)
	contents := map[string]string{}
	for {
		header, err := tw.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tw)
		if err != nil {
			t.Fatal(err)
		}
		contents[header.Name] = string(data)
	}
	if len(contents) != 2 || contents["home.tar"] != "home-data" || contents["state.tar"] != "state-data" {
		t.Fatalf("backup contents = %#v", contents)
	}
	info, err := os.Stat(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRemoveInstanceFailureKeepsSavedSettings(t *testing.T) {
	instance, eng := removalFixture(t)
	eng.removeErr = errors.New("runtime refused")
	_, err := RemoveInstance(eng, instance, RemoveInstanceOptions{})
	if err == nil || !strings.Contains(err.Error(), "saved settings were kept") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(instance.Path); err != nil {
		t.Fatalf("saved config was removed: %v", err)
	}
}

func TestRemoveInstanceReportsBackupWhenDataRemovalPartiallyFails(t *testing.T) {
	instance, eng := removalFixture(t)
	eng.volumeErr["demo-state"] = errors.New("volume is busy")
	result, err := RemoveInstance(eng, instance, RemoveInstanceOptions{
		DeleteData: true,
		BackupData: true,
		BackupDir:  t.TempDir(),
	})
	if err == nil || result.BackupPath == "" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if !reflect.DeepEqual(result.RemovedVolumes, []string{"demo-home"}) {
		t.Fatalf("removed volumes = %#v", result.RemovedVolumes)
	}
	if _, statErr := os.Stat(result.BackupPath); statErr != nil {
		t.Fatalf("reported backup does not exist: %v", statErr)
	}
	if _, statErr := os.Stat(instance.Path); statErr != nil {
		t.Fatalf("saved config was removed after partial failure: %v", statErr)
	}
}

func TestRemoveInstanceRejectsBackupWithoutDeletionBeforeMutation(t *testing.T) {
	instance, eng := removalFixture(t)
	_, err := RemoveInstance(eng, instance, RemoveInstanceOptions{BackupData: true})
	if err == nil || !strings.Contains(err.Error(), "only needed") {
		t.Fatalf("error = %v", err)
	}
	if !eng.containerExists || eng.status != "running" {
		t.Fatalf("instance changed before validation: %#v", eng)
	}
}
