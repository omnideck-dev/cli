package checks

import (
	"runtime"
	"strings"
	"testing"
)

// --- Memory tests ---

const sampleMemInfo = `MemTotal:       16310292 kB
MemFree:         1234567 kB
MemAvailable:    8192000 kB
Buffers:          512000 kB
Cached:          4096000 kB
`

func TestParseMemInfoString(t *testing.T) {
	mb, err := parseMemInfoString(sampleMemInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 8192000 kB / 1024 = 8000 MB
	if mb != 8000 {
		t.Errorf("expected 8000 MB, got %d", mb)
	}
}

func TestParseMemInfoStringMissing(t *testing.T) {
	_, err := parseMemInfoString("MemTotal: 16000000 kB\n")
	if err == nil {
		t.Fatal("expected error when MemAvailable is missing")
	}
}

func TestParseMemInfoStringLowMemory(t *testing.T) {
	data := "MemAvailable: 102400 kB\n" // 100 MB
	mb, err := parseMemInfoString(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mb != 100 {
		t.Errorf("expected 100 MB, got %d", mb)
	}
	warning := MemoryWarning(mb)
	if warning == "" {
		t.Error("expected a warning for low memory")
	}
}

func TestMemoryWarningAboveThreshold(t *testing.T) {
	w := MemoryWarning(1024)
	if w != "" {
		t.Errorf("expected no warning for 1024 MB, got %q", w)
	}
}

func TestMemoryWarningBelowThreshold(t *testing.T) {
	w := MemoryWarning(256)
	if w == "" {
		t.Error("expected warning for 256 MB")
	}
	if !strings.Contains(w, "256") {
		t.Errorf("warning should include actual MB value, got %q", w)
	}
}

// --- vm_stat parsing tests ---

const sampleVMStat = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                          32768.
Pages active:                       524288.
Pages inactive:                     131072.
Pages speculative:                   16384.
Pages throttled:                         0.
Pages wired down:                   200000.
`

func TestParseVMStat(t *testing.T) {
	mb, err := parseVMStat(sampleVMStat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (32768 + 131072 + 16384) * 4096 = 180224 * 4096 = 738,197,504 bytes ≈ 704 MB
	expected := int64((32768 + 131072 + 16384) * 4096 / (1024 * 1024))
	if mb != expected {
		t.Errorf("expected %d MB, got %d", expected, mb)
	}
}

func TestParseVMStatLargePageSize(t *testing.T) {
	data := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                          8192.
Pages inactive:                      4096.
Pages speculative:                   1024.
`
	mb, err := parseVMStat(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (8192 + 4096 + 1024) * 16384 = 13312 * 16384 = 218,103,808 bytes ≈ 208 MB
	expected := int64(13312 * 16384 / (1024 * 1024))
	if mb != expected {
		t.Errorf("expected %d MB, got %d", expected, mb)
	}
}

// --- Ollama host tests ---

func TestOllamaHostCurrentPlatform(t *testing.T) {
	h := OllamaHost()
	// OllamaHost is the address the CLI dials to check whether Ollama is running.
	// The CLI runs on the host and Ollama listens on the host loopback, so this
	// is 127.0.0.1 on every platform — notably NOT host.docker.internal, which
	// only resolves inside containers, not from the host process.
	if h != "127.0.0.1:11434" {
		t.Errorf("expected 127.0.0.1:11434 on all platforms, got %q", h)
	}
}

// TestTotalMemoryLinuxMocked tests TotalMemoryMB on Linux using a mock.
func TestTotalMemoryLinuxMocked(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	orig := readProcMeminfo
	readProcMeminfo = func() (string, error) {
		return sampleMemInfo, nil
	}
	defer func() { readProcMeminfo = orig }()

	mb, err := TotalMemoryMB()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 16310292 kB / 1024 = 15928 MB
	if mb != 15928 {
		t.Errorf("expected 15928 MB, got %d", mb)
	}
}

// TestDefaultContainerMemory verifies the formula M = max(1, min(floor(0.2*RAM_GB), 8)).
func TestDefaultContainerMemory(t *testing.T) {
	cases := []struct {
		totalMB int64
		wantMem string
		wantShm string
	}{
		{512, "1g", "512m"},    // 0.5 GB  → < 6 GB tier
		{4096, "1g", "512m"},   // 4 GB   → < 6 GB tier
		{8192, "2g", "1024m"},  // 8 GB   → 6–11 GB tier
		{16384, "3g", "1536m"}, // 16 GB  → 12–23 GB tier
		{32768, "4g", "2048m"}, // 32 GB  → 24–47 GB tier
		{65536, "6g", "3072m"}, // 64 GB  → ≥ 48 GB tier
	}
	for _, c := range cases {
		mem, shm := DefaultContainerMemory(c.totalMB)
		if mem != c.wantMem {
			t.Errorf("totalMB=%d: memory got %q, want %q", c.totalMB, mem, c.wantMem)
		}
		if shm != c.wantShm {
			t.Errorf("totalMB=%d: shmSize got %q, want %q", c.totalMB, shm, c.wantShm)
		}
	}
}

// TestAvailableMemoryLinuxMocked tests AvailableMemoryMB on Linux using a mock.
func TestAvailableMemoryLinuxMocked(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	orig := readProcMeminfo
	readProcMeminfo = func() (string, error) {
		return sampleMemInfo, nil
	}
	defer func() { readProcMeminfo = orig }()

	mb, err := AvailableMemoryMB()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mb != 8000 {
		t.Errorf("expected 8000 MB, got %d", mb)
	}
}
