# Omnideck JSON mode

`--json` is the stable process boundary for native shells and automation. It
never prompts, opens a TUI, or writes styled text to stdout. The contract
version is returned by `omnideck --json --version` as `jsonContract`.

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
- `status`, `start`, `stop`, and `restart` return the selected instance status.
- `environment ensure` is the idempotent application transaction used by
  Desktop. It accepts the exact desired name, image, loopback port, memory,
  shared-memory size, and volume names. It creates, starts, repairs, or safely
  replaces the environment and persists its instance YAML before returning.

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
`PERMISSION_DENIED`, `DOWNLOAD_FAILED`, `UNSUPPORTED`,
`RUNTIME_SETUP_FAILED`, `PORT_IN_USE`, `CONTAINER_CONFLICT`, `CANCELLED`, and
`INTERNAL_ERROR`.
