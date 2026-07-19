# GPU Passthrough Support

## Overview

Add GPU passthrough to the Omnideck container with full detection and configuration via the install wizard. Covers NVIDIA, AMD, and Intel on Linux; Apple Silicon on macOS.

## Detected environment (reference machine)

- **card0** `0x10de` → NVIDIA GeForce RTX 4060 Laptop GPU (8 GB VRAM) → `/dev/dri/renderD129`
- **card1** `0x8086` → Intel Iris Xe Graphics → `/dev/dri/renderD128`
- `/dev/kfd` present (ROCm kernel module loaded)
- `nvidia-container-toolkit` installed, CDI spec at `/etc/cdi/nvidia.yaml`
- Driver: 610.43.02, CUDA 13.3

---

## Container flags by GPU vendor × engine

| GPU | Docker | Podman |
|-----|--------|--------|
| NVIDIA (Linux) | `--gpus all` | `--device nvidia.com/gpu=all --security-opt=label=disable` |
| AMD (Linux) | `--device=/dev/dri --device=/dev/kfd --group-add=video --group-add=render` | `--device=/dev/dri --device=/dev/kfd --group-add=video --group-add=render` |
| Intel (Linux) | `--device=/dev/dri --group-add=video --group-add=render` | `--device=/dev/dri --group-add=video --group-add=render` |
| Apple Silicon (macOS) | `--gpus all` (Docker Desktop only) | not supported — warn user |
| Intel Mac (macOS) | not supported | not supported |

**AMD note:** `/dev/kfd` is required for ROCm compute (Ollama GPU acceleration). If only `/dev/dri` is present, DRI/VAAPI still works but ROCm compute will not — surface this as a warning rather than blocking install.

**AMD generation note:** ROCm support varies significantly by GPU generation. RDNA2 (RX 6000 series) is well-supported; RDNA3 (RX 7000) has been inconsistent; GCN 5 (RX 5000) is borderline. Detection will cross-reference `rocm-smi --showproductname` output against known-supported generations and set `Warning` accordingly rather than a binary `Available` flag. This prevents the false-positive of detecting a RX 7900 XTX as "available" when Ollama will silently fall back to CPU.

**NVIDIA + Docker note:** Requires `nvidia-container-runtime` registered in the Docker daemon (`/etc/docker/daemon.json`). Podman uses CDI natively (no daemon config needed) and is the preferred path on Linux.

**Podman AMD/Intel + rootless note:** `--group-add=video` and `--group-add=render` add groups by name inside the container, but rootless Podman runs in a user namespace and device cgroup rules may block access regardless. Detection will check for rootless Podman and surface a warning: `rootless Podman + AMD/Intel GPU may require 'podman machine' or running rootful — test with: podman run --rm --device=/dev/dri ubuntu ls /dev/dri`.

**`--group-add` consistency:** Both Docker and Podman use explicit `--group-add=video --group-add=render`. The earlier `--group-add=keep-groups` for Podman was dropped — it only preserves existing groups and is not a substitute for adding video/render if the user isn't already in them.

---

## Changes

### `checks/gpu.go` (new file)

**Detection strategy:** Primary source is `/sys/class/drm/card*/device/vendor` (reliable, no subprocess). `lspci` is used as a supplemental scan to catch cards that may not yet have a `/dev/dri` render node (e.g. driver not fully loaded). `nvidia-smi` is a *confirmation* step only — not primary detection — to avoid brittleness on headless or data-center configs.

PCI vendor IDs: `0x10de` NVIDIA, `0x1002` AMD, `0x8086` Intel.

```go
type GPUKind string // "nvidia" | "amd" | "intel" | "apple" | "none"

type GPUInfo struct {
    Kind       GPUKind
    Name       string  // e.g. "NVIDIA GeForce RTX 4060 Laptop GPU (8 GB)"
    VRAM       string  // e.g. "8192 MiB" — shown in TUI; empty if unknown
    RenderNode string  // e.g. "/dev/dri/renderD129"
    Available  bool    // true when container passthrough prerequisites are met
    Warning    string  // e.g. "nvidia-container-toolkit not installed"
}

func DetectGPUs() []GPUInfo  // all present GPUs
func PrimaryGPU() GPUInfo    // best candidate: NVIDIA > AMD > Intel > Apple
```

**Linux detection per vendor:**

- **NVIDIA:** PCI vendor `0x10de` → primary detection. Confirm toolkit: CDI spec at `/etc/cdi/nvidia.yaml` or `nvidia-container-runtime` in PATH. If toolkit present, run `nvidia-smi --query-gpu=name,memory.total --format=csv,noheader` to populate `Name` and `VRAM`. Mark `Available: false` with actionable warning if toolkit absent.
- **AMD:** PCI vendor `0x1002` → primary detection. Name from `rocm-smi --showproductname`. VRAM from `rocm-smi --showmeminfo vram`. Cross-reference generation against known ROCm support matrix; set `Warning` if generation is borderline or unsupported. `Available: true` only if `/dev/kfd` exists and generation is known-good; `Available: false` (DRI-only mode) otherwise.
- **Intel:** PCI vendor `0x8086`. Distinguish **Arc discrete** (device IDs `0x56xx`, `0x7dx0`) from **Iris Xe / UHD integrated** — Arc may require oneAPI or different kernel modules and should surface a note. `Available: true` if `/dev/dri/renderD*` exists. VRAM not reliably available; omit.

**macOS detection:**
- Run `system_profiler SPDisplaysDataType -json` for GPU name and VRAM.
- `arm64` → `GPUApple`, `Available: true` only when Docker is the selected engine (Podman uses a Linux VM with no Metal passthrough).
- `amd64` → Intel Mac, `Available: false`, no container GPU passthrough supported.

### `config/config.go`

Add field to `Config`:

```go
GPUEnabled bool `yaml:"gpu_enabled,omitempty"`
```

### `engine/engine.go`

Add fields to `RunOptions`:

```go
GPUEnabled bool
GPUKind    string // propagated from GPUInfo.Kind
```

### `engine/docker.go` — `buildRunArgs()`

When `opts.GPUEnabled`:

```go
switch opts.GPUKind {
case "nvidia":
    args = append(args, "--gpus", "all")
case "amd":
    args = append(args, "--device=/dev/dri", "--device=/dev/kfd",
        "--group-add=video", "--group-add=render")
case "intel":
    args = append(args, "--device=/dev/dri",
        "--group-add=video", "--group-add=render")
case "apple":
    args = append(args, "--gpus", "all")
}
```

### `engine/podman.go` — `buildPodmanRunArgs()`

```go
switch opts.GPUKind {
case "nvidia":
    args = append(args, "--device", "nvidia.com/gpu=all",
        "--security-opt=label=disable")
case "amd":
    args = append(args, "--device=/dev/dri", "--device=/dev/kfd",
        "--group-add=video", "--group-add=render")
case "intel":
    args = append(args, "--device=/dev/dri",
        "--group-add=video", "--group-add=render")
case "apple":
    // Podman on macOS uses a Linux VM — GPU passthrough not supported
}
```

### `engine/docker.go` and `podman.go` — Ollama GPU env vars

When `opts.GPUEnabled`, also append:

```go
args = append(args,
    "-e", "OLLAMA_NUM_GPU=99",     // use all GPU layers; 0 = CPU only
    "-e", "OLLAMA_KEEP_ALIVE=5m",  // keep model loaded between requests
)
```

`OLLAMA_NUM_GPU=99` is the conventional "use all available GPU layers" value for Ollama. `OLLAMA_KEEP_ALIVE` is set to prevent Ollama from unloading the model immediately after inference, which would negate GPU warm-up time. Both can be overridden by the user via future `omnideck config` subcommand.

### `tui/install.go`

**Preflight phase:**
- New `gpuCheckResult` message (runs in parallel with engine/ollama/memory checks)
- New row in `viewPreflight()` showing name + VRAM when known:
  - Available → `✓  NVIDIA GeForce RTX 4060 Laptop GPU  (8 GB)`
  - Warning → `⚠  AMD RX 7900 XTX  (ROCm support for RDNA3 is unstable)`
  - DRI-only → `⚠  AMD GPU  (ROCm unavailable — /dev/kfd missing; DRI/VAAPI only)`
  - Not available → `✗  Intel GPU  (GPU passthrough not supported on Intel Mac)`
  - No GPU → row omitted entirely

**Config phase:**
- GPU toggle rendered below text inputs when a GPU was detected
- `[Y] Enable GPU  /  [N] Disable` — toggled with Space or Enter
- When `Available: false`, toggle is shown but locked to N with the warning inline
- Defaults to **off** (explicit opt-in)
- Security note shown when user enables: `GPU passthrough exposes host driver and all GPU devices to the container.`

**Confirm phase:**
- Add `GPU` row to summary table: `enabled (NVIDIA GeForce RTX 4060 Laptop GPU, 8 GB)` or `disabled`

**Install phase:**
- `buildConfig()` sets `GPUEnabled` from toggle state
- `startInstallStep(4)` passes `GPUKind` in `RunOptions`

### `cmd/install.go`

Add `--gpu` boolean flag for non-interactive installs:

```
omnideck install --plain --gpu
```

Wire `GPUEnabled` and `GPUKind` (auto-detected via `checks.PrimaryGPU()`) through to `RunOptions` in `runInstallPlain()`.

---

## Warnings surfaced in TUI

| Condition | Message |
|-----------|---------|
| NVIDIA found, no toolkit | `nvidia-container-toolkit not installed — run: nvidia-ctk runtime configure` |
| NVIDIA + Docker, no daemon config | `nvidia-container-runtime not registered — run: nvidia-ctk runtime configure --runtime=docker` |
| AMD RDNA3 / borderline generation | `ROCm support for this GPU generation is unstable — Ollama may fall back to CPU` |
| AMD, `/dev/kfd` missing | `ROCm compute unavailable; /dev/dri only (DRI/VAAPI rendering, no compute)` |
| AMD/Intel + rootless Podman | `rootless Podman + AMD/Intel GPU may require rootful mode — test device access before enabling` |
| Apple Silicon + Podman selected | `GPU passthrough requires Docker Desktop on macOS` |
| Intel Mac | `GPU passthrough not supported on Intel Mac` |
| Intel Arc discrete | `Intel Arc requires oneAPI drivers — verify kernel module with: modinfo i915` |
| GPU enabled (any) | `GPU passthrough exposes host driver and all GPU devices to the container` |

---

## Multi-GPU behavior

On systems with multiple GPUs (e.g. NVIDIA + Intel iGPU), `PrimaryGPU()` picks the highest-capability device (NVIDIA > AMD > Intel). The selected GPU name and VRAM are shown in the wizard. `--gpus all` passes all NVIDIA GPUs; on multi-NVIDIA workstations this is expected behavior. No GPU selector UI in this pass — `--gpus "device=0"` targeting is deferred.

---

## Rollback / recovery

GPU is stored in config as `gpu_enabled: true|false`. If the container fails to start with GPU enabled:
- The install wizard already rolls back the container on step failure and surfaces the error.
- The user can re-run `omnideck install` with GPU disabled, or edit `~/.config/omnideck-cli/instances/<name>.yaml` and set `gpu_enabled: false` before running `omnideck start`.
- A future `omnideck config set gpu_enabled false` subcommand (tracked separately) would make this more ergonomic.

---

## Out of scope for this issue

- Windows GPU passthrough
- GPU selector for multi-NVIDIA or mixed-vendor systems
- Passing GPU through to an already-running container (`omnideck restart --gpu`)
- AMD GPU detection on macOS
- `omnideck config set` subcommand for post-install GPU toggle
