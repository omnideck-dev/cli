package checks

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const memWarnThresholdMB = 512

// AvailableMemoryMB returns the available system memory in megabytes.
// On Linux it parses /proc/meminfo; on macOS it uses vm_stat.
func AvailableMemoryMB() (int64, error) {
	switch runtime.GOOS {
	case "linux":
		return availableMemoryLinux()
	case "darwin":
		return availableMemoryDarwin()
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// TotalMemoryMB returns the total installed system memory in megabytes.
// On Linux it parses /proc/meminfo; on macOS it uses sysctl hw.memsize.
func TotalMemoryMB() (int64, error) {
	switch runtime.GOOS {
	case "linux":
		return totalMemoryLinux()
	case "darwin":
		return totalMemoryDarwin()
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func totalMemoryLinux() (int64, error) {
	data, err := readProcMeminfo()
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("unexpected MemTotal format: %q", line)
			}
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing MemTotal: %w", err)
			}
			return kb / 1024, nil
		}
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

func totalMemoryDarwin() (int64, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing hw.memsize: %w", err)
	}
	return bytes / (1024 * 1024), nil
}

// DefaultContainerMemory returns suggested --memory and --shm-size strings
// based on total system RAM using M = max(1, min(floor(0.2*HostRAM_GB), 8)).
// SHM is set to 50% of the memory value.
func DefaultContainerMemory(totalMB int64) (memory, shmSize string) {
	totalGB := totalMB / 1024
	memGB := int64(float64(totalGB) * 0.2)
	if memGB < 1 {
		memGB = 1
	}
	if memGB > 8 {
		memGB = 8
	}
	shmMB := memGB * 1024 / 2
	return fmt.Sprintf("%dg", memGB), fmt.Sprintf("%dm", shmMB)
}

// MemoryWarning returns a warning string if availMB < 512, otherwise "".
func MemoryWarning(availMB int64) string {
	if availMB < memWarnThresholdMB {
		return fmt.Sprintf("Warning: only %d MB available (recommended: ≥ 512 MB)", availMB)
	}
	return ""
}

// readProcMeminfo is a variable so tests can replace it.
var readProcMeminfo = func() (string, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// availableMemoryLinux parses /proc/meminfo.
func availableMemoryLinux() (int64, error) {
	data, err := readProcMeminfo()
	if err != nil {
		return 0, err
	}
	return parseMemInfoString(data)
}

// parseMemInfoString parses a /proc/meminfo string and returns MemAvailable in MB.
func parseMemInfoString(data string) (int64, error) {
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("unexpected MemAvailable format: %q", line)
			}
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing MemAvailable: %w", err)
			}
			return kb / 1024, nil
		}
	}
	return 0, fmt.Errorf("MemAvailable not found in /proc/meminfo")
}

// availableMemoryDarwin approximates available memory on macOS using vm_stat.
func availableMemoryDarwin() (int64, error) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, fmt.Errorf("vm_stat: %w", err)
	}
	return parseVMStat(string(out))
}

// parseVMStat parses vm_stat output and returns approximate free MB.
func parseVMStat(data string) (int64, error) {
	pageSize := int64(4096) // Default macOS page size.

	var freePages, inactivePages, speculativePages int64

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			freePages = parseVMStatLine(line)
		case strings.HasPrefix(line, "Pages inactive:"):
			inactivePages = parseVMStatLine(line)
		case strings.HasPrefix(line, "Pages speculative:"):
			speculativePages = parseVMStatLine(line)
		case strings.Contains(line, "page size of"):
			// "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "of" && i+1 < len(fields) {
					if ps, err := strconv.ParseInt(fields[i+1], 10, 64); err == nil {
						pageSize = ps
					}
				}
			}
		}
	}

	totalFreeBytes := (freePages + inactivePages + speculativePages) * pageSize
	return totalFreeBytes / (1024 * 1024), nil
}

func parseVMStatLine(line string) int64 {
	// Format: "Pages free:                         12345."
	parts := strings.Split(line, ":")
	if len(parts) < 2 {
		return 0
	}
	s := strings.TrimSpace(parts[1])
	s = strings.TrimSuffix(s, ".")
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
