//go:build windows

package checks

import "testing"

func TestWindowsMemoryStatus(t *testing.T) {
	total, err := TotalMemoryMB()
	if err != nil {
		t.Fatalf("TotalMemoryMB: %v", err)
	}
	available, err := AvailableMemoryMB()
	if err != nil {
		t.Fatalf("AvailableMemoryMB: %v", err)
	}
	if total <= 0 {
		t.Fatalf("total memory = %d MB, want a positive value", total)
	}
	if available <= 0 || available > total {
		t.Fatalf("available memory = %d MB, total = %d MB", available, total)
	}
}
