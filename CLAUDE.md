# omnideck-cli — Claude Code Context

## What This Is

`omnideck-cli` is a Go CLI that installs and manages the **Omnideck** container application. The binary is named `omnideck` (not `omnideck-cli` — that's just the repo name). It uses Cobra for commands, Bubble Tea for TUI, and shells out to Docker or Podman.

## Full Spec

See `SPEC.md` for the complete product specification.

## Architecture

See `docs/architecture.md` for the current request flow, workflow state
machines, lifecycle rules, and persistence transaction boundaries. `SPEC.md`
and `PHASES.md` are historical product/build references; the architecture guide
and current tests describe how new code should be structured.

---

## Repo Layout

```
omnideck-cli/
├── main.go                  # Entry point — calls cmd.Execute()
├── cmd/
│   ├── root.go              # Cobra root, global flags, PersistentPreRun config loader
│   ├── setup.go
│   ├── list.go
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
│   ├── app.go               # Application shell and shared instance state
│   ├── app_update.go        # Global messages and route dispatch
│   ├── app_view.go          # Shared frame and route rendering
│   ├── router.go            # Stack-based screen navigation
│   ├── dialog.go            # Short blocking confirmations only
│   ├── screen_*_update.go   # Input handling for one routed screen
│   ├── screen_*_view.go     # Presentation for one routed screen
│   ├── screen_*.go          # Screen state and asynchronous commands
│   ├── setup*.go            # First-use, additional-instance, and runtime setup stages
│   ├── maintenance.go       # Review-first update and repair workflow
│   ├── doctor_report.go     # Plain report rendering for `omnideck doctor`
│   └── spinner.go           # Spinner + fading message component
├── workflow/
│   ├── container.go         # Idempotent lifecycle and transactional recreate
│   ├── diagnostics.go       # Shared Doctor diagnosis and guided actions
│   ├── instances.go         # Unique new-instance defaults
│   └── settings.go          # Shared settings validation/mutation
├── engine/
│   ├── engine.go            # Engine interface
│   ├── docker.go            # Docker shell-out implementation
│   ├── podman.go            # Podman shell-out implementation
│   └── setup.go             # Platform-specific runtime detection/setup plans
├── checks/
│   ├── ollama.go            # TCP dial check, OS-aware host
│   └── memory.go            # Linux: /proc/meminfo  Mac: sysctl
├── config/
│   └── config.go            # Load/save OS-native per-user configuration
└── styles/
    └── styles.go            # Lip Gloss palette and shared renderers
```

---

## Naming Conventions

- **Binary name:** `omnideck`
- **Repo name:** `omnideck-cli`
- **Module path:** `github.com/omnideck-dev/cli`
- **Config directory:** OS-native user config directory under `omnideck-cli`
- **Container image:** `ghcr.io/omnideck-dev/omnideck:main`
- **Default container name:** `omnideck`
- **Default data volumes:** `{container-name}-home` and `{container-name}-state`

---

## Key Dependencies

```
github.com/spf13/cobra
github.com/charmbracelet/bubbletea
github.com/charmbracelet/bubbles
github.com/charmbracelet/lipgloss
gopkg.in/yaml.v3
```

No Docker SDK—the engine adapters intentionally shell out to the installed CLI.
Keep external dependencies minimal.

---

## Platform Rules (Critical)

Runtime command construction is covered by cross-platform tests in `engine/`.
Current important differences include:

- Docker on Linux maps `host-gateway`; Docker Desktop uses
  `host.docker.internal`.
- Podman uses its runtime-specific host alias, with a macOS override.
- Runtime installation and recovery plans differ across Linux distributions,
  macOS architectures, Windows, and WSL.
- Host memory detection uses an OS-specific implementation.

Use the platform passed through `engine.RunOptions` or `HostPlatform`; never
hardcode a Linux-only command or flag in user-facing workflow code.

---

## Engine Interface

All Docker/Podman operations go through the `engine.Engine` interface. No
command should call `exec.Command("docker", ...)` directly. User-facing
commands and screens should normally call `workflow/` operations rather than
interpreting raw engine errors or rebuilding `engine.RunOptions` themselves.

The complete adapter contract lives in `engine/engine.go`. Shared workflows
define narrower interfaces containing only the engine operations they need,
which keeps tests small and prevents accidental coupling.

---

## TUI and Workflow Conventions

- All TUI programs use `tea.NewProgram(model, tea.WithAltScreen())`
- `AppModel` is the one interactive shell; the Dashboard is only its root screen
- Logs, Settings, Doctor, Setup, and Maintenance are full screens managed by `Router`
- Back navigation must pop the router so nested workflows return to their caller
- Use `ConfirmDialog` only for short blocking decisions; substantial journeys are screens
- Setup, runtime setup, settings, Doctor, and Maintenance have separate typed states; do not add a shared phase enum
- Constructors receive `SetupRequest` or `MaintenanceRequest`; callers should not construct a model and mutate it into another journey
- Setup and Maintenance use a **spinner + fading messages** pattern (see `tui/spinner.go`)
  - Active step: full brightness text
  - Completed steps: dimmed with a `✓` prefix
  - Failed steps: error color with a `✗` prefix
- Long-running steps (image pull) cycle flavor messages every ~2s via `tea.Tick`
- Doctor uses a parallel-check pattern and routes repairs through shared workflows
- Never block the Bubble Tea event loop — all I/O runs in `tea.Cmd` goroutines

---

## Config Load/Save

`config.Config` is loaded in `cmd/root.go`'s `PersistentPreRun`. Commands that
require an installed instance (`start`, `stop`, `status`, etc.) use
`requireConfigMulti`: one instance is selected automatically, multiple
instances use an interactive picker, and non-interactive calls require
`--name`. The shared Docker/Podman choice lives in `settings.yaml`, not in each
new instance file.

```go
type Config struct {
    ContainerName string    `yaml:"container_name"`
    HomeVolume    string    `yaml:"home_volume,omitempty"`
    StateVolume   string    `yaml:"state_volume,omitempty"`
    Memory        string    `yaml:"memory"`
    ShmSize       string    `yaml:"shm_size"`
    WebUIPort     string    `yaml:"web_ui_port"`
    Engine        string    `yaml:"engine"`   // legacy migration field only
    Image         string    `yaml:"image"`
    InstalledAt   time.Time `yaml:"installed_at"`
}
```

Volume override fields are optional. Use `cfg.HomeVolumeName()` and
`cfg.StateVolumeName()` so missing values derive from `{ContainerName}-home`
and `{ContainerName}-state`.

---

## Error Handling Style

- Surface actionable errors: tell the user *what to do*, not just what went wrong
- Use plain language in user-facing copy; do not assume terms such as "elevate"
- Explain that Omnideck runs as a container and that it keeps the agent and its software isolated
- Ollama is optional; report it neutrally and do not abort setup
- In TUI context, errors render in the error style from `styles.go` inside the existing view — don't call `os.Exit` mid-render
- After TUI exits, non-zero exit codes for actual failures
- Lifecycle changes must be idempotent and shared through `workflow/`
- Recreate/save flows must keep runtime and YAML state aligned and attempt rollback on failure

---

## Build & Test

```bash
go build -ldflags="-s -w" -o omnideck .        # build binary
go test ./...                 			# run all tests
go vet ./...                   			# vet
```

The binary should be a single static binary with no runtime dependencies beyond a container engine.
