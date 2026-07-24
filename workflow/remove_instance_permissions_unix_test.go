//go:build linux || darwin

package workflow

import (
	"os"
	"testing"
)

func TestRemoveInstanceBackupIsOwnerOnly(t *testing.T) {
	instance, eng := removalFixture(t)
	result, err := RemoveInstance(eng, instance, RemoveInstanceOptions{
		DeleteData: true,
		BackupData: true,
		BackupDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions = %o, want 600", info.Mode().Perm())
	}
}
