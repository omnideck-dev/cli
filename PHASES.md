# omnideck-cli — Build Phases

Each phase is self-contained and leaves the codebase in a working, buildable state. Complete phases in order. Do not skip ahead.

Reference `SPEC.md` for detailed behavior specs and `CLAUDE.md` for conventions.

---

## Phase 1 — Scaffold & Cobra Root

**Goal:** Repo initializes, `omnideck --help` works, global flags are wired, config loads.

### Tasks

- [ ] Initialize Go module: `github.com/omnideck-dev/cli`
- [ ] Add dependencies to `go.mod`:
  - `github.com/spf13/cobra`
  - `github.com/charmbracelet/bubbletea`
  - `github.com/charmbracelet/bubbles`
  - `github.com/charmbracelet/lipgloss`
  - `gopkg.in/yaml.v3`
- [ ] Create `main.go` — calls `cmd.Execute()`
- [ ] Create `cmd/root.go`:
  - Root `omnideck` command with short/long description
  - Global persistent flags: `--config`, `--no-color`, `--debug`, `--version`
  - `PersistentPreRun` hook: attempt to load config; store in a package-level var; don't fail if not found (some commands like `install` don't require it)
  - Version output via `--version` flag using build-time ldflags vars
- [ ] Create `config/config.go`:
  - `Config` struct (see `CLAUDE.md`)
  - `Load(path string) (*Config, error)` — reads YAML, expands `~`
  - `Save(path string, cfg *Config) error` — writes YAML, creates parent dirs
  - `DefaultPath() string` — returns `~/.config/omnideck-cli/config.yaml`
- [ ] Create stub commands (just `cmd.AddCommand` + placeholder `Run` printing "not yet implemented"):
  - `install`, `update`, `start`, `stop`, `restart`, `status`, `logs`, `doctor`, `config`, `uninstall`
- [ ] Verify: `go build -o omnideck .` succeeds and `./omnideck --help` lists all commands

### Acceptance Criteria

```
$ ./omnideck --help        # shows usage + all commands
$ ./omnideck --version     # shows "omnideck version dev ..."
$ ./omnideck install       # prints "not yet implemented"
$ go vet ./...              # no errors
```

---

## Phase 2 — Engine Package

**Goal:** Docker and Podman detection and core container operations work, fully abstracted behind an interface.

### Tasks

- [ ] Create `engine/engine.go`:
  - Define `Engine` interface (see `CLAUDE.md` for the full interface)
  - Define `RunOptions` struct (see `SPEC.md`)
  - `Detect() (Engine, error)` — tries Podman first, then Docker; returns first available; errors if neither found

- [ ] Create `engine/docker.go` — `DockerEngine` implementing `Engine`:
  - `Name()` returns `"docker"`
  - `IsAvailable()` — `exec.LookPath("docker")`
  - `HasPermission()` — runs `docker ps` and checks for permission error
  - `ContainerExists(name)` — `docker ps -a --filter name=^name$`
  - `PullImage(image, msgs chan<- string)` — `docker pull image`, sends lines to `msgs` channel
  - `RunContainer(opts RunOptions)` — builds and runs the full `docker run` command (see `SPEC.md` for args); platform-aware (`:Z`, `host.docker.internal`)
  - `StopContainer(name)` — `docker stop name`
  - `StartContainer(name)` — `docker start name`
  - `RemoveContainer(name)` — `docker rm -f name`
  - `ContainerStatus(name)` — `docker inspect --format '{{.State.Status}}' name`
  - `TailLogs(name, follow)` — `docker logs [--follow] [--tail N] name`, streams to stdout

- [ ] Create `engine/podman.go` — `PodmanEngine` implementing `Engine`:
  - Same methods, substituting `podman` for `docker`
  - `RunContainer`: use `--replace` flag instead of pre-removing
  - No permission check needed (rootless Podman)

- [ ] In all engine methods: if `--debug` global flag is set, print the full constructed command to stderr before running

- [ ] Write `engine/engine_test.go`:
  - Test `Detect()` logic with mock PATH
  - Test `RunOptions` → command construction (don't need a real engine, test flag assembly)

### Acceptance Criteria

```
$ go test ./engine/...      # passes
$ go vet ./...              # no errors
```

---

## Phase 3 — Checks Package

**Goal:** OS-aware Ollama and memory checks are implemented and testable.

### Tasks

- [ ] Create `checks/ollama.go`:
  - `OllamaHost() string` — returns `127.0.0.1:11434` on Linux, `host.docker.internal:11434` on macOS
  - `CheckOllama() (reachable bool, host string)` — TCP dial with 2s timeout

- [ ] Create `checks/memory.go`:
  - `AvailableMemoryMB() (int64, error)`:
    - Linux: parse `/proc/meminfo` for `MemAvailable` line
    - macOS: run `sysctl -n hw.memsize` for total; `vm_stat` for free pages × page size (use available approximation)
  - `MemoryWarning(availMB int64) string` — returns warning string if < 512MB, empty string if fine

- [ ] Write `checks/checks_test.go`:
  - Test Linux `MemAvailable` parsing with a fixture string
  - Test macOS `vm_stat` parsing with a fixture string
  - Test `OllamaHost()` returns correct value per GOOS

### Acceptance Criteria

```
$ go test ./checks/...      # passes
$ go vet ./...              # no errors
```

---

## Phase 4 — Styles Package

**Goal:** Shared Lip Gloss styles defined once, used everywhere.

### Tasks

- [ ] Create `styles/styles.go` with package-level `lipgloss.Style` vars:
  - `Title` — bold, primary accent color
  - `Dim` — muted/gray for completed steps
  - `Success` — green
  - `Error` — red
  - `Warning` — yellow
  - `Active` — bright white or accent, for current step text
  - `Border` — rounded border style for summary/status panels
  - `CheckMark` — `"✓"` rendered in `Success` style
  - `CrossMark` — `"✗"` rendered in `Error` style
  - `Bullet` — `"●"` for status dots
  - `NoColor(enabled bool)` — sets `lipgloss.NoColor` based on `--no-color` flag; call in root `PersistentPreRun`

- [ ] Color palette should work on both dark and light terminals — use adaptive colors where possible (`lipgloss.AdaptiveColor`)

### Acceptance Criteria

```
$ go build ./...            # no errors
```

---

## Phase 5 — TUI: Spinner + Fading Messages Component

**Goal:** Reusable Bubble Tea component used by `install` and `update`.

### Tasks

- [ ] Create `tui/spinner.go` — a self-contained Bubble Tea component (`Model`, `Init`, `Update`, `View`):

  **State:**
  - `steps []Step` where `Step` has: `label string`, `status StepStatus` (pending/active/done/failed), `detail string`
  - `currentStep int`
  - `flavorMessages []string` — cycling messages for the active step
  - `flavorIndex int`
  - `spinner bubbles/spinner.Model`

  **Messages:**
  - `StepStartedMsg{Index int}`
  - `StepDoneMsg{Index int, Detail string}`
  - `StepFailedMsg{Index int, Err error}`
  - `FlavorTickMsg{}` — internal, drives flavor message rotation

  **View rendering:**
  - Completed steps: `styles.Dim` + `styles.CheckMark` + label + detail
  - Failed steps: `styles.Error` + `styles.CrossMark` + label + error
  - Active step: spinner + `styles.Active` + current flavor message
  - Pending steps: not shown (appear as active when their turn comes)

  **Flavor tick:** `tea.Tick(2*time.Second, ...)` rotates `flavorIndex` while a step is active

- [ ] Create `tui/model.go`:
  - `Phase` enum: `PhasePreflight`, `PhaseConfig`, `PhaseConfirm`, `PhaseInstall`, `PhaseDone`, `PhaseError`
  - Base `Model` struct with `phase Phase` and `windowWidth/Height int`
  - `tea.WindowSizeMsg` handler to update dimensions

### Acceptance Criteria

```
$ go build ./...            # no errors
```

Write a simple standalone test program in `tui/spinner_demo/main.go` (not committed to main build, excluded via build tag) that runs the spinner with fake steps to visually verify rendering. Remove after verification.

---

## Phase 6 — `install` Command (Full TUI)

**Goal:** `omnideck install` runs the complete wizard end-to-end.

### Tasks

- [ ] Create `tui/install.go` — install-specific Bubble Tea model managing all 5 phases:

  **Preflight phase:**
  - Launch 4 concurrent `tea.Cmd` goroutines for: engine, permissions, Ollama, memory
  - Each sends a typed result message back
  - Engine + permissions failures transition to `PhaseError` with remediation message
  - Ollama + memory failures add warnings but auto-advance after all checks complete
  - Auto-advance to config phase when all checks done

  **Config phase:**
  - Use `bubbles/textinput` — one active input at a time
  - Fields: container name, shared dir, state dir, shm size
  - Tab/Enter advances to next field; Shift+Tab goes back
  - Inline validation on advance (shm size format, non-empty strings)
  - `[Enter]` on last field advances to confirm

  **Confirm phase:**
  - Render summary in `styles.Border` box
  - Show: engine, OS, all config values, Ollama warning if applicable
  - Show macOS network warning if on macOS
  - Keys: `i` → install, `b` → back to config, `q` → quit

  **Install phase:**
  - Use `tui/spinner.go` component
  - Steps: create shared dir, create state dir, remove existing container (skip if not found), pull image, run container, save config
  - Pull image uses flavor messages cycling
  - On any step failure → `PhaseError`
  - On complete → `PhaseDone`

  **Done phase:**
  - Big success header
  - Show access URLs: `http://localhost:2337`
  - Show Ollama endpoint (OS-aware)
  - Show paths
  - `Press any key to exit`

  **Error phase:**
  - Show which step failed
  - Show error detail
  - Show remediation hint (looked up from error type)
  - `Press any key to exit`

- [ ] Update `cmd/install.go` to run the Bubble Tea program

### Acceptance Criteria

```
$ ./omnideck install       # full wizard runs, container is created
$ ./omnideck status        # shows running (even though status is stub)
$ cat ~/.config/omnideck-cli/config.yaml   # config written correctly
```

---

## Phase 7 — `status` Command

**Goal:** `omnideck status` renders a formatted status panel.

### Tasks

- [ ] Update `cmd/status.go`:
  - Load config (fail fast if not found)
  - Detect engine from config
  - Gather concurrently: container status, Ollama reachable, dir existence
  - Render table using Lip Gloss (no Bubble Tea program — static output)
  - Exit 0 if running, 1 if stopped or error

### Acceptance Criteria

```
$ ./omnideck status        # shows table, correct status
$ ./omnideck status; echo $?   # exit code 0=running 1=stopped
```

---

## Phase 8 — `start`, `stop`, `restart` Commands

**Goal:** Basic lifecycle commands with minimal TUI feedback.

### Tasks

- [ ] Each command: load config → engine op → print result (no full TUI, just a single spinner line via `bubbletea` or even plain `fmt` + spinner from `bubbles`)
- [ ] `restart` = stop then start, two operations
- [ ] Graceful handling of "already stopped" / "already running" — warn, don't error

### Acceptance Criteria

```
$ ./omnideck stop && ./omnideck start   # works
$ ./omnideck restart                     # works
```

---

## Phase 9 — `update` Command

**Goal:** `omnideck update` pulls latest image and recreates container.

### Tasks

- [ ] Update `cmd/update.go`:
  - Reuse `tui/spinner.go`
  - Steps: load config, pull image (with flavor messages), stop, remove, run, done
  - No config prompts — use saved config

### Acceptance Criteria

```
$ ./omnideck update        # pulls and recreates, spinner shown
```

---

## Phase 10 — `doctor` Command

**Goal:** Deep health check with parallel checks and static report output.

### Tasks

- [ ] Create `tui/doctor.go` — parallel check runner:
  - All checks fire concurrently
  - Each check returns `CheckResult{Label, Status, Detail, Hint}`
  - Collect all results, then render static report
  - Checks: engine version, permissions, container status, image freshness (compare `docker inspect` digest), Ollama, port 2337 dial, shared dir writable, state dir writable, memory, OS/arch

- [ ] Update `cmd/doctor.go` to run checks and render report
- [ ] Exit 1 if any check fails

### Acceptance Criteria

```
$ ./omnideck doctor        # full report rendered
$ ./omnideck doctor; echo $?   # 0=all pass 1=any fail
```

---

## Phase 11 — `logs` Command

**Goal:** Stream container logs to terminal.

### Tasks

- [ ] Update `cmd/logs.go`:
  - Flags: `--follow/-f` (default true), `--tail int` (default 50)
  - Calls `engine.TailLogs()` — streams directly to stdout
  - Ctrl+C exits cleanly (trap SIGINT)
  - No Bubble Tea — direct streaming

### Acceptance Criteria

```
$ ./omnideck logs          # streams logs
$ ./omnideck logs --tail 10 --follow=false   # last 10 lines, exits
```

---

## Phase 12 — `config` Subcommands

**Goal:** View and edit saved config.

### Tasks

- [ ] Update `cmd/config.go` with subcommands:
  - `config show` — pretty-print YAML config with Lip Gloss formatting
  - `config set <key> <value>` — update one key, validate key name, save
  - `config path` — print config file path
- [ ] Valid settable keys: `container_name`, `shared_dir`, `state_dir`, `shm_size`
- [ ] Invalid key → error with list of valid keys

### Acceptance Criteria

```
$ ./omnideck config show
$ ./omnideck config set shm_size 512m
$ ./omnideck config path
```

---

## Phase 13 — `uninstall` Command

**Goal:** Clean teardown with confirmation prompts.

### Tasks

- [ ] Update `cmd/uninstall.go`:
  - Cobra confirmation prompt (Bubble Tea or plain stdin): `Are you sure? [y/N]`
  - Stop container, remove container
  - Second prompt: `Delete data directories? [y/N]`
  - If yes: `os.RemoveAll` shared and state dirs
  - Remove config file
  - Print confirmation

### Acceptance Criteria

```
$ ./omnideck uninstall     # prompts, then cleans up
```

---

## Phase 14 — Polish & Release Prep

**Goal:** Production-ready binary.

### Tasks

- [ ] Add `Makefile` with targets:
  - `build` — `go build -ldflags "..." -o omnideck .`
  - `test` — `go test ./...`
  - `vet` — `go vet ./...`
  - `lint` — `golangci-lint run` (if available)
  - `release` — cross-compile for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`

- [ ] Add `--version` build-time ldflags wired in `Makefile`

- [ ] Write `README.md`:
  - Install instructions
  - Quick start
  - Full command reference
  - Requirements (Docker or Podman, Ollama)
  - macOS notes

- [ ] Review all error messages against the UX reference in `SPEC.md`

- [ ] Add `--no-color` respected everywhere (check `styles.NoColor` is called in all render paths)

- [ ] Final: `go vet ./...` clean, `go test ./...` all pass

### Acceptance Criteria

```
$ make build                # produces ./omnideck
$ make test                 # all pass
$ make release              # produces binaries for all 4 targets
$ ./omnideck --help        # full, polished help output
```

---

## Phase Summary

| Phase | Focus | Key Output |
|-------|-------|------------|
| 1 | Scaffold + Cobra | `omnideck --help` works |
| 2 | Engine package | Docker/Podman abstraction |
| 3 | Checks package | Ollama + memory checks |
| 4 | Styles | Lip Gloss palette |
| 5 | Spinner TUI component | Reusable fading-step widget |
| 6 | `install` command | Full wizard, end-to-end |
| 7 | `status` command | Status panel |
| 8 | `start/stop/restart` | Lifecycle commands |
| 9 | `update` command | Pull + recreate |
| 10 | `doctor` command | Health report |
| 11 | `logs` command | Streaming logs |
| 12 | `config` subcommands | Config CRUD |
| 13 | `uninstall` command | Clean teardown |
| 14 | Polish + release | Makefile, README, cross-compile |
