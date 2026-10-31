# Omnideck JSON mode

`--json` is the stable process boundary for native shells and automation. It
never prompts, opens a TUI, or writes styled text to stdout. The contract
version is returned by `omnideck --json --version` as `jsonContract` (currently
`4`).
Machine-readable schemas live under [`contracts/`](contracts/README.md).

Structured failures are written to stdout without Cobra appending an empty
`Error:` line or command usage to stderr. Callers should still capture both
streams for diagnostics, but they never need to parse Cobra text to understand
a JSON-mode result.

Ordinary commands emit one JSON value. Long-running commands emit newline-
delimited JSON (NDJSON), one flushed event per line:

```json
{"stage":"pull_image","state":"start"}
{"stage":"pull_image","state":"progress","detail":"Copying blob…"}
{"stage":"pull_image","state":"done"}
{"stage":"complete","state":"done","result":{}}
```

`state` is `start`, `progress`, `done`, or `error`. A terminal error carries:

```json
{"stage":"pull_image","state":"error","error":{"code":"DOWNLOAD_FAILED","message":"…","hint":"…"}}
```

Callers must ignore unknown fields and stages. A process exit code of zero
means the terminal result was successful; nonzero means the stream or JSON
value contains an error.

## Desktop runtime contract

- `runtime status` returns schema-versioned Podman readiness and the shared
  machine/container resource policy.
- `runtime ensure` emits prerequisite and machine-setup progress, then the same
  status payload.
- `status`, `start`, `stop`, `restart`, and `wait` return the selected instance status.
- `start` and `restart` succeed only after the application answers over HTTP.
  `wait` performs the same check without changing the container. It waits up to
  two minutes and reports failures since the current startup check began, including automatic
  container restarts. A new user retry starts a fresh check.
- `environment ensure` is the idempotent application transaction used by
  Desktop. It accepts the exact desired name, image, loopback port, memory,
  shared-memory size, and volume names. It creates, starts, repairs, or safely
  replaces the environment and persists its instance YAML, then waits for the
  application before returning. A startup failure keeps the instance and its
  volumes available for diagnosis and retry.

The `environment ensure` complete result is:

```json
{
  "changed": true,
  "action": "created",
  "status": {
    "name": "omnideck-desktop",
    "container": "omnideck-desktop",
    "status": "running",
    "image": "ghcr.io/omnideck-dev/omnideck@sha256:…",
    "engine": "podman",
    "webUiPort": "2338",
    "homeVolume": {"name":"omnideck-desktop-home","exists":true},
    "stateVolume": {"name":"omnideck-desktop-state","exists":true},
    "ollama": {"reachable":true,"host":"http://host.containers.internal:11434"}
  }
}
```

`action` is `unchanged`, `started`, `created`, `repaired`, or `recreated`.

## Error codes

The closed error-code vocabulary is defined in `cmd/jsonout.go`. Codes relevant
to native setup include `ENGINE_NOT_FOUND`, `RESTART_REQUIRED`,
`PERMISSION_DENIED`, `PERMISSION_CANCELLED`, `WINDOWS_FEATURES_FAILED`,
`PACKAGE_INDEX_FAILED`, `INSTALLER_FAILED`, `DOWNLOAD_FAILED`, `UNSUPPORTED`,
`RUNTIME_SETUP_FAILED`, `PORT_IN_USE`, `CONTAINER_CONFLICT`, `CANCELLED`, and
`MIGRATION_FAILED`, `APP_STARTUP_FAILED`, and `INTERNAL_ERROR`.

`MIGRATION_FAILED` means the application's migration barrier stopped startup.
The safe `detail` names the failed migration; it never copies saved records,
credentials, or raw logs. `APP_STARTUP_FAILED` means the process exited or the
application did not answer before the deadline without a recognized migration
failure. Neither failure automatically deletes data or retries a migration.

Startup diagnostics are version-1 `OMNIDECK_STARTUP ` JSON records in container
logs. Consumers accept only known codes and safe migration identifiers, and
ignore records older than Podman's initial `StartedAt` for that check. Published images without these
records still use the HTTP readiness check and generic startup error.

Contract 4 adds the migration/startup error codes and makes `start`, `restart`,
and `environment ensure` success mean application readiness. Contract 3 schemas
remain available for previously published CLIs. Runtime status stays schema 4.

## Container startup events

The application may emit `OMNIDECK_STARTUP ` followed by one JSON object on
stderr. Version 1 requires `version: 1`, a UTC `timestamp`, `phase`, and `status`
(`starting`, `complete`, or `failed`). `step` is optional; failures may include
`code`. Phase and step are code-owned identifiers matching
`[a-zA-Z0-9_:.-]{1,100}`, never user data, paths, or exception text.

The CLI maps `phase: migrations` with `code: MIGRATION_FAILED` to the specific
settings-upgrade error. Other valid phase failures produce `APP_STARTUP_FAILED`.
Completion clears only that phase's failure; it never establishes app readiness.
Podman's log timestamps and the initial container start time bound the events
used for a readiness check. An automatic restart does not clear a failure.

HTTP 2xx remains the readiness signal. Existing application images without
these events remain supported, reporting a general error on exit or timeout.
The protocol is carried in logs so errors remain observable before a web server
exists. No container or volume is changed by `wait`.
