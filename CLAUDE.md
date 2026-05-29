# omnideck-cli — Claude Code Context

## What This Is

`omnideck-cli` is a Go CLI that installs and manages the **Omnideck** container application. The binary is named `omnideck` (not `omnideck-cli` — that's just the repo name). It uses Cobra for commands, Bubble Tea for TUI, and shells out to Docker or Podman.

## Full Spec

See `SPEC.md` for the complete product specification.

## Build Plan

See `PHASES.md` for the ordered, phase-by-phase build plan. Always check which phase is current before adding new code.

---

## Repo Layout

```
omnideck-cli/
├── main.go                  # Entry point — calls cmd.Execute()
├── cmd/
│   ├── root.go              # Cobra root, global flags, PersistentPreRun config loader
│   ├── install.go
│   ├── update.go
│   ├── start.go
│   ├── stop.go
│   ├── restart.go
│   ├── status.go
│   ├── logs.go
│   ├── doctor.go
│   ├── config.go
│   └── uninstall.go
├── tui/
│   ├── model.go             # Shared tea.Model, phase enum, Update dispatch
│   ├── install.go           # Install wizard phases
│   ├── doctor.go            # Doctor report view
│   └── spinner.go           # Spinner + fading message component
├── engine/
│   ├── engine.go            # Engine interface
│   ├── docker.go            # Docker shell-out implementation
│   └── podman.go            # Podman shell-out implementation
├── checks/
│   ├── ollama.go            # TCP dial check, OS-aware host
│   └── memory.go            # Linux: /proc/meminfo  Mac: sysctl
├── config/
│   └── config.go            # Load/save ~/.config/omnideck-cli/config.yaml
└── styles/
    └── styles.go            # Lip Gloss palette and shared renderers
```

---

## Naming Conventions

- **Binary name:** `omnideck`
- **Repo name:** `omnideck-cli`
- **Module path:** `github.com/omnideck-dev/cli`
- **Config file:** `~/.config/omnideck-cli/config.yaml`
- **Container image:** `ghcr.io/lefoulkrod/computron_9000:main`
- **Default container name:** `omnideck`
- **Default shared dir:** `~/Omnideck`
- **Default state dir:** `~/Omnideck/state`

---

## Key Dependencies

```
github.com/spf13/cobra
github.com/charmbracelet/bubbletea
github.com/charmbracelet/bubbles
github.com/charmbracelet/lipgloss
gopkg.in/yaml.v3
```

No Docker SDK — shell out only for Phase 1. Keep external deps minimal.

---

## Platform Rules (Critical)

These differences MUST be respected in all generated code:

| Concern              | Linux                        | macOS                                  |
|----------------------|------------------------------|----------------------------------------|
| Volume SELinux flag  | Append `:Z` to volume mounts | Omit `:Z` — it breaks on Mac           |
| Ollama host          | `127.0.0.1:11434`            | `host.docker.internal:11434`           |
| Network mode         | `--network host`             | `--network host` (warn user — limited) |
| Memory check         | `/proc/meminfo`              | `sysctl hw.memsize` + `vm_stat`        |

Use `runtime.GOOS` to branch. Never hardcode Linux-only flags without a platform guard.

---

## Engine Interface

All Docker/Podman operations go through the `engine.Engine` interface. No command should call `exec.Command("docker", ...)` directly — always go through the interface. This makes future SDK swaps or testing easier.

```go
type Engine interface {
    Name() string
    IsAvailable() bool
    HasPermission() bool
    ContainerExists(name string) (bool, error)
    PullImage(image string, msgs chan<- string) error
    RunContainer(opts RunOptions) error
    StopContainer(name string) error
    StartContainer(name string) error
    RemoveContainer(name string) error
    ContainerStatus(name string) (string, error)
    TailLogs(name string, follow bool) error
}
```

---

## TUI Conventions

- All TUI programs use `tea.NewProgram(model, tea.WithAltScreen())`
- The install/update phases use a **spinner + fading messages** pattern (see `tui/spinner.go`)
  - Active step: full brightness text
  - Completed steps: dimmed with a `✓` prefix
  - Failed steps: error color with a `✗` prefix
- Long-running steps (image pull) cycle flavor messages every ~2s via `tea.Tick`
- `doctor` uses a parallel-check pattern, not a sequential wizard
- Never block the Bubble Tea event loop — all I/O runs in `tea.Cmd` goroutines

---

## Config Load/Save

`config.Config` is loaded in `cmd/root.go`'s `PersistentPreRun`. Commands that require an installed instance (`start`, `stop`, `status`, etc.) should fail fast with a helpful message if config is not found.

```go
type Config struct {
    ContainerName string    `yaml:"container_name"`
    SharedDir     string    `yaml:"shared_dir"`
    StateDir      string    `yaml:"state_dir"`
    ShmSize       string    `yaml:"shm_size"`
    Engine        string    `yaml:"engine"`   // "docker" or "podman"
    Image         string    `yaml:"image"`
    InstalledAt   time.Time `yaml:"installed_at"`
}
```

---

## Error Handling Style

- Surface actionable errors: tell the user *what to do*, not just what went wrong
- Example: permission denied on Docker → print `sudo usermod -aG docker $USER`
- Example: Ollama not found → print install URL, don't abort install
- In TUI context, errors render in the error style from `styles.go` inside the existing view — don't call `os.Exit` mid-render
- After TUI exits, non-zero exit codes for actual failures

---

## Build & Test

```bash
go build -ldflags="-s -w" -o omnideck .        # build binary
go test ./...                 			# run all tests
go vet ./...                   			# vet
```

The binary should be a single static binary with no runtime dependencies beyond a container engine.
