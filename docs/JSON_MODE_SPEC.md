# Omnideck CLI — JSON Mode Reference

This is the contract a desktop (or any other GUI) app builds against when it
drives `omnideck` as a subprocess instead of a person typing at a terminal.
It documents every `--json` output shape, the closed error-code set, the
NDJSON streaming convention, and the process-lifecycle rules that make this
safe to automate. Machine-readable schemas live under
[`contracts/`](../contracts/README.md).

Source of truth is `cmd/jsonout.go` plus the `--json` branch of each command
file in `cmd/`. If this document and the code disagree, the code wins — but
treat that as a bug in this file, since a GUI built against stale docs will
misparse real output.

---

## 1. The `--json` contract, in one page

- `--json` is a **persistent flag** and works on any subcommand:
  `omnideck status --json`, `omnideck --json list`, and so on.
- Under `--json` a command **never** prompts, opens a picker, or launches the
  TUI, regardless of whether stdin is a terminal. A decision that would need a
  prompt fails with a structured error instead (for example
  `AMBIGUOUS_INSTANCE`, which lists the instance names so you can retry with
  `--name`).
- Output is one of two shapes:
  - **Single value**: one JSON object or array, newline-terminated. Used by
    `--version`, `status`, `list`, `config`, `doctor`, `start`, `stop`,
    `restart`, `wait`, `runtime status`, `logs` without `--follow`, and
    `runtime ensure` when Podman is already ready.
  - **NDJSON stream**: one flushed JSON object per line, for multi-step work:
    `add`, `update`, `repair`, `remove`, `environment ensure`,
    `runtime ensure`, and `logs --follow`.
- **Errors are always structured** (see [§3](#3-structured-error-shape)) and
  every error resolves to one of the closed codes.
- **stdout is exclusively machine-readable.** Structured failures are written
  to stdout without Cobra appending an `Error:` line or usage text to stderr.
- **stderr** may carry advisory warnings (for example "config file … is
  unreadable") printed while loading settings. They are not part of the
  contract; log or ignore them.
- **Exit codes**: `0` on success, `1` on any failure *and* when the observed
  state is not the affirmative one (`status` on a stopped container, `doctor`
  with a failed check). Always parse the body. See [§8](#8-process-lifecycle-rules-for-a-gui-wrapper).
- **Consumers must ignore unknown fields and unknown stages.** Additive
  optional fields and new stages do not change the contract version.
- **`jsonContract`** (currently `5`) is returned by `omnideck --version
  --json`. It is bumped only for an incompatible change: a removal, rename,
  type change, enum change, or semantic change. Check it once at startup.
  Contract 5 added the `INVALID_SETTING` and `MISSING_STORAGE` codes, the
  error `setting` field, the `update_instance` action, and `repair --json`;
  `CONTAINER_NOT_FOUND` is now emitted, and status `name` is the instance
  name. Contract 4 added the migration/startup error codes and made `start`,
  `restart`, and `environment ensure` success mean the application answers
  over HTTP. Earlier schemas remain under `contracts/json/v<n>`.

---

## 2. Global flags relevant to a GUI wrapper

| Flag | Effect |
|---|---|
| `--json` | Machine-readable output for this invocation. |
| `--name`, `-n` | Select an instance by its **instance name** (the saved file's name, as shown by `list --json`'s `name`). Required whenever more than one instance exists. |
| `--config` | Explicit path to a config file, bypassing name/instance-directory resolution. |
| `--no-color` | Irrelevant under `--json` (no ANSI is emitted) but harmless. |
| `--debug` | Prints raw Podman command lines to stderr. Useful while developing an integration. |

An instance name usually matches its container name, but not always (for
example an instance named `desktop` whose container is `omnideck-desktop`).
Always use the instance name for `--name`.

Commands that act on one instance (`start`, `stop`, `restart`, `status`,
`wait`, `update`, `logs`, `config show`, `config set`) resolve it the same way:

1. `--config <path>` if given.
2. Else `--name <name>` → that instance; an unknown name is `NOT_INSTALLED`.
3. Else, if exactly one instance is saved, that one.
4. Else, if none is saved, the legacy single-instance file if present,
   otherwise `NOT_INSTALLED`.
5. Else (several instances, no `--name`) → `AMBIGUOUS_INSTANCE` with an
   `instances` array.

A GUI managing more than one instance should always pass `--name`.

---

## 3. Structured error shape

Every non-streaming error writes exactly this envelope to stdout and exits `1`:

```json
{
  "error": {
    "code": "AMBIGUOUS_INSTANCE",
    "message": "Multiple instances found — specify --name",
    "hint": "optional guidance",
    "detail": "optional safe technical detail",
    "action": "runtime_setup",
    "actionValue": "podman",
    "instances": ["omnideck", "staging"],
    "setting": "web_ui_port"
  }
}
```

`code` and `message` are always present. `hint`, `detail`, `action`,
`actionValue`, `instances`, and `setting` appear only when relevant (omitted,
never `null`). Streaming commands carry the same object as the `error` field of
their terminal event (see [§5](#5-ndjson-streaming-commands)).

### Closed error codes

The set is defined in `cmd/jsonout.go` and pinned by
`TestClosedErrorCodeSetIsStable` in `cmd/jsonout_test.go`. The CLI never emits
a code outside this list; adding one is a contract change.

| Code | Meaning |
|---|---|
| `AMBIGUOUS_INSTANCE` | Several instances exist and no `--name` was given. `instances` lists the valid names. |
| `NOT_INSTALLED` | No instance matches: none saved, an unknown `--name`, or an unreadable `--config` path. |
| `ENGINE_NOT_FOUND` | Podman is not installed or not ready. `environment ensure` adds `action: runtime_setup`. |
| `CONTAINER_NOT_FOUND` | `start` or `restart` found the instance's settings but not its container. Adds `action: repair_instance` when both saved data volumes still exist. |
| `MISSING_REQUIRED_FLAG` | A required non-interactive flag was omitted or contradictory (`remove` flags, `environment ensure --name`). |
| `INVALID_SETTING` | A setting or flag value was rejected before anything changed. `setting` names it with the saved-settings key (`container_name`, `web_ui_port`, `memory`, `shm_size`, `image`, `home_volume`, `state_volume`, or an unknown key as given), or `backup_dir` for `remove --backup-dir`. Used by `config set`, `add`, and `environment ensure`. |
| `MISSING_STORAGE` | A saved data volume is missing, so `repair` or `environment ensure` stopped rather than create an empty one. Nothing was changed. |
| `MISSING_SUBCOMMAND` | Bare `omnideck --json`. `--json` never falls through to help text or the TUI. |
| `CANCELLED` | Interrupted by SIGINT/SIGTERM mid-flight. |
| `RESTART_REQUIRED` | Runtime setup finished a step that needs the computer to restart. |
| `PERMISSION_DENIED` | Runtime setup needed administrator approval and it was refused or failed. |
| `PERMISSION_CANCELLED` | The person dismissed the administrator prompt. |
| `WINDOWS_FEATURES_FAILED` | Enabling the Windows features Podman needs failed. |
| `PACKAGE_INDEX_FAILED` | The Linux package manager could not refresh its package lists. |
| `INSTALLER_FAILED` | The Podman installer ran and failed. |
| `DOWNLOAD_FAILED` | A download failed: the Podman installer, or the application image during `add`, `update`, `repair`, or `environment ensure`. |
| `UNSUPPORTED` | This computer cannot run Omnideck's runtime setup. |
| `RUNTIME_SETUP_FAILED` | Any other runtime setup failure. |
| `PORT_IN_USE` | The requested local port is taken. |
| `CONTAINER_CONFLICT` | An unrelated container, or another saved instance, already uses the requested name, or another saved instance already uses one of its data volumes. The hint says not to remove the container when it belongs to another saved instance. |
| `MIGRATION_FAILED` | The application's migration barrier stopped startup. `detail` names the failed migration; it never contains saved records, credentials, or raw logs. |
| `APP_STARTUP_FAILED` | The application exited, or did not answer before the deadline, without a recognized migration failure. |
| `INTERNAL_ERROR` | Anything not classified above. |

Neither startup failure deletes data or retries a migration automatically.

`action`/`actionValue` use Doctor's vocabulary ([§7](#7-doctor-action-vocabulary-guided-repair))
so a GUI can offer a "fix it" button straight from an error.

`status` and `list` report a missing container as `"status": "unknown"`
rather than an error, and `stop` on a missing container is a successful no-op.

---

## 4. Command-by-command JSON reference

### `omnideck --version --json`

Single value, no instance required.

```json
{"version": "0.11.0", "commit": "a1b2c3d", "date": "2026-11-01", "jsonContract": 5}
```

### `omnideck status --json`

Requires an instance ([§2](#2-global-flags-relevant-to-a-gui-wrapper)).
Gathers container status, Ollama reachability, and volume existence
concurrently.

```json
{
  "name": "omnideck",
  "container": "omnideck",
  "status": "running",
  "image": "ghcr.io/omnideck-dev/omnideck:latest",
  "engine": "podman",
  "webUiPort": "2337",
  "homeVolume": {"name": "omnideck-home", "exists": true},
  "stateVolume": {"name": "omnideck-state", "exists": true},
  "ollama": {"reachable": false, "host": "127.0.0.1:11434"}
}
```

`status` is Podman's own status string (`"running"`, `"exited"`,
`"paused"`, …) or `"unknown"` if the container could not be inspected. The
exit code is `1` whenever `status != "running"`, with the body still emitted.

`name` is the **instance name** (what `--name` accepts, same as
`list --json`); `container` is the container name. They usually match.
Earlier CLIs repeated the container name in `name`.

This shape is reused by `start`, `stop`, `restart`, and `wait` (gathered after
the action), by the `complete` result of `add` and `update`, and inside the
`environment ensure` result.

### `omnideck list --json`

No instance resolution. Always an array; `[]` when nothing is installed.

```json
[
  {
    "name": "omnideck",
    "container": "omnideck",
    "status": "running",
    "image": "ghcr.io/omnideck-dev/omnideck:latest",
    "webUiPort": "2337",
    "cpu": "0.11%",
    "cpuPct": 0.0011,
    "ram": "195.4MB",
    "ramTotal": "1.074GB",
    "ramPct": 0.18,
    "uptime": "1h 30m",
    "restarts": 2,
    "health": "healthy",
    "created": "2026-07-20"
  },
  {
    "name": "staging",
    "container": "staging",
    "status": "exited",
    "image": "ghcr.io/omnideck-dev/omnideck:latest",
    "webUiPort": "2338",
    "cpu": null,
    "cpuPct": null,
    "ram": null,
    "ramTotal": null,
    "ramPct": null,
    "uptime": null,
    "restarts": null,
    "health": "",
    "created": "2026-06-01"
  }
]
```

**Every entry has the same set of keys, always** (pinned by
`TestGatherListEntryStoppedHasNullLiveFields`). `cpu`, `cpuPct`, `ram`,
`ramTotal`, `ramPct`, `uptime`, and `restarts` are explicit `null` — never
omitted and never a misleading zero — unless the container is active
(`running`, `paused`, or `restarting`). `health` and `created` are always
strings; `health` is `""` without a healthcheck. If Podman is not ready,
entries report saved settings with `status: "unknown"` and null live fields.

### `omnideck config show --json` / `config set <key> <value> --json`

Both emit the same shape; `config set` re-emits it after a successful write.

```json
{
  "containerName": "omnideck",
  "homeVolume": "omnideck-home",
  "stateVolume": "omnideck-state",
  "memory": "2g",
  "shmSize": "1024m",
  "webUiPort": "2337",
  "runtime": "podman",
  "image": "ghcr.io/omnideck-dev/omnideck:latest",
  "installedAt": "2026-07-01T12:00:00Z"
}
```

Editable keys: `home_volume`, `state_volume`, `memory`, `shm_size`,
`web_ui_port`, `image`. `container_name` is rejected (a name cannot change
after setup). Any rejected key or value is `INVALID_SETTING` with `setting`
set to the key. Changing `memory` or `shm_size` is also rejected when shared
memory would end up larger than memory; `setting` is the key being changed
(`memory` or `shm_size`).

Validation a GUI form can mirror client-side (the CLI still enforces it):

- `home_volume` / `state_volume`: start with a letter or number; then only
  letters, numbers, dots, underscores, and hyphens.
- `memory` / `shm_size`: a positive number plus a unit, e.g. `2g`, `512m`.
- `web_ui_port`: a number from 1 to 65535.

`config set` only saves; it does not change the running container. Run
`update` to apply saved settings, and show your own "apply" affordance — the
plain-text reminder is not printed under `--json`.

### `omnideck config path --json`

```json
{"path": "/home/user/.config/omnideck-cli/instances/omnideck.yaml"}
```

### `omnideck doctor --json`

Instance resolution is forced only when one is implied (saved instances, or
an explicit `--name`/`--config`). With no instances, Doctor still runs the
checks that do not need one.

```json
{
  "checks": [
    {"id": "runtime.podman", "label": "Container runtime", "status": "pass", "detail": "Podman is ready · 5.8.4", "hint": ""},
    {
      "id": "instance.container",
      "label": "Omnideck instance",
      "status": "fail",
      "detail": "work is stopped",
      "hint": "Run: omnideck start --name work",
      "action": "start_instance",
      "actionLabel": "Start Omnideck",
      "actionValue": "work"
    },
    {
      "id": "instance.external_url",
      "label": "MCP sign-in address",
      "status": "warn",
      "detail": "MCP integrations are unavailable: this installation was created without its sign-in address",
      "hint": "Run: omnideck update --name work (saved data is kept)"
    }
  ],
  "allPass": false
}
```

`status` is `"pass"`, `"fail"`, `"warn"`, or `"info"`. **Only `"fail"`
affects `allPass`**; the exit code is `1` exactly when `allPass` is `false`.
Ollama is optional and is never a failure.

`id` is a stable identifier; match on it, not on `label` or `detail`:

| `id` | Check |
|---|---|
| `runtime.podman` | Podman readiness |
| `instance.setup` | No instance set up yet |
| `instance.container` | Container exists and is running |
| `instance.browser` | Web UI answers on its port (only while running) |
| `instance.external_url` | Container's `OMNIDECK_EXTERNAL_URL` matches the browser address (whenever the container exists) |
| `storage.home`, `storage.state` | Saved data volumes exist |
| `storage.summary` | Volumes not checked yet |
| `instance.image` | Application image is downloaded |
| `host.memory` | Available memory |
| `optional.ollama` | Local AI (optional) |
| `host.platform` | This computer (informational) |
| `config.instance_file.<name>` | A saved instance file could not be read |

`action`, `actionLabel`, and `actionValue` appear together when a check has a
guided next step ([§7](#7-doctor-action-vocabulary-guided-repair)).
`actionValue` and the instance in `hint` are the **instance name** that
`--name` accepts. Earlier CLIs used the container name. `actionValue` is
omitted for an installation that cannot be selected by name (a `--config`
file outside the instances directory, or the legacy single file); run the
follow-up command without `--name`.

### `omnideck logs --json`

**Follow defaults to `false` under `--json`**, although the human default is
`--follow=true`, so a scripted call never blocks forever by accident. Pass
`--follow` (`-f`) explicitly for the stream.

Without `--follow` — single value; `--tail N` controls the line count
(default 50):

```json
{"lines": ["2026-08-01T10:00:00Z INFO starting", "2026-08-01T10:00:01Z INFO ready"]}
```

With `--follow --json` — one NDJSON object per log line:

```json
{"line": "2026-08-01T10:00:00Z INFO starting", "ts": "2026-08-01T10:00:00Z"}
```

`ts` is when the CLI observed the line (RFC 3339, UTC), not a timestamp parsed
from the container's log text. The stream has no `complete` event: it ends
when the process is killed or the log source closes.

---

## 5. NDJSON streaming commands

### Shared envelope

Every line is one JSON object with at least `stage` and `state`:

```json
{"stage": "pull_image", "state": "start"}
{"stage": "pull_image", "state": "progress", "detail": "Copying blob…"}
{"stage": "pull_image", "state": "done"}
{"stage": "complete", "state": "done", "result": {}}
```

- `state` is `"start"`, `"progress"`, `"done"`, `"error"`, or (runtime setup
  only) `"permission"`.
- Optional fields: `detail` (raw progress text — do not parse it), `result`
  (a stage-specific result on `done`, and the command result on `complete`),
  and for `runtime ensure` also `substage`, `activity`, `status`, and
  `progress` (a number from 0 to 1).
- Every stage that runs emits `start` then `done` before the next stage
  starts. **Stages that do not apply are skipped entirely** — do not assume a
  fixed count.
- The **final line** is either
  `{"stage": "complete", "state": "done", "result": {...}}` on success, or
  `{"stage": "<stage that failed>", "state": "error", "error": {...}}`, where
  `error` is the same object as in [§3](#3-structured-error-shape). Nothing
  follows either.

### `omnideck add --json` (aliases `install`, `setup`)

Stages: `check_availability` → `create_home_volume` → `create_state_volume` →
`pull_image` (with `progress`) → `run_container` → `save_config`. The
`complete` result is the status shape for the new instance.

Flags: `--name`, `--port`, `--memory`, `--shm-size`, `--image`, `--runtime`.
An invalid flag value is `INVALID_SETTING` (for example `--port` →
`setting: "web_ui_port"`); a name already used by a container or a saved
instance, or whose data volumes another saved instance uses, is
`CONTAINER_CONFLICT`; a port another saved instance or another app uses is
`PORT_IN_USE`. All of these are single error values reported before any stage
starts. Without `--name`, `add` picks a name that avoids all of them.

`add --suggest-defaults --json` is a non-mutating single value that previews
what a plain `add` would pick:

```json
{"name": "omnideck2", "webUiPort": "2338"}
```

**Cancellation and teardown**: if the process is killed before the new
instance's config file is saved, the CLI best-effort removes whatever
container and volumes that run created — nothing could discover a
half-created instance otherwise, since `list`, `doctor`, and `remove` are all
keyed off the saved file. The stream still ends with a `CANCELLED` error.

### `omnideck update --json`

Stages: `pull_image` (with `progress`) → `recreate` → `external_url`. The
`complete` result is the status shape after the update.

The `external_url` done event reports what happened to the container's OAuth
callback origin (`OMNIDECK_EXTERNAL_URL`), which MCP and Slack integrations
need and which must equal the browser address `http://localhost:<webUiPort>`:

```json
{"stage":"external_url","state":"done","result":{"state":"corrected","expected":"http://localhost:2339","previous":"http://localhost:9999"}}
```

`state` is `set` (the old container had no value), `corrected` (`previous`
holds the replaced value), `confirmed` (it was already right, including an old
address that differed only because the update applies a saved port change), or `verified`
(the new container is right, but the old one could not be read, so whether
anything changed is unknown). The value is read back from the
new container; if it is wrong or cannot be read, the stage fails. By then the
update has been applied and saved, so retrying is safe. The update also
records the current container layout, so a later `environment ensure` reports
`unchanged`.

A failure or cancellation during `recreate` rolls back to the previous,
already-saved container and config automatically. Failures use the same codes
as `add`: `DOWNLOAD_FAILED` for `pull_image`, `PORT_IN_USE` when another app
holds the port, `CONTAINER_CONFLICT`, and `CANCELLED`.

### `omnideck remove <name> --json`

The instance is the positional `<name>` or `--name`, like every other instance
command. Removal never picks one implicitly: giving neither, or two different
names, is `MISSING_REQUIRED_FLAG`.

If another saved instance uses the same container, the container is left
running and only this instance's settings are removed. `--delete-volumes` on a
data volume another saved instance uses is `CONTAINER_CONFLICT` and removes
nothing.

`remove` is destructive, so under `--json` these flags are required, and a
missing one is `MISSING_REQUIRED_FLAG` before any runtime call:

- `--yes`, always.
- Exactly one of `--keep-volumes` or `--delete-volumes`.
- With `--delete-volumes`, exactly one of `--backup` or `--no-backup`.

`--backup-dir <folder>` saves the backup in an existing folder instead of the
home folder. It is only valid with `--delete-volumes --backup`
(`MISSING_REQUIRED_FLAG` otherwise), and a folder that does not exist or is
not a folder is `INVALID_SETTING` with `setting: "backup_dir"`. Both are
checked before anything is removed.

Passing `--yes --delete-volumes` *is* the confirmation; there is no
"type the name" step.

Stages (only those that apply): `prepare` → `stop_container` → [`backup`] →
`remove_container` → [`delete_volumes`]. Result:

```json
{
  "containerStopped": true,
  "containerRemoved": true,
  "removedVolumes": ["omnideck-home", "omnideck-state"],
  "backupPath": "/home/user/omnideck-backup-omnideck-20260801-143022.123456789.tar.gz"
}
```

`removedVolumes` is `[]`, never `null`, when nothing was deleted.
`backupPath` is omitted when no backup was made. Backups are named
`omnideck-backup-<container>-<timestamp>.tar.gz` in `--backup-dir` or the
user's home folder; show the returned path rather than assuming it.

A cancelled remove needs no cleanup: a partly removed instance is a state
`doctor` and a retried `remove` already handle.

### `omnideck environment ensure --json`

The idempotent application transaction Desktop uses. `--name` is required
and is the instance name. A new instance's container takes the same name; an
existing instance keeps its saved container name. Optional flags give the
exact desired `--image`, `--port`, `--memory`, `--shm-size`, `--home-volume`,
and `--state-volume`; anything omitted keeps the saved value or the default.

It creates, starts, repairs, or safely replaces the environment, persists the
instance file, then waits for the application to answer before returning.
Stages (only those that apply): `check_environment` → `start_container`, or
`prepare_storage` → `pull_image` → `create_container` / `replace_container` →
`save_config`, then `startup`. A startup failure keeps the instance and its
volumes for diagnosis and retry.

```json
{
  "changed": true,
  "action": "recreated",
  "reason": "external_url_missing",
  "status": {"name": "desktop", "container": "omnideck-desktop", "status": "running", "...": "status shape"}
}
```

`action` is `unchanged`, `started`, `created`, `repaired`, or `recreated`.
The optional `reason` explains a `recreated` result the saved settings did
not require: `external_url_missing` or `external_url_mismatch` means the
container lacked, or had a different, `OMNIDECK_EXTERNAL_URL`. That repair
runs only `check_environment` → `replace_container`: it recreates the
container from the exact image it was running (by image ID, so a tag another
instance's update moved is not followed), reuses the volumes it mounts, and
leaves the saved settings unchanged. If the container or its volumes cannot
be inspected, ensure leaves it alone and starts it as usual; if a saved volume
is missing, it stops with an error naming that volume rather than let Podman
create an empty one. An address on a different port than the saved one is
usually a saved `config set` port change: ensure applies it the same way,
from the running image, after checking that the saved port is free while the
old container keeps running; if another app holds it, ensure fails with
`PORT_IN_USE` and leaves the container unchanged. Otherwise a stopped container
is left in place when another app holds its port; a running one is stopped to
check the port (waiting a few seconds for the runtime to release it) and, if
another app still holds it, started again and reported `unchanged`.
Saved data volumes are never removed; if one is missing, ensure stops
with `MISSING_STORAGE` rather than create an empty replacement. Invalid flag
values are `INVALID_SETTING`. A container name or data volume that belongs to
another saved instance is `CONTAINER_CONFLICT`, checked before any runtime call.

### `omnideck repair --json`

Makes a saved instance run again without a reviewed update. It is the command
for Doctor's `repair_instance` action and for `CONTAINER_NOT_FOUND`, and the
TUI's Repair runs the same transaction.
Requires an instance ([§2](#2-global-flags-relevant-to-a-gui-wrapper)).

- **Missing container:** recreates it from the saved settings, reusing the
  saved image already stored locally. It downloads only when that image is
  not stored (or cannot be checked), with a `pull_image` stage. The container
  gets the current layout and a legacy image name is migrated to the current
  one; both are saved (`save_config`). If that save fails, the new container
  is removed again so nothing changes. The lost
  container's exact image is no longer known, so this is the version the
  saved image name points to locally: usually the one it ran, but a newer
  one if another instance's update has since moved a shared tag such as
  `:latest`.
- **Existing container:** starts it if stopped. If its MCP sign-in address is
  missing or wrong, it is replaced from the exact image it runs, as
  `environment ensure` does. It never replaces a healthy container just to
  modernize it; that is `update`'s job.
- Saved data volumes are reused and never created empty: a missing one is
  `MISSING_STORAGE`, before anything changes.

Stages are the `environment ensure` stages that apply, then `startup`. The
`complete` result has the `environment ensure` shape:

```json
{"changed": true, "action": "repaired", "status": {"name": "desktop", "container": "omnideck-desktop", "status": "running", "...": "status shape"}}
```

`action` is `repaired` whenever the container was missing, otherwise
`unchanged`, `started`, or `recreated` (with `reason` for a sign-in address
fix). Other failures use the `environment ensure` codes.

### `omnideck runtime status --json` / `runtime ensure --json`

`runtime status` is a single value, schema-versioned independently of
`jsonContract` (currently schema `4`, see `contracts/runtime/v4`):

```json
{
  "schemaVersion": 4,
  "runtime": "podman",
  "state": "ready",
  "ready": true,
  "version": "5.8.4",
  "machineName": "omnideck-runtime",
  "phase": "environment",
  "activity": "Preparing a secure space to run in…",
  "resources": {
    "container": {"memory": "4g", "shmSize": "2048m"},
    "machine": {"mode": "podman-managed", "memoryMB": 6144}
  }
}
```

`state` is `ready`, `missing`, `stopped`, `machine_missing`,
`machine_stopped`, `machine_needs_update`, `permission_denied`,
`unsupported_version`, or `broken`. `resources.machine.mode` is `host-native`
(Linux), `wsl-managed` (Windows), or `podman-managed` (macOS); `cpus`,
`memoryMB`, and `diskGB` are omitted when the platform does not set them.
`path`, `version`, `detail`, `warning`, and `machineName` are omitted when
empty. `phase` is `software` while Podman is missing or too old, otherwise
`environment`.

`runtime ensure` returns the same single value when Podman is already ready.
Otherwise it streams setup events with stages `software` and `environment`,
optional `substage` values (`wsl-permission`, `wsl-enable`, `windows-restart`,
`podman-permission`, `podman-download`, `podman-install`, `macos-permission`,
`package-index`, `secure-space`), and a `permission` state while waiting for
administrator approval. It ends with `complete` carrying the status payload,
or an error using the runtime setup codes in [§3](#3-structured-error-shape).

---

## 6. `start` / `stop` / `restart` / `wait` --json

Each runs its action, then emits the [status shape](#omnideck-status---json)
gathered fresh, so no follow-up `status` call is needed.

- `start` and `restart` succeed only after the application answers over HTTP.
  They wait up to two minutes.
- `wait` performs the same readiness check without changing the container.
- `stop` succeeds as soon as the container is stopped; stopping a stopped
  container is a successful no-op.
- `restart` is stop-then-start; if the stop fails, the start never runs.

Readiness failures since the current check began — including automatic
container restarts — are reported as `MIGRATION_FAILED` or
`APP_STARTUP_FAILED`. A new retry starts a fresh check. A missing container is
`CONTAINER_NOT_FOUND`, with `action: repair_instance` and the instance name in
`actionValue` when its saved data volumes still exist. Other failures are
`ENGINE_NOT_FOUND`, `PORT_IN_USE`, `CONTAINER_CONFLICT`, `CANCELLED`, or
`INTERNAL_ERROR`.

### Container startup events

The application may emit `OMNIDECK_STARTUP ` followed by one JSON object on
stderr. Version 1 requires `version: 1`, a UTC `timestamp`, `phase`, and
`status` (`starting`, `complete`, or `failed`). `step` is optional; failures
may include `code`. Phase and step are code-owned identifiers matching
`[a-zA-Z0-9_:.-]{1,100}`, never user data, paths, or exception text.

The CLI maps `phase: migrations` with `code: MIGRATION_FAILED` to the
specific settings-upgrade error; other valid phase failures produce
`APP_STARTUP_FAILED`. Completion clears only that phase's failure and never
establishes readiness — HTTP 2xx remains the readiness signal. Events older
than Podman's initial `StartedAt` for the check are ignored, and an automatic
restart does not clear a failure. Images without these events remain
supported and report the general error on exit or timeout.

---

## 7. Doctor `action` vocabulary (guided repair)

`doctor --json`'s per-check `action` and the error envelope's `action` share
these values:

| `action` | Meaning | What to do with `actionValue` |
|---|---|---|
| `runtime_setup` | Podman needs installing or starting. | The runtime name (`podman`). Run `runtime ensure`. |
| `start_instance` | A known instance exists but is not running. | Pass as `--name` to `omnideck start`. |
| `setup_instance` | No instance exists yet. | Absent. Guide the person through `omnideck add`. |
| `repair_instance` | The instance's container is missing but its saved data volumes exist. | Pass as `--name` to `omnideck repair`. |
| `update_instance` | The instance's container lacks, or has a wrong, MCP sign-in address (`instance.external_url`). | Pass as `--name` to `omnideck update`, which downloads the latest version and keeps saved data. `omnideck repair` fixes the address without changing the version. |

No `action` means there is nothing actionable to offer; show the message.

---

## 8. Process lifecycle rules for a GUI wrapper

- **Exit codes**: `0` = success. `1` = failure *or* a non-affirmative observed
  state. Always parse stdout, even on exit `1`; a bare "non-zero means red X"
  is wrong for `status`/`doctor` on a healthy but stopped instance.
- **stdout only ever contains JSON/NDJSON under `--json`.** Do not merge
  stdout and stderr when parsing.
- **Killing the process is safe** and is the documented way to stop a
  `--follow` log stream or, in a pinch, an in-flight stream. The CLI wires
  SIGINT/SIGTERM into every Podman subprocess it spawns
  (`engine.SetCancelContext` in `cmd/root.go`), so no child is orphaned.
  Streams end with `CANCELLED` and leave a state that `doctor` or a retry can
  recover from; see the per-command notes in [§5](#5-ndjson-streaming-commands).
- **A closed output pipe stops NDJSON emission cleanly** rather than
  crashing. You need not drain a stream you have abandoned.
- **One invocation, one answer.** There is no daemon or RPC mode. Poll
  `status --json` or `list --json` for a live dashboard.

---

## 9. Worked example: a minimal GUI instance-management flow

```
# Discover what's installed
$ omnideck list --json
[]

# Preview defaults before showing a "create instance" form
$ omnideck add --suggest-defaults --json
{"name":"omnideck","webUiPort":"2337"}

# Create it, streaming progress into a progress bar
$ omnideck add --json --name omnideck --port 2337
{"stage":"check_availability","state":"start"}
{"stage":"check_availability","state":"done"}
{"stage":"create_home_volume","state":"start"}
{"stage":"create_home_volume","state":"done"}
{"stage":"create_state_volume","state":"start"}
{"stage":"create_state_volume","state":"done"}
{"stage":"pull_image","state":"start"}
{"stage":"pull_image","state":"progress","detail":"..."}
{"stage":"pull_image","state":"done"}
{"stage":"run_container","state":"start"}
{"stage":"run_container","state":"done"}
{"stage":"save_config","state":"start"}
{"stage":"save_config","state":"done"}
{"stage":"complete","state":"done","result":{"name":"omnideck","status":"running", ...}}

# Poll for the dashboard
$ omnideck list --json
[{"name":"omnideck","status":"running", ...}]

# Health check on demand
$ omnideck doctor --name omnideck --json
{"checks":[...],"allPass":true}
```

`--json` implies the non-interactive path that `--plain` selects in text
mode, so `add --json` and `add --plain --json` behave identically for `add`,
`update`, and `remove`.
