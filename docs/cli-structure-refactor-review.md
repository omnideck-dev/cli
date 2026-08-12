# CLI setup, control-plane, Doctor, and platform refactor review

Baseline: `origin/main` at `a32d3ad` (`v0.11.0-beta.2`).

## Implementation status in this branch

The safety-critical and cross-surface portions of this review are implemented:

- volume creation now reports ownership, and failed setup removes only resources
  created by that transaction;
- TUI, plain, and JSON setup share `workflow.CreateInstance`, typed lifecycle
  events, validation, cross-field memory policy, and rollback;
- saved configuration is committed with private same-directory temporary files,
  flushes, and atomic replacement;
- unreadable instance files remain in inventory and become stable-ID Doctor
  findings instead of disappearing;
- repair is suppressed and rejected when either expected saved volume is missing
  or cannot be verified;
- control-plane results carry stable instance identity and request generations,
  retain selection across refresh, and prevent overlapping periodic poll rounds;
- the engine no longer imports the command package, common Podman mutations use
  one bounded command-error path, and installer guidance comes from the verified
  installer catalog; and
- unsupported architectures and immutable Linux variants use explicit manual
  guidance instead of guessed installers or host package mutation.

The larger `PodmanClient`/`CommandRunner`, batched snapshot API, fully concurrent
Doctor collector, and route-as-child-model redesign remain follow-up architecture
work. They require broader public-interface changes and should build on the safety
and lifecycle boundaries established here.

## Executive summary

The intended dependency direction is sound:

```text
cmd / tui -> workflow -> engine -> Podman CLI
                    \-> config / checks
```

The implementation does not consistently preserve that direction yet. Interactive
setup still owns a container-creation transaction, the control plane identifies
asynchronous results by slice index, Doctor mixes fact collection, diagnosis,
repair planning, and user-facing copy, and host policy is split across probes,
setup plans, installers, runtime execution, and call sites that still read
`runtime.GOOS` directly.

The recommended sequence is safety first, then consolidation:

1. Correct volume ownership and repair preconditions.
2. Make config inventory and persistence reliable.
3. Put every instance mutation behind one application service.
4. Replace per-screen Podman polling with stable, batched snapshots.
5. Split Doctor into collection, evaluation, action planning, and presentation.
6. Make platform support an explicit capability model owned by the Podman layer.
7. Decompose the TUI only after the underlying services and messages are stable.

## Highest-priority correctness findings

### 1. Failed setup can remove a pre-existing data volume

`PodmanEngine.CreateVolume` is intentionally idempotent: it returns success when
the named volume already exists. `workflow.CreateInstance` nevertheless marks a
volume as newly created after any successful `CreateVolume` call, then removes it
on a later failure. Interactive setup has the same ownership assumption, encoded
as `lastCompletedStep`, and directly removes both volume names during rollback.

This matters because removal explicitly tells users that retained data can be
reconnected by setting up an instance with the same name. A later image-pull,
container-start, or config-save failure can instead delete those retained volumes.

Recommended correction:

- Stop using successful `CreateVolume` as proof of ownership.
- Inspect each volume before creation and record only volumes absent at the start.
- Clean up only resources proven to have been created by the current operation.
- Prefer one create/reconcile transaction for TUI, plain, JSON, and Desktop.
  `EnsureInstance` already has the safer volume-accounting shape and can become
  that transaction after its repair policy is made explicit.
- Add failure tests with pre-existing home only, state only, and both volumes.

### 2. Doctor can offer a repair that contradicts its saved-data warning

Doctor creates the missing-container repair action before it checks the two named
volumes. Missing volumes are reported later with the instruction not to create a
replacement, but the repair action remains selectable. Repair ultimately runs a
container with those volume names; Podman can create absent named volumes as part
of that run.

Recommended correction:

- Collect one immutable diagnostic snapshot first.
- Evaluate findings and available actions from the complete snapshot.
- Offer automatic container repair only when required saved volumes are present,
  or when the instance is known to be a genuinely new empty environment.
- Model missing storage as a distinct recovery state with no automatic mutation.
- Add a workflow-level repair precondition so non-TUI callers cannot bypass it.

### 3. Unreadable instance files disappear from inventory and Doctor

`config.ListInstances` silently skips YAML files that fail to load. A corrupt or
partially written instance can therefore look like no installation at all. It is
absent from the dashboard, Doctor cannot explain the problem, and setup default
selection does not reserve its name or port.

Recommended correction:

- Return an inventory containing both loaded instances and per-file load issues.
- Let bare-command routing distinguish `none`, `healthy`, and `present but broken`.
- Show broken entries in Doctor/control-plane UI without enabling unsafe actions.
- Keep list-level I/O failure separate from entry-level parse/validation failure.

### 4. Config writes do not provide the transaction boundary the workflows claim

Normal config and settings saves use a direct `os.WriteFile`. A crash, short write,
or disk-full error can leave a truncated file. Rolling the container back after a
save error does not restore the previous YAML contents if that file was already
partially overwritten.

Recommended correction:

- Write a private sibling temporary file, flush it, close it, and atomically rename
  it over the destination; sync the containing directory where supported.
- Serialize mutations per instance with a lock spanning runtime reconciliation and
  config commit.
- Use the same persistence primitive for instance files and shared settings.

## Setup and lifecycle consolidation

There are currently three overlapping lifecycle abstractions:

- TUI setup's hand-written seven-step create/rollback sequence.
- `workflow.CreateInstance`, used by plain and JSON setup.
- `workflow.EnsureInstance`, used by the Desktop/automation environment contract.

Update and settings also compose `PullImage` plus `RecreateAndSave` separately.
That produces drift in validation, progress stages, cancellation, cleanup, and
error classification.

Create one `InstanceService` (the existing `workflow` package can host it) with
explicit operations such as:

```go
PlanCreate(request) (Plan, error)
Create(ctx, plan, observer) (Result, error)
Reconcile(ctx, current, desired, observer) (Result, error)
Repair(ctx, snapshot, observer) (Result, error)
Remove(ctx, request, observer) (Result, error)
```

The plan should contain validated desired config, resource ownership decisions,
preconditions, and typed stages. The observer should receive typed events rather
than each caller translating string stage names. TUI, plain text, NDJSON, and the
Desktop contract should be presenters over the same operation.

Validation and defaults also need one entry point. Today:

- TUI setup uses host-derived memory defaults and enforces SHM <= memory.
- Plain/JSON setup uses static config defaults and does not enforce that relation.
- `environment ensure` has another validator.
- Settings validates individual fields and adds cross-field checks in the TUI.

A canonical `ResolveDesiredConfig` plus `ValidateDesiredConfig` should accept the
host resource policy, existing inventory, and caller overrides, then return field-
addressable typed violations. User-facing surfaces can phrase those violations
without reimplementing the rules.

## Control-plane model and Podman polling

The dashboard starts a poll for every instance every second. Each active-instance
poll can invoke Podman for status, stats, and inspect; the next tick starts without
tracking whether the previous poll finished. This scales as multiple processes per
instance per second and can overlap indefinitely when a machine or connection is
slow.

Results carry only a slice index. Instance refresh can remove or reorder entries
while stats or log requests are in flight, allowing a stale result for one
container to be applied to another entry at the same index. Selection is also an
index, so refreshing inventory can silently change the selected instance.

Recommended correction:

- Introduce a stable `InstanceID` (config path or persisted ID; container name is a
  workable transitional key).
- Carry identity and a request generation in every asynchronous result.
- Store selection and expanded state by identity, not index.
- Allow only one control-plane refresh in flight; schedule the next refresh after
  completion or explicitly drop stale generations.
- Add a Podman snapshot API that batches `ps`, `stats`, and multi-container
  `inspect` where supported, returning partial per-instance errors.
- Poll dashboard facts only while the dashboard is visible. Fetch logs on demand.
- Reuse the same snapshot in `list`, `status`, bare routing, and Doctor instead of
  maintaining parallel collectors and duplicate uptime formatting.

## Doctor architecture

`workflow.DiagnoseWithProbes` currently performs synchronous I/O, emits final
English copy, chooses actions, and returns an engine selected from probe results.
The TUI then switches on action enums to perform some actions itself, while the
plain renderer translates those enums back into commands. `doctor --json` exposes
labels rather than stable check identifiers. The command path also probes twice:
once through `detectReadyEngine`, then again through `workflow.Diagnose`.

Split Doctor into four layers:

1. **Collector**: acquire runtime, host, instance, volume, image, port, and optional
   Ollama facts concurrently with bounded timeouts and partial errors.
2. **Evaluator**: pure functions turn facts into stable findings such as
   `runtime.not_ready`, `instance.container_missing`, or `storage.home_missing`.
3. **Action planner/executor**: derive safe actions from the complete finding set
   and execute shared workflow operations, never raw engine calls from the TUI.
4. **Presenters**: TUI, plain text, and JSON render the same findings. JSON should
   include a stable check ID and schema version; labels remain display copy.

This also makes it practical to stream completed checks to the Doctor screen while
preserving deterministic report order.

## TUI structure

`InstallationSection` and `ControlPlaneSection` document the intended boundary,
but embedding makes most state and methods part of one large `AppModel` namespace.
`Router.Section()` does not drive dispatch. `AppModel.Update`, the shared footer,
and the shared view know the detailed stage enums of Setup, Maintenance, Removal,
Settings, and Doctor.

After the services above are stable:

- Make each route a real child model with its own `Init`, `Update`, `View`, busy
  state, and key help.
- Let the app shell own only window size, navigation, modal coordination, shared
  inventory cache, and application-level commands.
- Use typed child-to-parent messages such as `Navigate`, `InventoryInvalidated`,
  `OperationFinished`, and `QuitRequested`.
- Give Installation and Control Plane actual coordinator models if that distinction
  remains useful; otherwise remove the ceremonial section type.
- Replace direct child field inspection in headers/footers with a small route
  contract (`Title`, `Breadcrumb`, `Help`, `Busy`).

This decomposition should follow the workflow consolidation, not precede it;
otherwise the same duplicated business logic is merely moved into smaller files.

## Podman adapter cleanup

The adapter is nominally one `Engine`, but behavior is still driven by global
process state: `runtime.GOOS`, a global cancellation context, global test seams,
and process-wide PATH mutation. Podman methods also mix `Output`,
`CombinedOutput`, and the shared command helpers, producing inconsistent diagnostic
detail and swallowed errors (`Version` and `ImageDigest` return sentinel strings).
`engine` also imports `cmd/debug`, reversing the desired dependency direction.

Recommended target:

```go
type PodmanClient struct {
    Host   HostProfile
    Runner CommandRunner
}

type CommandRunner interface {
    Run(ctx context.Context, request CommandRequest) CommandResult
    Stream(ctx context.Context, request CommandRequest, observer LineObserver) CommandResult
}
```

- Bind OS/architecture, connection policy, environment, binary path, debug sink,
  and cancellation when constructing the client.
- Pass contexts per operation; remove `SetCancelContext` / `SuspendCancellation`
  global state by creating a bounded cleanup context explicitly.
- Make all command failures carry action, exit code, bounded stdout/stderr, and a
  typed reason when known.
- Return `(value, error)` for version and digest queries.
- Keep narrow workflow-facing interfaces, but derive them from one concrete client
  instead of growing a broad interface that every mock must implement.

## OS and distribution targeting

Host targeting needs to become an explicit support/capability decision rather than
a collection of string switches.

### Current structural gaps

- Unknown operating systems fall through to Linux-like no-machine behavior in
  `podmanPolicy`, even though release targets are explicitly Linux, macOS, and
  Windows.
- Installer selection is duplicated between `missingPlan` and the verified
  `podmanInstallers` catalog. One path defaults unknown Windows architectures to
  AMD64 and unknown macOS architectures to ARM64; the actual downloader rejects an
  unknown tuple.
- `RuntimeUnsupportedVersion` and version parsing exist, but
  `applyVersionPolicy` is empty, so the state cannot currently be produced after a
  successful `podman info`.
- Linux distribution identity is treated as package-manager capability. This
  misses immutable/atomic variants and assumes `dnf` for every listed RPM-family
  system. `ID_LIKE` helps derivatives but does not prove that the chosen manager or
  mutation model is available.
- `WSL`, `Systemd`, and CPU/disk resource facts are collected or exposed but do not
  affect current policy, obscuring which facts are authoritative.
- Setup-plan commands shown to users always wrap Linux package commands in `sudo`,
  while the actual installer independently chooses root, `pkexec`, or `sudo`.

### Recommended host model

Resolve a `HostProfile` once and pass it through every layer:

```go
type HostProfile struct {
    Target          Target       // supported OS + arch or explicit unsupported
    Linux           *LinuxHost   // parsed os-release and environment facts
    RuntimePolicy   PodmanPolicy
    ResourcePolicy  ResourceDefaults
}

type LinuxHost struct {
    ID, Version, Variant string
    Like                 []string
    PackageManager       PackageManager
    MutationMode         MutationMode // mutable, rpm-ostree, transactional, unknown
    Elevation            ElevationCapability
}
```

- Parse `/etc/os-release` with a dedicated parser and preserve parse/read errors.
- Detect package-manager executables and immutable-host markers as capabilities;
  use distro family only to rank safe candidates and enforce version support.
- Treat Silverblue/Kinoite/rpm-ostree and other immutable systems explicitly.
  If Podman is already present, continue normally; if absent, give the platform's
  supported host-mutation guidance rather than running `dnf install`.
- Keep one reviewed installer catalog as the source for URL, version, filename,
  checksum, signature policy, and supported target tuple. Generate setup copy from
  that catalog.
- Either implement and test minimum Podman versions by target or delete the
  unreachable unsupported-version state until a real policy exists.
- Reject unsupported OS/architecture tuples before probing or constructing commands.

Recommended Linux tests should cover at least Ubuntu, Debian, Mint/Pop,
Fedora/RHEL-family mutable hosts, Fedora atomic variants, Arch-family, openSUSE,
Alpine, an `ID_LIKE` derivative, a recognized distro without the expected package
manager, root, PolicyKit, terminal sudo, and no available elevation path.

## Suggested package boundary

This can be reached incrementally without a repository-wide rewrite:

```text
cmd/                 argument parsing and text/JSON presenters
tui/                 app shell and route models
workflow/
  instance_service   create/reconcile/repair/update/remove transactions
  inventory          saved + runtime instance snapshots
  diagnostics        pure findings and safe action planning
engine/
  podman_client      container/image/volume operations
  command_runner     context, environment, output, and debug policy
  runtime_service    Podman install/machine reconciliation
platform/
  host_profile       OS/arch support and Linux capability detection
config/
  repository         inventory, atomic save, locking, and migration
checks/               pure value validation and host probes
```

Moving `HostPlatform` out of the broad engine setup file is optional; the important
part is that one resolved profile is injected rather than rediscovered.

## Delivery plan

### Phase 0: safety fixes

- Preserve pre-existing volumes on every failed create path.
- Gate repair on storage preconditions.
- Add atomic config/settings writes and expose corrupt inventory entries.
- Add regression tests before structural movement.

### Phase 1: one instance lifecycle service

- Fold `CreateInstance`, TUI setup steps, `EnsureInstance`, update, repair, and
  settings recreation onto one typed operation/event model.
- Centralize config resolution and cross-field validation.
- Migrate one caller at a time; remove old paths only after parity tests pass.

### Phase 2: inventory snapshots and Doctor

- Introduce stable instance identity and batched Podman snapshots.
- Fix in-flight control-plane refresh semantics.
- Build Doctor collector/evaluator/action planner over the snapshot.
- Add stable Doctor JSON check IDs and a schema migration plan.

### Phase 3: platform and Podman construction

- Introduce `HostProfile`, `CommandRunner`, and an injected `PodmanClient`.
- Consolidate installer metadata and runtime reconciliation plans.
- Add immutable Linux and unsupported-target behavior.
- Remove global platform, cancellation, PATH, and debug dependencies.

### Phase 4: TUI decomposition

- Convert routes into child models and shrink `AppModel` to coordination.
- Make Installation/Control Plane real boundaries or remove them.
- Keep screen snapshot/navigation tests while replacing internal state coupling.

## Verification expectations

Each phase should keep `make verify` and `make race` green, add transaction failure
tests, and run the VM E2E lanes appropriate to changed platform behavior. Runtime
installer, elevation, Podman machine, and immutable-distro changes require the
hardware/manual evidence described in `TESTING.md`; unit tests alone cannot validate
those host mutations.
