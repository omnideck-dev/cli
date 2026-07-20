# Hardware test harness

This harness exercises the compiled CLI against a real Docker or Podman runtime.
It is intended for dedicated macOS, Windows, and Linux test machines. It can be
run manually before self-hosted GitHub runners are available.

The lifecycle scenario uses a uniquely named container, two uniquely named
volumes, high host ports, a temporary localhost image registry, and a tiny
fixture image. It never uses the production Omnideck image or an existing
Omnideck instance.

## Current coverage

The scripts currently verify:

- CLI build, version, and help
- explicit Docker or Podman selection
- non-interactive install
- saved runtime and instance configuration
- web UI port mapping
- status and logs
- volume persistence
- stop, start, and restart
- doctor report
- uninstall and resource cleanup
- JSON, JUnit, command, container, and diagnostic output

The following need separate scenarios before the hardware suite can be called
complete:

- guided terminal setup and its key-by-key interaction
- actual Podman or Docker installation and operating-system permission prompts
- `omnideck update`, which currently has no non-interactive mode
- upgrade from the previous stable CLI and production image
- uninstall with a real data backup and restore
- multiple simultaneous instances
- local Ollama connectivity

These gaps are deliberately visible. The lifecycle script does not claim that a
simulated runtime installation is a real installation test.

## Run manually

The machine needs Go, curl on macOS/Linux, and one ready container runtime.

macOS or Linux:

```sh
OMNIDECK_HARDWARE_ENGINE=auto ./tests/hardware/run.sh
```

Windows PowerShell:

```powershell
./tests/hardware/run.ps1 -Engine auto
```

Use `docker` or `podman` instead of `auto` to test a specific runtime. `auto`
uses the same platform recommendations as setup: Podman on Apple Silicon and
Linux, Docker on Intel macOS and Windows, with the other runtime as fallback.

Artifacts are written beneath `artifacts/hardware/`. To select a different
directory or port:

```sh
OMNIDECK_HARDWARE_OUTPUT_DIR=/path/to/results \
OMNIDECK_HARDWARE_PORT=45123 \
OMNIDECK_HARDWARE_REGISTRY_PORT=46123 \
./tests/hardware/run.sh
```

PowerShell accepts `-OutputDirectory`, `-Port`, and `-RegistryPort`. Set
`OMNIDECK_HARDWARE_KEEP_RESOURCES=1` (or use `-KeepResources` on Windows) only
while debugging. The generated instance name always begins with
`omnideck-hw-` so it is visibly separate from user data.

By default the harness builds the current checkout. To test an extracted beta or
release binary instead, set `OMNIDECK_HARDWARE_CLI` to its path, or pass
`-CliUnderTest` to the PowerShell script.

## Safety model

The lifecycle scenario does not install software, ask for an administrator
password, modify user groups, or control system services. Cleanup targets only
the generated `omnideck-hw-*` application and registry containers, volumes,
image, and configuration.

Only trusted commits from the canonical repository should run on the hardware
machines. Do not expose these runners to arbitrary pull-request code, especially
once runtime-install scenarios can request administrator access.

## GitHub runner labels

The manual workflow expects these labels in addition to `self-hosted`:

| Machine | Labels |
|---|---|
| Apple Silicon Mac | `omnideck-hardware`, `macos`, `arm64` |
| Windows 11 x64 | `omnideck-hardware`, `windows`, `x64` |
| Linux x64 | `omnideck-hardware`, `linux`, `x64` |

The workflow has no schedule and requires an explicit confirmation checkbox, so
it will not queue jobs while no runners exist. Once all three machines are
online and hardened, add a nightly `schedule` trigger and restrict the workflow
to the protected `main` branch.
