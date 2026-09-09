# Instance data backup & restore (create-from-backup)

## Context

Omnideck already has one-directional volume export (`engine.ExportVolume`,
`podman volume export`), but it's wired only into `omnideck remove
--delete-volumes --backup` as a "just in case" safety net before permanent
deletion (`workflow/remove_instance.go`). There is no restore/import path at
all, no way to take a backup without deleting the instance, and no way to
seed a new instance's volumes from an existing archive.

The goal (from conversation): add real backup/restore, and use it to let a
new instance be created *from* a backup — enabling "ship a pre-built
instance" workflows (bundle a configured home/state into an archive,
hand it to someone, `omnideck add --from-backup` recreates it).

Scope decision, confirmed by conversation history: **Podman only.** Docker
support was deliberately removed by this repo's own author in commit
`2c71d7b` ("Unify setup on the shared Podman runtime"), replaced by the
`omnideck-runtime` owned-VM strategy in `engine/platform.go` specifically to
solve the "Podman is rough on macOS" problem without needing Docker. Every
commit since (42 of them) has invested further in that Podman-only path.
Reviving Docker is a separate, much larger effort than backup/restore and is
explicitly out of scope here. The good news: because `engine/command.go`'s
`buildCmd` already injects the right `--connection omnideck-runtime` flag
per-OS transparently, a new Podman command (`volume import`) needs **zero**
platform-specific code — it's "easy" precisely because that unification
already happened.

Restore scope is also narrowed deliberately: it seeds **new** instances only.
Restoring into an existing, already-running instance's live volumes (wipe +
reimport) is a materially riskier operation (must stop it, discard current
data, import, verify) and is left as future Maintenance-flow work, not part
of this change.

## Engine layer

**`engine/engine.go`** — add to the `Engine` interface, symmetric with the
existing `ExportVolume`:

```go
// ImportVolume replaces a named volume's contents from r, an uncompressed
// tar stream in the format `podman volume export` produces.
ImportVolume(name string, r io.Reader) error
```

**`engine/podman.go`** — implement it right next to `ExportVolume` (:90-97),
same shape, stdin instead of stdout:

```go
func (e *PodmanEngine) ImportVolume(name string, r io.Reader) error {
	cmd := buildCmd("podman", "volume", "import", name, "-")
	cmd.Stdin = r
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman volume import: %w", err)
	}
	return nil
}
```

Verify the exact `podman volume import` positional/stdin syntax against the
installed version during implementation (`podman volume import --help`);
adjust the `"-"` argument if needed. No other engine file changes — no OS
branching required, `buildCmd` already handles that.

## Workflow layer

### Share the existing backup logic instead of duplicating it

`workflow/remove_instance.go` already has `backupInstanceVolumes` (:161-230)
and `addRemovalBackupFile` (:232-252) — a working tar.gz-of-two-tars
implementation keyed by volume name. Move and export these into a new
**`workflow/backup_instance.go`**, as `BackupInstanceVolumes(eng, volumes
[]string, containerName string, opts BackupOptions) (path string, err
error)`. `remove_instance.go` keeps calling it (renamed). This is the reuse
CLAUDE.md asks for — one archive format, one writer, used by both removal's
safety-net backup and the new standalone backup feature.

Add one small, justified enhancement: write a third tar entry,
`manifest.json`, containing `{ containerName, image, memory, shmSize,
webUiPort, createdAt }` from the config at backup time. This is what makes
"ship a pre-built instance" actually useful — the recipient doesn't have to
guess flags to match the original. Restore must tolerate its **absence**
(older backups, or ones made before this change, have only `home.tar` +
`state.tar`) — manifest is enrichment, never a requirement.

Add a restore-side counterpart in the same file: `OpenBackupArchive(path
string) (manifest *BackupManifest, homeTar, stateTar string, cleanup func(),
err error)` — extracts `home.tar`/`state.tar` to a temp dir (mirroring the
existing temp-file approach in reverse) and parses `manifest.json` if
present. Reject archives missing either data entry with a
`workflow.ErrBackupInvalid` (new sentinel in `workflow/errors.go`, same
pattern as `ErrPortInUse`/`ErrContainerConflict`).

### Standalone backup (not tied to removal)

New `BackupInstance(eng, cfg *config.Config, opts BackupOptions) (path
string, err error)` in `backup_instance.go`. Unlike the removal path (which
always ends with the container gone), a standalone backup must not leave a
previously-running instance stopped:

1. `stopped, err := EnsureStopped(eng, cfg.ContainerName)` (existing helper,
   `workflow/container.go`) — volumes must be quiesced before export.
2. `BackupInstanceVolumes(...)`.
3. If `stopped` was true (i.e., it was actually running before), call
   `EnsureStarted(eng, cfg.ContainerName)` to restore prior state.

Needs only `ContainerStopEngine` + `ContainerStartEngine` + `ExportVolume`,
already-defined narrow interfaces.

### Restore = seed a new instance's volumes, reusing `CreateInstance`

Rather than a parallel `RestoreInstance` that reimplements
`CreateInstance`'s check/rollback machinery, extend
**`workflow/create_instance.go`**:

- `InstanceCreationEngine` gains `ImportVolume(name string, r io.Reader)
  error`.
- `CreateInstanceOptions` gains `RestoreArchive string` (path to a backup
  produced by `BackupInstanceVolumes`).
- In `CreateInstance`, right after each `eng.CreateVolume(...)` call
  succeeds (:120-133 today), if `opts.RestoreArchive != ""`:
  - Require `created == true` for both volumes — if either volume already
    existed, fail with `ErrBackupInvalid` ("refusing to restore into
    existing storage; choose a different name"). Restore only ever
    populates brand-new volumes, matching the narrowed scope above.
  - Open the archive once (before the home-volume stage) via
    `OpenBackupArchive`, keep the two temp file paths, defer cleanup.
  - Call `eng.ImportVolume(cfg.HomeVolumeName(), homeFileReader)` /
    same for state, instead of leaving the volume empty.
  - Stage detail becomes `"restored from backup"` instead of `"created"`
    (extend `creationDetail`).
- If a manifest was present and the caller didn't explicitly override
  `cfg.Image`, prefill `cfg.Image` from it before the existing
  `check_availability`/pull stages run — CLI/TUI callers still decide the
  final config, this only changes the default.

This keeps exactly one creation/rollback code path (the existing
defer-based volume/container cleanup in `CreateInstance` already covers a
failed restore the same as a failed fresh create) and only touches file
`create_instance.go` plus the two mock implementations of
`InstanceCreationEngine` used in tests.

## CLI layer

**New `cmd/backup.go`** (mirrors `cmd/remove.go`'s structure/flags):

```
omnideck backup NAME [--output DIR] [--plain] [--json]
```

Resolves the instance the same way `remove.go` does
(`savedInstanceNamed`), requires a ready engine
(`detectReadyEngine`), calls `workflow.BackupInstance`, prints/JSON-emits the
resulting archive path. Add `ErrCodeBackupInvalid` to `cmd/jsonout.go`'s
error-code block (:28-46) for the JSON path.

**Extend `cmd/setup.go` (`add`)**:

- New flag: `setupCmd.Flags().StringVar(&setupFromBackupFlag, "from-backup",
  "", "create this instance's storage from a backup file produced by
  'omnideck backup'")`.
- `resolveSetupConfig` (or its caller) validates the file exists and is a
  well-formed archive early — same "local, deterministic failure before
  requiring an engine" discipline already used for other flag validation in
  this file and in `remove.go`'s `resolveRemoveOptions`.
- If a manifest is found, prefill `cfg.Image` (and only `cfg.Image` —
  memory/port/name stay driven by flags/prompts as today) unless
  `--image`/`--port`/etc. were explicitly passed.
- Thread `RestoreArchive: setupFromBackupFlag` into the
  `workflow.CreateInstanceOptions{}` built in `runSetupPlain` (:220-236) and
  `runSetupStepsJSON`, so `--plain`, `--json`, and the interactive TUI path
  (see below) all go through the identical `CreateInstance` behavior — the
  same guarantee the doc comment on `CreateInstance` already claims for
  plain/json today.

## TUI layer

### Standalone "Backup" action on an existing instance

New `Route RouteBackup` (`tui/router.go`), new `tui/backup.go` +
`tui/screen_backup_view.go`, modeled directly on `tui/removal.go`'s shape
(`RemovalModel`/`RemovalRequest`/`RemovalStage{Review,Applying,Complete,
Failed}`, reusing `SpinnerModel` the same way Setup/Maintenance/Removal do —
this is the pattern CLAUDE.md calls out explicitly). `BackupRequest{Instance
config.InstanceInfo, Engine engine.Engine, Embedded bool}`; steps are
Stop (if running) → Export home → Export state → Restart (if it was
running); on completion, show the archive path (same `styles.Active.Render`
treatment `cmd/remove.go` uses for `BackupPath`).

Wire-up in `tui/screen_workflow.go`, mirroring `startEmbeddedRemoval`
(:63-79) exactly:

```go
func (m AppModel) startEmbeddedBackup() (AppModel, tea.Cmd) {
	inst := m.CurrentInstance()
	if inst == nil || inst.Info.Config == nil || m.eng == nil {
		return m, nil
	}
	bm := NewBackupModel(BackupRequest{Instance: inst.Info, Engine: m.eng, Embedded: true})
	bm.WindowWidth, bm.WindowHeight = m.width, m.height
	m.backupModel = bm
	m.router.Push(RouteBackup)
	return m, bm.Init()
}
```

Surface it the same way the other four dashboard actions are surfaced
(`tui/screen_dashboard_update.go`): a 6th action chip ("Backup") alongside
Open UI/Logs/Update/Stop-Start/Remove (:180-199 `execChip`), plus a `"b"`
keyboard shortcut next to the existing `s`/`l`/`c`/`n`/`u`/`x`/`d` handlers
(:105-140).

### "Create from backup" during Setup

`SetupModel`'s existing stage machine (`tui/model.go`:
Welcome→QuickCheck→Runtime→**Settings**→Review→Applying→Complete/Failed) is
kept as-is — this is additive within the Settings stage, not a new journey
sharing state with another one (respecting the "don't add a shared phase
enum" rule, since this is one field within one existing journey's own
stage).

In `tui/setup_settings.go`, add one more input at the top of the Settings
screen for `SetupFirstRun`/`SetupAdditionalInstance` modes: a toggle
("Start fresh" / "Restore from a backup file") and, when the restore option
is chosen, a path text input. On advancing past Settings
(`validateAllInputs`, :162), validate the path via the same
`OpenBackupArchive` used by the CLI, surfacing a field error the same way
existing settings validation does (:105-198) rather than failing later at
apply time.

`buildConfig` (`tui/setup_apply.go`:374) prefills `Image` from the
manifest when present, same rule as the CLI path. `startSetupWorkflow`
(:189) passes `RestoreArchive` into the `workflow.CreateInstanceOptions` it
builds, so the TUI apply spinner steps go through the exact same
`workflow.CreateInstance` restore branch as `add --from-backup` — same
stage labels (`creationDetail` returning `"restored from backup"`), same
rollback-on-failure behavior, no separate code path to keep in sync.

## Files touched (summary)

- `engine/engine.go`, `engine/podman.go` — `ImportVolume`
- `workflow/errors.go` — `ErrBackupInvalid`
- `workflow/backup_instance.go` (new) — moved/exported archive read+write
  helpers, manifest, `BackupInstance`
- `workflow/remove_instance.go` — call the now-exported helper instead of
  its private copy
- `workflow/create_instance.go` — `RestoreArchive` option + import branch
- `cmd/backup.go` (new), `cmd/setup.go`, `cmd/jsonout.go` — CLI surface
- `tui/router.go`, `tui/backup.go` (new), `tui/screen_backup_view.go` (new),
  `tui/screen_workflow.go`, `tui/screen_dashboard_update.go`,
  `tui/setup_settings.go`, `tui/setup_apply.go` — TUI surface
- Test-fixture mocks implementing `InstanceCreationEngine` /
  `InstanceRemovalEngine` (wherever `_test.go` files define them) need the
  new `ImportVolume` method added to satisfy the interface.

## Verification

- `go build ./... && go vet ./... && go test ./...` — the mock-interface
  updates above are required just to keep the build/tests compiling.
- Manual, with real Podman: `omnideck add` a throwaway instance, write a
  marker file into it, `omnideck backup <name>`, `omnideck remove
  <name> --delete-volumes --no-backup --yes`, then `omnideck add --name
  restored --from-backup <archive>` and confirm the marker file is present
  and the image matches the manifest.
- Same round trip through the TUI: dashboard → Backup chip → confirm archive
  path printed; dashboard → "n" (add) → choose restore → confirm the new
  instance boots with the restored data.
- `--json` variants of both `backup` and `add --from-backup`, checked
  against whatever event/error shape `docs/JSON_MODE_SPEC.md` (currently
  untracked in this repo) already documents for `add`, so the new flag's
  NDJSON stays consistent with it.
