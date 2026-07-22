//go:build !windows

package checks

import "fmt"

func availableMemoryWindows() (int64, error) {
	return 0, fmt.Errorf("Windows memory status is unavailable on this operating system")
}

func totalMemoryWindows() (int64, error) {
	return 0, fmt.Errorf("Windows memory status is unavailable on this operating system")
}
