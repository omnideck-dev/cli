# omnideck-cli — Product Specification

## Overview

`omnideck` is a cross-platform CLI tool (macOS and Linux) for installing, managing, and monitoring the **Omnideck** containerized application. It wraps Docker/Podman shell commands in a polished Bubble Tea TUI with animated spinners, fading status messages, and clear error guidance.

The binary is `omnideck`. The repo is `omnideck-cli`.

---

## Commands

### `omnideck install`

Full interactive TUI wizard. The primary command — runs all phases in sequence.

**Phases:**

1. **Preflight** — checks run in parallel, results stream in as they complete
   - Container engine detected (Docker or Podman) — **blocking failure if not found**
   - Docker socket permissions (`docker ps` exits 0) — **blocking failure if denied**
   - Ollama reachable on port 11434 — **warning only, do not block**
   - Available memory ≥ 512MB — **warning if low, do not block**
   - OS detected and displayed

2. **Configuration** — sequential text input fields, Tab/Enter to advance
   - Container name (default: `omnideck`)
   - Shared directory (default: `~/Omnideck`)
   - State directory (default: `~/Omnideck/state`)
   - SHM size (default: `256m`) — validate format (number + unit: `256m`, `1g`)

3. **Confirm** — summary panel in a Lip Gloss border box showing all choices + engine + OS notes. Keys: `[i]` install, `[b]` back, `[q]` quit

4. **Install** — sequential steps with spinner + fading messages
   - Create shared directory
   - Create state directory
   - Remove existing container (if present)
   - Pull image (`ghcr.io/lefoulkrod/computron_9000:main`)
   - Run container
   - Write config to `~/.config/omnideck-cli/config.yaml`

5. **Done / Error** — success banner with access URLs, or error box with remediation hint

**Flavor messages for long operations (image pull):**

```
Pulling from the mothership...
Downloading the good stuff...
This might take a minute...
Almost there...
Unpacking intelligence...
```

**macOS-specific behavior:**

- Warn that `--network host` behaves differently on Docker Desktop
- Use `host.docker.internal:11434` in container env for Ollama
- Omit `:Z` from volume mounts

---

### `omnideck update`

Pulls the latest image and recreates the container, preserving the saved config. No config prompts.

**Steps (TUI with spinner):**
1. Load config — fail fast if not installed
2. Pull latest image
3. Stop existing container
4. Remove existing container
5. Run container with same options
6. Done

---

### `omnideck start`

Start a stopped container. Reads container name from config.

- TUI: one-line spinner → done message
- Error if container doesn't exist (suggest `omnideck install`)

---

### `omnideck stop`

Gracefully stop the running container.

- TUI: one-line spinner → done message
- Warn if already stopped, don't error

---

### `omnideck restart`

Stop then start. Same TUI pattern as start/stop.

---

### `omnideck status`

One-shot, no TUI animation. Renders a status table:

```
  Omnideck Status
  ─────────────────────────────
  Container     omnideck       ● running
  Image         ...latest       up to date (or: newer available)
  Engine        docker 24.0.1
  Shared dir    ~/Omnideck     ✓ exists
  State dir     ~/Omnideck/state  ✓ exists
  Ollama        ✓ reachable at 127.0.0.1:11434
  Web UI        http://localhost:2337
```

Use Lip Gloss for the table layout. Green dot for running, red for stopped.

---

### `omnideck logs`

Tail container logs. Streams to stdout. Ctrl+C exits cleanly.

- Flag: `--follow` / `-f` (default true)
- Flag: `--tail int` (default 50)
- Not a Bubble Tea program — stream directly to terminal

---

### `omnideck doctor`

Deep parallel health check. Renders a full report (not a wizard).

Checks:
- Container engine present + version
- Docker socket permissions
- Container exists + running
- Image is latest (compare digest)
- Ollama reachable (OS-aware host)
- Port 2337 reachable (is Omnideck responding?)
- Shared dir exists and is writable
- State dir exists and is writable
- Available memory
- OS + arch

Each check shows: `✓ label — detail` or `✗ label — detail [hint]`

Output is a static rendered report (not animated). Exits 0 if all pass, 1 if any fail.

---

### `omnideck config`

View and optionally edit the saved config.

**Subcommands:**

- `omnideck config show` — pretty-print current config
- `omnideck config set <key> <value>` — update a single key
  - Valid keys: `container_name`, `shared_dir`, `state_dir`, `shm_size`
- `omnideck config path` — print the config file path

No TUI for config — plain terminal output.

---

### `omnideck uninstall`

Stop and remove the container. Prompts before deleting data directories.

**Steps:**
1. Confirm prompt: `Are you sure? This will stop and remove the container. [y/N]`
2. Stop container
3. Remove container
4. Prompt: `Delete shared data directories? [y/N]`
5. If yes: remove `shared_dir` and `state_dir`
6. Remove config file

TUI: spinner per step, confirmation prompts use Bubble Tea text prompts.

---

## Global Flags

```
--config string    Path to config file (default: ~/.config/omnideck-cli/config.yaml)
--no-color         Disable color/style output
--debug            Print raw engine commands and output
--version          Print version and exit
```

`--debug` causes all `exec.Command` calls to print the full command and captured stderr/stdout.

---

## Config File

Location: `~/.config/omnideck-cli/config.yaml`

```yaml
container_name: omnideck
shared_dir: /home/user/Omnideck
state_dir: /home/user/Omnideck/state
shm_size: 256m
engine: docker                  # "docker" or "podman"
image: ghcr.io/lefoulkrod/computron_9000:main
installed_at: 2025-01-15T10:30:00Z
```

---

## Container Run Options

The `engine.RunOptions` struct captures all parameters for `docker run`:

```go
type RunOptions struct {
    Name        string
    Image       string
    ShmSize     string
    SharedDir   string
    StateDir    string
    Network     string   // "host"
    Restart     string   // "always"
    OllamaHost  string   // 127.0.0.1 or host.docker.internal
    Platform    string   // runtime.GOOS
}
```

**Linux docker run (equivalent):**

```bash
docker run -d \
  --name omnideck \
  --restart always \
  --shm-size=256m \
  --network host \
  -v ~/Omnideck:/home/omnideck:Z \
  -v ~/Omnideck/state:/var/lib/omnideck:Z \
  ghcr.io/lefoulkrod/computron_9000:main
```

**macOS differences:**

- No `:Z` on volume mounts
- Add `-e OLLAMA_HOST=http://host.docker.internal:11434` (exact env var name TBD — document as a known assumption)
- Warn user about `--network host` limitation in Docker Desktop

**Podman differences:**

- Use `--replace` instead of pre-removing the container
- `--network host` works on Linux Podman

---

## Memory Size Validation

SHM size input must match: `^\d+[mMgGkK]$`

Examples of valid: `256m`, `512M`, `1g`, `1G`
Examples of invalid: `256`, `256mb`, `abc`

---

## Ollama Check

```go
// Linux
host := "127.0.0.1:11434"

// macOS
host := "host.docker.internal:11434"

// Actually try to dial it
conn, err := net.DialTimeout("tcp", host, 2*time.Second)
```

If unreachable: show warning with install URL `https://ollama.com` — never block install.

---

## Memory Check

**Linux** — parse `/proc/meminfo`:
```
MemAvailable: 4096000 kB
```

**macOS** — use `sysctl`:
```bash
sysctl -n hw.memsize      # total bytes
vm_stat                   # parse for free pages × page size
```

Warn if available memory < 512MB. Show actual value in warning.

---

## Version Info

Embed version at build time via ldflags:

```go
// cmd/root.go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```

```bash
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse --short HEAD)"
```

`omnideck --version` outputs:
```
omnideck version 1.0.0 (abc1234) built 2025-01-15
```

---

## Error UX Reference

| Situation | Message |
|---|---|
| No container engine | `Error: Neither Docker nor Podman was found.\nInstall Docker: https://docs.docker.com/get-docker/` |
| Docker permission denied | `Error: Docker found but permission denied.\nFix: sudo usermod -aG docker $USER\nThen log out and back in.` |
| Config not found | `Error: Omnideck is not installed.\nRun: omnideck install` |
| Container not found | `Error: Container 'omnideck' not found.\nRun: omnideck install` |
| Ollama not reachable | `Warning: Ollama not found at [host]:11434\nInstall: https://ollama.com\nYou can install Omnideck now and start Ollama later.` |

---

## Out of Scope (Phase 1)

- Docker SDK (using shell-out only)
- Windows support
- `shell` / exec-into-container command
- `backup` / `restore`
- Auto-update / self-update of the CLI binary
- Container build / image management
