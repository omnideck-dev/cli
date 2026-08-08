# Stable upgrade, backup, restore, and removal test

## Purpose and required coverage

Verify a real upgrade from the supported previous stable CLI and production
image. A newly configured candidate, fixture image, or hand-written legacy
configuration is not an upgrade substitute.

Run this procedure separately on:

| Platform | Required host |
|---|---|
| Windows | Windows x64 disposable VM |
| macOS | A supported native macOS architecture |
| Linux | At least one supported mutable distribution on Linux x64; Ubuntu/Debian or Fedora-family |

Additional Linux distributions and Linux ARM64 are useful coverage but do not
replace the required Windows or macOS runs. Conclude and retain evidence for
each platform independently; an unavailable platform is blocked, not passed.

## Starting state and safety

- Use a revertible VM or dedicated test machine with no important Omnideck
  data. Save its snapshot identifier.
- Download the exact previous-stable and candidate assets for the host. Verify
  both published checksums and provenance when present, and record the extracted
  binary hashes and `--version` output.
- Record the operating system, architecture, runtime version, occupied test
  ports, configuration files, Podman machines, containers, images, and volumes.
- Create the stable instance during this procedure. Never repurpose an existing
  user instance as the test fixture.
- Use the production image selected by the stable release, not the hardware
  harness fixture image.
- On Linux, begin from a clean snapshot with Podman absent, record that state,
  install rootless Podman with the distribution package manager, and then run
  the stable CLI. If linger is enabled to keep rootless Podman alive across
  headless SSH sessions, record and revert that VM-only change.
- Stop if an intended name, port, configuration, container, or volume already
  exists. The commands below are destructive only for resources created and
  recorded by this test.

## Stable baseline

Choose unique values. These examples match the Linux x64 release run:

```sh
STABLE_CLI=/absolute/path/to/previous-stable/omnideck
CANDIDATE_CLI=/absolute/path/to/candidate/omnideck
RUNTIME=podman
PRIMARY=omnideck
PRIMARY_PORT=2337
SECOND=upgrade-linux-second
SECOND_PORT=2440
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
```

1. Record the clean inventory and binary identities:

   ```sh
   command -v podman || printf '%s\n' 'podman absent'
   podman version
   podman ps -a
   podman volume ls
   sha256sum "$STABLE_CLI" "$CANDIDATE_CLI"
   "$STABLE_CLI" --version
   "$CANDIDATE_CLI" --json --version
   ```

2. Use the previous stable CLI's supported non-interactive interface to create
   the primary production instance. For v0.8.0, the command is:

   ```sh
   "$STABLE_CLI" install --plain --name "$PRIMARY" \
     --port "$PRIMARY_PORT" --memory 2g --shm-size 1024m \
     --engine "$RUNTIME"
   ```

   If the prior stable interface differs, use its checked-in or release help
   and record the exact command. Do not use the candidate to synthesize the
   stable state.

3. Wait for `http://127.0.0.1:${PRIMARY_PORT}` to return HTTP 200. Record the
   saved configuration and container inspection, including image name, image
   ID/digest, restart policy, memory, SHM, port binding, and mounts.

4. Write unique sentinels to both persistent volumes and give them distinct
   modes:

   ```sh
   HOME_SENTINEL_VALUE="stable-home-${RUN_ID}"
   STATE_SENTINEL_VALUE="stable-state-${RUN_ID}"
   podman exec "$PRIMARY" sh -c \
     'printf %s "$1" > /home/omnideck/upgrade-home-sentinel; chmod 640 /home/omnideck/upgrade-home-sentinel' \
     sh "$HOME_SENTINEL_VALUE"
   podman exec "$PRIMARY" sh -c \
     'printf %s "$1" > /var/lib/omnideck/upgrade-state-sentinel; chmod 600 /var/lib/omnideck/upgrade-state-sentinel' \
     sh "$STATE_SENTINEL_VALUE"
   podman exec "$PRIMARY" sh -c \
     'sha256sum /home/omnideck/upgrade-home-sentinel /var/lib/omnideck/upgrade-state-sentinel; stat -c "%a %u:%g %n" /home/omnideck/upgrade-home-sentinel /var/lib/omnideck/upgrade-state-sentinel'
   ```

   Save the hashes, modes, owners, stable configuration, and inventory before
   invoking any candidate command.

## Candidate migration and lifecycle

1. Before update, run candidate `--json list` and named `--json status`. Confirm
   the candidate discovers the stable instance and reports its stable image,
   port, running state, and both volumes. Save any platform config migration as
   separate before/after evidence.

   ```sh
   "$CANDIDATE_CLI" --json list
   "$CANDIDATE_CLI" --json --name "$PRIMARY" status
   ```

2. Run the actual non-interactive update and record stdout, stderr, and exit
   code:

   ```sh
   "$CANDIDATE_CLI" update --plain --name "$PRIMARY"
   ```

3. Require HTTP 200 after a bounded readiness wait. Record the new config and
   container inspection. Confirm:

   - the candidate image reference is saved and active;
   - only expected schema normalization occurred, such as removal of a legacy
     per-instance `engine` field;
   - the application port is bound to `127.0.0.1`, not every interface;
   - memory, SHM, restart policy, and both named-volume destinations remain
     correct; and
   - both sentinel hashes and 0640/0600 modes are unchanged.

   Record ownership before and after. An application initialization that
   normalizes ownership, such as `0:0` to `1000:1000`, must be visible in the
   result even when content and modes remain correct.

4. Exercise the upgraded lifecycle:

   ```sh
   "$CANDIDATE_CLI" --name "$PRIMARY" stop
   "$CANDIDATE_CLI" --json --name "$PRIMARY" status
   "$CANDIDATE_CLI" --name "$PRIMARY" start
   "$CANDIDATE_CLI" --name "$PRIMARY" restart
   ```

   A stopped status may intentionally use a nonzero exit code while still
   emitting valid JSON. Confirm HTTP is unavailable while stopped, settles at
   200 after start and restart, and both sentinel hashes and modes survive.
   Treat an immediate connection reset during process startup as a readiness
   retry, not as HTTP success.

5. Remove only the primary application container with the runtime, leaving its
   configuration and both volumes. Launch the candidate with no arguments,
   follow the displayed Doctor/repair route, approve the explicit repair, and
   confirm one container is recreated with both sentinels intact. Any manual
   repair outside the displayed CLI instructions is a failure.

   ```sh
   podman rm -f "$PRIMARY"
   podman volume exists "${PRIMARY}-home"
   podman volume exists "${PRIMARY}-state"
   "$CANDIDATE_CLI"
   ```

## Second-instance isolation and keep behavior

1. Add a second instance with the candidate on another port:

   ```sh
   "$CANDIDATE_CLI" setup --plain --runtime "$RUNTIME" \
     --name "$SECOND" --port "$SECOND_PORT" \
     --memory 2g --shm-size 1024m
   ```

2. Require HTTP 200 from both ports and confirm `--json list` and both named
   status commands identify the correct container, port, image, and volumes.
   Write different 0640 and 0600 sentinel files into the second instance's home
   and state volumes, then record hashes and owners.

   ```sh
   podman exec "$SECOND" sh -c \
     'printf %s "$1" > /home/omnideck/second-home-sentinel; chmod 640 /home/omnideck/second-home-sentinel' \
     sh "second-home-${RUN_ID}"
   podman exec "$SECOND" sh -c \
     'printf %s "$1" > /var/lib/omnideck/second-state-sentinel; chmod 600 /var/lib/omnideck/second-state-sentinel' \
     sh "second-state-${RUN_ID}"
   podman exec "$SECOND" sh -c \
     'sha256sum /home/omnideck/second-home-sentinel /var/lib/omnideck/second-state-sentinel; stat -c "%a %u:%g %n" /home/omnideck/second-home-sentinel /var/lib/omnideck/second-state-sentinel'
   ```

3. Remove only the second container/configuration while keeping its volumes:

   ```sh
   "$CANDIDATE_CLI" remove "$SECOND" --plain --yes --keep-volumes
   ```

   Confirm its container and config are gone, both second volumes still exist,
   the primary remains HTTP 200, and the primary hashes are unchanged.

4. Repeat the second-instance setup with the same name and port. Confirm the
   kept volumes are reattached and their exact sentinel hashes and modes return.

## Backup, delete, and restore

1. Remove the second instance with machine-readable backup and explicit volume
   deletion:

   ```sh
   "$CANDIDATE_CLI" --json remove "$SECOND" \
     --yes --delete-volumes --backup
   ```

   Require complete NDJSON stages and save `result.backupPath`. Confirm the
   second container, config, and both volumes are gone while the primary remains
   healthy and unchanged.

2. Inspect the backup rather than trusting archive creation alone:

   ```sh
   BACKUP_PATH=/absolute/path/from/result.backupPath
   RESTORE_WORK="$(mktemp -d)"
   tar -tzf "$BACKUP_PATH"
   tar -xzf "$BACKUP_PATH" -C "$RESTORE_WORK"
   tar -tf "$RESTORE_WORK/home.tar"
   tar -tf "$RESTORE_WORK/state.tar"
   ```

   Require the outer archive to contain `home.tar` and `state.tar`. Require the
   nested exports to contain the expected sentinels with recorded modes and
   values.

3. On Linux with Podman, restore into fresh, test-only volumes and attach an
   isolated fixture. Equivalent native commands are acceptable on Windows and
   macOS, but the same assertions are required.

   ```sh
   RESTORE_HOME="${SECOND}-restore-home-${RUN_ID}"
   RESTORE_STATE="${SECOND}-restore-state-${RUN_ID}"
   RESTORE_FIXTURE="${SECOND}-restore-fixture-${RUN_ID}"
   podman volume create "$RESTORE_HOME"
   podman volume create "$RESTORE_STATE"
   podman run --rm --entrypoint sh \
     -v "${RESTORE_HOME}:/restore" -v "${RESTORE_WORK}:/backup:ro" \
     docker.io/library/busybox:1.36 -c 'cd /restore && tar -xf /backup/home.tar'
   podman run --rm --entrypoint sh \
     -v "${RESTORE_STATE}:/restore" -v "${RESTORE_WORK}:/backup:ro" \
     docker.io/library/busybox:1.36 -c 'cd /restore && tar -xf /backup/state.tar'
   podman run -d --name "$RESTORE_FIXTURE" --entrypoint sh \
     -v "${RESTORE_HOME}:/home/omnideck" \
     -v "${RESTORE_STATE}:/var/lib/omnideck" \
     docker.io/library/busybox:1.36 -c 'sleep 300'
   podman exec "$RESTORE_FIXTURE" sh -c \
     'sha256sum /home/omnideck/second-home-sentinel /var/lib/omnideck/second-state-sentinel; stat -c "%a %u:%g %n" /home/omnideck/second-home-sentinel /var/lib/omnideck/second-state-sentinel'
   ```

   Compare restored hashes, values, modes, and owners with the pre-backup
   evidence. A successful `tar -tf` without a successful restore is not a pass.

4. Remove the fixture and fresh restore volumes by their exact recorded names.
   Remove BusyBox only if this test introduced it. Delete `RESTORE_WORK` only
   after confirming it is the directory returned by `mktemp`.

   ```sh
   podman rm -f "$RESTORE_FIXTURE"
   podman volume rm "$RESTORE_HOME" "$RESTORE_STATE"
   printf '%s\n' "$RESTORE_WORK"
   rm -r -- "$RESTORE_WORK"
   ```

## Image-content caveat

Record the stable and candidate image references, repository digests, and local
image IDs. If the stable and candidate tags produce the same local image ID,
the run still exercises CLI/config migration, container replacement, and data
persistence, but it does **not** exercise an application-image content change.
State that limitation explicitly; do not silently report full image-upgrade
coverage.

## Cleanup and evidence

Save outside the disposable snapshot:

- release and extracted binary hashes, versions, and provenance results;
- before/after host, config, container, image, port, and volume inventories;
- update/lifecycle commands with exit codes and TUI screenshots where used;
- stable, post-migration, and post-update configuration copies;
- sentinel hashes, values, modes, and owners at every checkpoint;
- backup NDJSON, backup archive hash, nested archive listings, and restore
  verification; and
- an explicit result and the image-content caveat outcome.

Prefer reverting the VM snapshot after evidence is copied. Verify the VM is
stopped and the dirty overlay is removed. If a dedicated host must be cleaned
in place, remove only the exact resources recorded by this procedure and prove
the final inventory matches the starting inventory.

## Pass criteria

The candidate recognizes the real stable instance before update; update and
repair preserve both data volumes; hashes and modes survive lifecycle changes;
the migrated configuration and loopback binding match the candidate contract;
named commands remain isolated; keep/delete choices affect only the selected
instance; a generated backup can actually be restored with the recorded data
and metadata; and cleanup changes no unrelated resources.

Report Windows, macOS, and Linux conclusions independently. The overall manual
upgrade gate passes only when all required platform runs pass and any identical
stable/candidate application image is disclosed as a coverage limitation.
