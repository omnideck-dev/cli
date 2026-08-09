package engine

import (
	"runtime"
	"testing"
)

func TestHostCommandEnvironmentDropsAppImageLoaderVariablesOnLinux(t *testing.T) {
	environment := []string{
		"PATH=/usr/bin",
		"LD_LIBRARY_PATH=/tmp/.mount_omnideck/usr/lib",
		"LD_PRELOAD=/tmp/.mount_omnideck/usr/lib/libfoo.so",
		"LD_AUDIT=/tmp/.mount_omnideck/usr/lib/libaudit.so",
		"HOME=/home/tester",
	}

	filtered := hostCommandEnvironment(environment)
	if runtime.GOOS != "linux" {
		if len(filtered) != len(environment) {
			t.Fatalf("non-Linux environment changed: %v", filtered)
		}
		return
	}

	for _, entry := range filtered {
		if entry == "LD_LIBRARY_PATH=/tmp/.mount_omnideck/usr/lib" ||
			entry == "LD_PRELOAD=/tmp/.mount_omnideck/usr/lib/libfoo.so" ||
			entry == "LD_AUDIT=/tmp/.mount_omnideck/usr/lib/libaudit.so" {
			t.Errorf("AppImage loader variable leaked into host command: %s", entry)
		}
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/tester"} {
		found := false
		for _, entry := range filtered {
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("host command environment lost %s", want)
		}
	}
}
