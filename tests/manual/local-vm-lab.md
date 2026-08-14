# Local VM lab workflow

This is the CLI workflow for the external OmniDeck release lab. Prefer the
automated E2E matrix for every deterministic Linux and Windows check. Open a
graphical viewer only for behavior listed as manual in the applicable procedure.

The lab's leased physical Apple Silicon host has a separate automated source
lane:

```sh
export OMNIDECK_VM_LAB_DIR=/mnt/data/VMs/omnideck-release-lab
make macos-lab-test
```

This lane installs the prepared CLI under `~/.omnideck-lab` on the dedicated
Mac and owns a `runtime-ready` cleanup baseline. It removes the lab-managed
binary and namespaced resources after every run while retaining the user's
normal CLI/config/resources and the costly Podman installation and machine.

It runs isolated CLI/container lifecycle coverage and copies evidence back to
the lab without treating the physical Mac as a resettable VM.

## Preflight and ownership

Set the lab path without committing its machine-specific value:

```sh
export OMNIDECK_VM_LAB_DIR=/absolute/path/to/omnideck-release-lab
test -x "$OMNIDECK_VM_LAB_DIR/lab.sh"
cd "$OMNIDECK_VM_LAB_DIR"
./lab.sh doctor --strict
./lab.sh preflight cli release-clean --lanes appimage,deb,rpm,windows
```

Every command that changes a guest must run inside the lab-owned lease. The
cleanup baseline restores the clean checkpoint when the shell exits normally,
fails, or is interrupted:

```sh
./lab.sh lease silverblue cli-manual --cleanup-baseline clean -- bash
./lab.sh start silverblue
./lab.sh wait silverblue
./lab.sh verify silverblue
./lab.sh viewer silverblue
# Perform only the remaining manual observations, then exit the shell.
exit
```

If the lease is already held, `lab.sh status` reports its owner. Never stop,
reset, or snapshot a guest owned by another run. Do not issue a second reset at
the end of a lease that already has `--cleanup-baseline clean`.

## Canonical automated regression

Run the complete deterministic matrix from the CLI worktree:

```sh
cd /path/to/omnideck-cli
export OMNIDECK_VM_LAB_DIR=/absolute/path/to/omnideck-release-lab
make vm-e2e-matrix YES=1
```

Override `LANES=appimage,windows` only when intentionally running a subset. The
matrix preflights the selected profile, prepares one content-addressed CLI build
before acquiring a guest, leases each lane in deterministic order, restores the
clean baseline, and writes a single aggregate record under
`$OMNIDECK_VM_LAB_DIR/artifacts/cli/matrix/`.

For a single mutable lane, use `make vm-e2e VM=appimage` or the compatibility
wrapper:

```sh
OMNIDECK_VM_LAB_VM=appimage ./tests/manual/run-local-hardware.sh --yes
```

The wrapper delegates to the same E2E lane; it no longer maintains separate
SSH, builder-image, artifact-path, or cleanup logic. Each lane runs the packaged
first-run/TUI journey, portable contract, and Podman hardware lifecycle against
the same cached release-shaped binary.

## Remaining manual CLI checks

The automated mutable-Linux lanes cover first run, returning behavior, TUI
management, and the unattended lifecycle. The Windows lane covers real UAC,
restart-later, a controlled reboot, Podman installation, TUI behavior, and the
same lifecycle. Keep these checks manual:

- the Windows **Restart now** RunOnce auto-reopen path;
- native macOS prompts and behavior;
- graphical PolicyKit presentation and subjective terminal/visual quality; and
- stable-to-candidate production-image upgrade, backup, and restore.

Create a marked evidence directory before a manual viewer run so all generated
records land under the lab root:

```sh
cli_root=/path/to/omnideck-cli
source_commit="$(git -C "$cli_root" rev-parse HEAD)"
cd "$OMNIDECK_VM_LAB_DIR"
run_id="cli-manual-$(date -u +%Y%m%dT%H%M%SZ)"
evidence_dir="$(./lab.sh artifact-path cli manual "$run_id")"
export evidence_dir
./lab.sh evidence-init "$evidence_dir" cli manual "$run_id" \
  "$source_commit" windows clean
./lab.sh lease windows cli-manual "$run_id" --cleanup-baseline clean -- bash
./lab.sh start windows
./lab.sh wait windows
./lab.sh verify windows
./lab.sh viewer windows
# Perform only the applicable manual procedure and write compact results to:
printf '%s\n' "$evidence_dir"
exit
./lab.sh evidence-finish "$evidence_dir" passed
```

If the manual assertion fails, finish it as `failed`. Record the source commit,
candidate checksum, guest inventory, exact observations, timestamps, and final
result. Do not copy candidates or raw VM state into the repository.

## Evidence cleanup

Successful reset transactions disappear immediately. Unpinned evidence and
failed state expire after 48 hours; unused content-addressed caches expire after
seven days. Preview or apply that shared policy from the CLI worktree:

```sh
make vm-lab-cleanup
make vm-lab-cleanup APPLY=1
```

For an intentional complete removal of generated evidence, caches, and retained
reset state:

```sh
make vm-lab-cleanup ALL=1
make vm-lab-cleanup ALL=1 APPLY=1
```

Cleanup never targets golden images, named checkpoints, base images, automation,
or keys.
