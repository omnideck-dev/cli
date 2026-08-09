# Local VM lab workflow

This is the repeatable CLI hardware workflow for the external OmniDeck release
lab. It keeps Go off the developer host by building inside the repository's
`.devcontainer/Dockerfile` image, and it keeps guest state on disposable lab
overlays.

The helper runs the non-visual Podman lifecycle suite. It does not claim the
interactive runtime-install, elevation, reboot, or desktop-window procedures.
Those require the commands below and a graphical viewer.

## Safety and ownership

Set the lab path without committing its machine-specific value:

```sh
export OMNIDECK_VM_LAB_DIR=/absolute/path/to/omnideck-release-lab
test -x "$OMNIDECK_VM_LAB_DIR/lab.sh"
cd "$OMNIDECK_VM_LAB_DIR"
./lab.sh status
```

Start only the guest you own for the current run. The `flock` lease is
intentional: QEMU inherits the lock descriptor, so the lock remains held while
the guest is running and a second operator cannot start the same lane:

```sh
flock -n /tmp/omnideck-cli-atomic-lease.lock bash -c \
  'cd "$OMNIDECK_VM_LAB_DIR" && ./lab.sh start atomic && ./lab.sh wait atomic && ./lab.sh verify atomic'
fuser -v /tmp/omnideck-cli-atomic-lease.lock
```

If the lease is already held, inspect the owner before doing anything. Never
stop, reset, or snapshot a guest owned by another run. Keep the Windows guest
stopped when it is reserved for desktop testing.

## Repeated CLI lifecycle test

Build the reusable Go builder image once from the CLI worktree:

```sh
cd /path/to/omnideck-cli
docker build --tag omnideck-cli-builder:local \
  --file .devcontainer/Dockerfile .devcontainer
```

Run the helper from this worktree after the selected guest reports ready:

```sh
cd /path/to/omnideck-cli
export OMNIDECK_VM_LAB_DIR=/absolute/path/to/omnideck-release-lab
OMNIDECK_VM_LAB_VM=atomic \
OMNIDECK_HARDWARE_ENGINE=podman \
./tests/manual/run-local-hardware.sh
```

The helper builds the current checkout in the container, records the binary
checksum, copies the binary and hardware harness into the guest, runs the
unique `omnideck-hw-*` lifecycle, copies back `summary.json`, `junit.xml`, and
diagnostic logs, and removes only its generated guest staging directory. The
generated host artifacts are under the external lab's `artifacts/` directory.

To compare a pristine branch, run the same helper from a clean worktree. A
baseline run is useful when a harness assertion is stale or when a refactor is
intended to preserve behavior.

## Non-scriptable desktop and cleanup commands

Use the graphical console for setup prompts, visible terminal behavior,
restart/resume, and any visual result:

```sh
cd "$OMNIDECK_VM_LAB_DIR"
./lab.sh viewer atomic
./lab.sh viewer windows
```

The exact Windows lane is:

```sh
./lab.sh start windows
./lab.sh wait windows
./lab.sh verify windows
./lab.sh viewer windows
# Perform the published desktop clean-first-run procedure in the viewer.
./lab.sh stop windows
./lab.sh reset windows
./lab.sh status windows
```

Only after confirming the copied evidence contains everything needed, end a
CLI guest run and restore its disposable overlay:

```sh
./lab.sh stop atomic
./lab.sh reset atomic
./lab.sh status atomic
```

`reset` archives and replaces the active overlay. It is destructive to that
guest's test state and must not be used as a generic cleanup command.

For Linux desktop candidates launched from an AppImage, verify the packaged
host path as well as the CLI lifecycle. The CLI removes AppImage loader
variables from host subprocesses so Podman's `conmon` uses the guest's native
GLib libraries.

## Evidence record

For each run retain the source commit, builder image tag/digest, CLI checksum,
guest identifier, guest OS/version, Podman version, exact helper or manual
commands, start/end timestamps, result, artifact paths, and any blocked
visual/manual steps. Redact credentials and personal home-directory details.
