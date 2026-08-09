# CLI architecture

The CLI is organized around user workflows with Podman as its one container
runtime. User-facing code should request an Omnideck outcome such as “ensure
this instance is stopped” instead of interpreting raw runtime errors itself.

## Request path

```text
main.go
  └─ cmd/                   Cobra commands and bare-command routing
       ├─ first use ──────> tui/ AppModel > InstallationSection
       ├─ runtime broken ─> tui/ AppModel > InstallationSection (repair)
       ├─ instance broken > tui/ AppModel > ControlPlaneSection > Doctor
       └─ returning user ─> tui/ AppModel > ControlPlaneSection
                                                   ├─ Dashboard
                                                   ├─ Logs
                                                   ├─ Settings
                                                   ├─ Doctor
                                                   ├─ Maintenance
                                                   └─ Removal

workflow/                 Shared lifecycle, settings, and diagnosis operations
engine/                   Raw Podman commands, platform policy, and setup plans

config/                   Platform-native persisted settings
checks/                   Host checks and input validation
styles/                   Terminal presentation primitives
```

`cmd/root.go` makes only the high-level entry decision. `AppModel` is the one
interactive application shell; there is no separate launcher with duplicate
start, stop, status, logs, or Doctor implementations.

## TUI sections and navigation

`AppModel` is a thin shell around two durable sections and a small stack-based
`Router`:

- `InstallationSection` owns the first-run and runtime-repair walkthrough. The
  desktop is the canonical setup experience; this section adapts its phases,
  copy, progress, and failures to a terminal.
- `ControlPlaneSection` owns day-to-day management after setup: Dashboard,
  Logs, Settings, Doctor, Maintenance, and Removal.

Every route belongs to exactly one section. Pushing a screen records its
caller, so Back returns to the actual previous screen—for example, Repair
opened by Doctor returns to Doctor for another health check.

Setup, Maintenance, and Removal remain independent Bubble Tea workflow models hosted by
the application shell. Their exit messages distinguish completion from
cancellation: a root-level cancel exits the TUI, while a completed first setup
continues to the Dashboard.

Short blocking decisions use `ConfirmDialog`. A dialog captures input before
the active screen and defaults to the safe choice. Substantial journeys must
not be implemented as dialogs.

## State machines

Each workflow owns its states. They deliberately do not share one generic
phase enum, because that would allow impossible combinations such as Update
being in Setup's runtime-selection state.

| Workflow | States or modes |
| --- | --- |
| Application sections | Installation or Control Plane |
| Application router | Dashboard, Logs, Settings, Doctor, Setup, Maintenance, Removal routes |
| Setup | Welcome, Quick check, Runtime, Settings, Review, Applying, Complete, Failed |
| Runtime setup | Working or Restart needed |
| Settings | Editing, Applying |
| Doctor | Checking, Results, Acting |
| Maintenance | Update or Repair mode; Review, Applying, Complete, Failed |
| Removal | Data choice, Review or Backup choice, Delete confirmation, Applying, Complete, Failed |

Workflow constructors accept request values such as `SetupRequest`,
`MaintenanceRequest`, and `RemovalRequest`. This makes mode, target, runtime,
and embedded/standalone behavior explicit before a workflow starts. A workflow
should not be constructed and then mutated into another journey by its caller.

## Container lifecycle rules

`engine.Engine` is intentionally a thin adapter over Podman. Raw runtime
behavior still differs from application semantics—for example, stopping an
already stopped container can be an error. `workflow/` provides the application
semantics used everywhere:

- `EnsureStarted`, `EnsureStopped`, and `EnsureRemoved` are idempotent.
- `EnsureInstance` is the idempotent create/start/repair/update transaction used
  by Desktop and automation. It owns image pulls, volumes, replacement,
  rollback, and persistence.
- `RunOptions` is the only config-to-container mapping. It leaves the
  container-facing Ollama hostname to the selected engine and host platform.
- `Recreate` removes and replaces a container, then attempts to restore the
  previous container configuration if the replacement fails.
- `NewInstanceDefaults` owns unique name and browser-port suggestions.
- `ApplySetting` owns the editable settings surface and syntax validation.
- `Diagnose` owns Doctor's runtime, instance, volume, and host checks for both
  the interactive screen and the plain `omnideck doctor` report.
- `RemoveInstance` stops and removes one container, keeps its data by default,
  and owns optional backup and permanent volume deletion for both CLI and TUI.

Commands and TUI screens should call these operations rather than calling raw
start/stop/remove methods or rebuilding `engine.RunOptions` themselves.

## Platform runtime policy

Host differences are contained in `engine` and represented as setup plans or a
small Podman platform policy. Views do not construct platform commands.

- Windows and macOS always use the named `omnideck-runtime` Podman machine and
  explicitly select its connection for container operations.
- Creating that machine does not replace an existing user's default Podman
  connection. An unrelated developer machine is never adopted.
- Linux uses native Podman directly and never receives machine or connection
  flags.
- Linux host commands clear AppImage-private dynamic-loader variables before
  invoking distribution tools or Podman helpers, so host `conmon` loads the
  distribution's GLib rather than a bundled desktop copy.
- Windows machine creation uses the WSL provider and user-mode networking.
  macOS leaves CPU and sparse-disk defaults to Podman and sets only a detected
  memory ceiling that remains 2 GB above the container limit.
- Apple Silicon and Intel macOS select separately reviewed official installer
  assets. Linux distribution selection and privilege escalation are separate
  policies, so derivatives can follow `ID_LIKE` and root is never forced
  through `sudo`.
- Linux Desktop setup uses a graphical PolicyKit request. The interactive CLI
  may fall back to `sudo` because it owns a real terminal; the Desktop backend
  never launches a password prompt into an invisible pipe.
- Setup installer and runtime helper commands are split by host in
  `engine/runtime_setup_*_host.go`. On Windows, `engine/process_windows.go`
  suppresses console windows only for console helpers; the UAC prompt remains
  visible and MSI installation runs quietly with its result captured for the
  TUI/Desktop.

`engine.EnsureRuntime` owns prerequisite installation, machine creation and
repair, progress events, verification, and typed failures. The TUI calls it
directly. The versioned `runtime status --json` and `runtime ensure --json`
commands expose the same backend to Desktop. Bare `omnideck` routes a fresh or
broken computer into this workflow automatically; users do not invoke the
internal runtime command.

Runtime-contract schema 4 also carries `DefaultRuntimeResources`: the shared
container memory/shared-memory policy plus the platform-specific origin of
machine resources. Desktop must consume those values instead of carrying its
own container defaults.

Desktop is a native browser shell and update presenter, not a second runtime
implementation. It selects an immutable release image, renders the shared
progress stream, and invokes `runtime ensure`, `status`, `start`, and
`environment ensure` through its bundled CLI. It never invokes Podman, manages
the named machine, creates volumes, or writes instance YAML itself. A Desktop
update is therefore applied through the same transaction used for a fresh
install and repair.

Each front end renders the shared events in its native UI. Desktop handles its
own restart button and one-time relaunch; the direct CLI registers its own
RunOnce resume only after the user chooses **Restart now** in the TUI. The CLI
resume command explicitly opens a new visible console before starting the TUI;
it never assumes that Windows RunOnce supplied interactive terminal handles.

## Persistence and transactions

`config/` stores one YAML file per instance in the operating system's
conventional user config directory. A legacy runtime setting may still be read
and migrated, but new installations always use Podman. Instance data lives in
named container volumes and is not stored in the YAML file.

Settings apply is ordered as a small transaction:

1. Build and validate a candidate config without changing the live config.
2. Recreate the container with the candidate.
3. Save the candidate only after the container starts.
4. If recreate or save fails, restore the previous container when possible and
   keep the previous saved config.

Update and Repair also begin on a review screen and allow retry. No mutation is
started from a model's `Init` method before user confirmation.

Instance removal deletes its saved YAML file last. If a runtime, backup, or
volume operation fails, the instance therefore stays visible and the user can
retry. Permanent data deletion requires an explicit choice and an exact-name
confirmation; keeping the named volumes is the default.

## Adding or changing behavior

- Add user entry commands and selector behavior under `cmd/`.
- Add workflow transitions and presentation under `tui/`.
- Add shared business behavior under `workflow/`.
- Add runtime-specific command construction under `engine/`.
- Add persisted fields and platform paths under `config/`.
- Add host probes or reusable validation under `checks/`.
- Keep OS-specific setup behavior in the corresponding
  `engine/runtime_setup_*_host.go` file and process-window behavior in
  `engine/process_windows.go` / `engine/process_other.go`.

Tests should cover state transitions, idempotent outcomes, transaction failure,
and parity between command and TUI call sites. Cross-platform engine command
construction remains covered in `engine/`; real-hardware scenarios are listed
in `TESTING.md` and exercised by the nightly runner scripts when runners exist.
