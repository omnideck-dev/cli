# CLI architecture

The CLI is organized around user workflows, with Docker and Podman treated as
replaceable infrastructure. User-facing code should request an Omnideck outcome
such as “ensure this instance is stopped” instead of interpreting raw runtime
errors itself.

## Request path

```text
main.go
  └─ cmd/                   Cobra commands and bare-command routing
       ├─ first use ──────> tui/ Setup
       ├─ runtime broken ─> tui/ Setup (runtime-repair mode)
       ├─ instance broken > tui/ Doctor > tui/ Maintenance (repair mode)
       └─ returning user ─> tui/ Dashboard
                                ├─ Setup (additional instance)
                                ├─ Settings apply
                                ├─ Doctor
                                └─ Maintenance (update or repair)

workflow/                 Shared Omnideck operations
  └─ engine/              Raw Docker and Podman commands and host setup plans

config/                   Platform-native persisted settings
checks/                   Host checks and input validation
styles/                   Terminal presentation primitives
```

`cmd/root.go` makes only the high-level entry decision. The Dashboard is the
single interactive shell for returning users; there is no separate launcher
with duplicate start, stop, status, logs, or Doctor implementations.

## State machines

Each workflow owns its states. They deliberately do not share one generic
phase enum, because that would allow impossible combinations such as Update
being in Setup's runtime-selection state.

| Workflow | States or modes |
| --- | --- |
| Dashboard | Dashboard, Logs, Settings, Doctor, Setup, Maintenance |
| Setup | Quick check, Runtime, Settings, Review, Applying, Complete, Failed |
| Runtime setup | Choose, Review, Working, Waiting |
| Settings | Editing, Applying |
| Doctor | Checking, Results, Acting |
| Maintenance | Update or Repair mode; Review, Applying, Complete, Failed |

Constructors accept `SetupRequest` or `MaintenanceRequest`, which makes mode,
target, runtime, and embedded/standalone behavior explicit before a workflow
starts. A workflow should not be constructed and then mutated into another
journey by its caller.

## Container lifecycle rules

`engine.Engine` is intentionally a thin adapter over Docker or Podman. Raw
runtime behavior differs—for example, stopping an already stopped container can
be an error. `workflow/` provides the application semantics used everywhere:

- `EnsureStarted`, `EnsureStopped`, and `EnsureRemoved` are idempotent.
- `RunOptions` is the only config-to-container mapping. It leaves the
  container-facing Ollama hostname to the selected engine and host platform.
- `Recreate` removes and replaces a container, then attempts to restore the
  previous container configuration if the replacement fails.
- `NewInstanceDefaults` owns unique name and browser-port suggestions.
- `ApplySetting` owns the editable settings surface and syntax validation.

Commands and TUI screens should call these operations rather than calling raw
start/stop/remove methods or rebuilding `engine.RunOptions` themselves.

## Persistence and transactions

`config/` stores one shared runtime choice plus one YAML file per instance in
the operating system's conventional user config directory. Instance data lives
in named container volumes and is not stored in the YAML file.

Settings apply is ordered as a small transaction:

1. Build and validate a candidate config without changing the live config.
2. Recreate the container with the candidate.
3. Save the candidate only after the container starts.
4. If recreate or save fails, restore the previous container when possible and
   keep the previous saved config.

Update and Repair also begin on a review screen and allow retry. No mutation is
started from a model's `Init` method before user confirmation.

## Adding or changing behavior

- Add user entry commands and selector behavior under `cmd/`.
- Add workflow transitions and presentation under `tui/`.
- Add shared business behavior under `workflow/`.
- Add runtime-specific command construction under `engine/`.
- Add persisted fields and platform paths under `config/`.
- Add host probes or reusable validation under `checks/`.

Tests should cover state transitions, idempotent outcomes, transaction failure,
and parity between command and TUI call sites. Cross-platform engine command
construction remains covered in `engine/`; real-hardware scenarios are listed
in `TESTING.md` and exercised by the nightly runner scripts when runners exist.
