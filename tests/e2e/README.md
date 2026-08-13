# Local VM end-to-end suite

This suite treats the compiled CLI as a black box in a clean disposable local
lab guest. It is the terminal counterpart to the application browser E2E suite:
the assertions describe the behavior a person sees, while the implementation
is free to change behind that boundary.

The controller is maintained in the standalone `omnideck-vm-lab` repository,
not the CLI repository. Install controller 2.x into the external lab before
running this suite.

Visible wording, capitalization, and punctuation are part of that boundary.
The PTY matcher ignores only terminal layout whitespace, since Bubble Tea may
pad the same key label differently at different widths; copy changes fail the
run and retain the transcript for deliberate review.

## What it covers

One run verifies all of the following against the current checkout:

- release-shaped archive checksum, extraction, executable installation,
  embedded version, and the `install` command alias;
- first-run Welcome, progress phases, operating-system permission handoff,
  Podman installation, fixture-image pull, container creation, and Ready screen;
- the returning dashboard, expanded runtime card, live logs, log filtering,
  settings, and update review/cancel behavior;
- stopping an instance and using Doctor's interactive action to start it;
- recommended name and port selection for a second instance, with cancellation
  before mutation;
- guarded permanent removal, including the explicit data and backup choices,
  typed instance-name confirmation, and resource cleanup; and
- config, browser-port, container, volume, transcript, inventory, JSON, and
  JUnit evidence.

After the attended journey, the same invocation runs the portable black-box
contract and the unattended hardware lifecycle. That second lifecycle covers
plain setup, list/status/config/logs, JSON surfaces, same-image update,
stop/start/restart, Doctor, persistence, and removal against uniquely named
resources.

The application image is a tiny local BusyBox fixture from
`tests/hardware/fixture`. The guest reaches the host's loopback-only registry
through an SSH reverse tunnel, so the suite does not publish an image or expose
a registry on the LAN. The Windows lane wraps that registry in per-run TLS,
installs only its ephemeral CA in the disposable Podman machine, and removes
the bridge and firewall rule during cleanup.

The Linux SSH lane has no graphical PolicyKit agent. During only the attended
install, the disposable guest temporarily hides `pkexec`, causing the CLI to
exercise its documented terminal `sudo` fallback. A five-second test-only wrapper keeps
the permission screen observable on the lab's passwordless-sudo account, then
passes the original command and arguments to `/usr/bin/sudo`. The `pkexec` file
is restored by the evidence trap even on failure. A real desktop session
remains the appropriate test for the graphical PolicyKit dialog itself.

## Run before a release

The external lab path stays machine-local:

```sh
export OMNIDECK_VM_LAB_DIR=/absolute/path/to/omnideck-release-lab
make vm-e2e
```

The default lane is the Ubuntu `appimage` guest. The command requires that guest
to be stopped, acquires an exclusive lease, and asks you to type its name before
resetting it. It restores the clean golden after the run. Use another mutable
Linux package family with:

```sh
make vm-e2e VM=deb
make vm-e2e VM=rpm
make vm-e2e VM=windows
```

The Windows lane starts from a Windows 11 golden without WSL or Podman. It
drives the real Windows Security/UAC prompt through the QEMU console, captures
a screenshot, locks the restart copy, selects **restart later**, performs and
verifies a full reboot, installs the pinned official Podman MSI, creates the
`omnideck-runtime` WSL machine, and continues the same attended and unattended
coverage. The explicit reboot makes this lane repeatable over SSH; the
**restart now** RunOnce visual auto-reopen remains a separate manual check.

For unattended use on a workstation whose VM lane is already reserved for this
run:

```sh
./tests/e2e/run.sh --vm appimage --yes
```

Use `--keep-vm` only to debug a failure. It leaves the guest stopped and retains
the isolated config, containers, volumes, and staged runner inside that guest;
a later normal run will still require an explicit clean reset. Never use the
flag as a reason to stop or reset a VM owned by another session.

Artifacts are written outside the repository in exactly one directory per run:
`$OMNIDECK_VM_LAB_DIR/artifacts/cli/e2e/<run>/`. The compact evidence
includes raw PTY output, readable transcripts, semantic checkpoint events,
guest logs, before/after inventories, CLI/runtime inspection, `summary.json`,
and `junit.xml`.

The lab archives reset state inside the run transaction. Successful transaction
state is deleted immediately. Failed state and compact evidence expire after 48
hours unless explicitly pinned.

Purge a run—including any retained overlays named by its manifest—with:

```sh
make vm-e2e-purge RUN="$OMNIDECK_VM_LAB_DIR/artifacts/cli/e2e/<run>"
```

The purge command delegates to the lab's marked-run API, shows disk usage, and
requires the exact run directory name before deleting anything. Pass `--yes`
directly to `tests/e2e/purge.sh` only in trusted local automation. Routine
`lab.sh gc` removes unpinned evidence after the short retention window.

## Scope

The automated lanes cover mutable x64 Linux guests and the local Windows 11 x64
guest. They do not claim stable-to-candidate production-image upgrades,
backup/restore, macOS installation prompts, subjective visual quality, or the
Windows **restart now** auto-reopen path. Run the checked-in manual procedures
for those release-candidate behaviors.

The terminal driver's dependency-free parser has a fast host check:

```sh
python3 -m unittest tests/e2e/test_terminal_driver.py
bash -n tests/e2e/run.sh tests/e2e/run-windows.sh tests/e2e/guest.sh
```
