//go:build windows

package checks

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// memoryStatusEx mirrors the Windows MEMORYSTATUSEX structure. Keeping the
// Windows API call in this file lets the rest of the memory logic stay portable.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func readWindowsMemoryStatus() (memoryStatusEx, error) {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, callErr := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return memoryStatusEx{}, fmt.Errorf("GlobalMemoryStatusEx: %w", callErr)
	}
	return status, nil
}

func availableMemoryWindows() (int64, error) {
	status, err := readWindowsMemoryStatus()
	if err != nil {
		return 0, err
	}
	return int64(status.AvailPhys / (1024 * 1024)), nil
}

func totalMemoryWindows() (int64, error) {
	status, err := readWindowsMemoryStatus()
	if err != nil {
		return 0, err
	}
	return int64(status.TotalPhys / (1024 * 1024)), nil
}
