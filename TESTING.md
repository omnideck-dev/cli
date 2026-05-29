# omnideck — Testing & Open Questions

This file tracks known assumptions, untested configurations, and open questions
to validate before a broader release. Items marked ✅ have been verified.

---

## 1. Container Internal Port

**Assumption:** The container app listens on port 2337 internally (or respects
the `PORT=2337` env var we set).

**Risk:** If the image binds to a different port (e.g., 3000, 8000), the
`-p HOST:2337` mapping silently fails — the container starts but the web UI is
unreachable on any host port.

**To test:**
- `docker inspect --format '{{json .Config.ExposedPorts}}' <container>` — check
  which ports the image declares
- Run and confirm `http://localhost:2337` loads
- Check whether the app logs show it reading the `PORT` env var

---

## 2. Multi-Instance (Two Omnideck Installs)

**To test:**
- Install a second instance (`omnideck install`), confirm it picks port 2338
- Both containers running simultaneously — confirm port 2337 and 2338 both load
  correct web UIs
- `omnideck status` / `omnideck --name omnideck2 status` — correct instance shown
- `omnideck stop` / `omnideck --name omnideck2 stop` — stops correct instance only
- `omnideck uninstall` with two instances — picker appears, correct one removed

---

## 3. File Ownership / Uninstall

**Approach:** Docker uses `--user UID:GID`; Podman uses `:U` on volume mounts.
Both should cause container-written files to be owned by the host user.

**To test:**
- After install and running, check ownership: `ls -la ~/Omnideck/`
- Files should be owned by the current user, not root or an unknown UID
- Uninstall → delete data dirs → should succeed without `sudo`
- Test the backup path (answer yes to backup prompt) — tar.gz created and complete
- Test with a container that has written files before uninstall (not just empty dirs)

**Open question — Docker `--user` compatibility:**
- Does the container app function correctly when run as the host user (UID/GID)?
  Some images set up `/home`, permissions, etc. for a specific internal UID.
  If the app breaks under `--user`, this flag needs to be removed and the
  `sudo rm -rf` fallback message is the only option.

**Open question — Podman `:U` in rootful mode:**
- `:U` is designed for rootless Podman user namespace remapping. In rootful
  Podman (running as root), the behavior may differ or have no effect.

---

## 4. Engine × OS Matrix

Combinations to validate end-to-end (install → use → uninstall):

| Engine          | OS              | Notes                                      |
|-----------------|-----------------|---------------------------------------------|
| Docker          | Linux           | Primary target. `host-gateway`, `--user`    |
| Podman rootless | Linux (Fedora)  | Primary target. `:Z,U`, `host.containers.internal` |
| Podman rootless | Linux (Ubuntu)  | Podman version may differ                  |
| Podman rootful  | Linux           | `:U` behavior unclear; `--user` not used   |
| Docker Desktop  | macOS           | Volume ownership handled by Desktop        |
| Podman Desktop  | macOS           | `host.docker.internal` may not be auto-set |

---

## 5. Docker Version Requirements

**`--add-host=host-gateway:host-gateway`** was introduced in Docker 20.10.
On older Docker, `docker run` fails with an unrecognised host entry error.

**To test:**
- Check `docker version` and confirm ≥ 20.10 before using `host-gateway`
- Consider adding a preflight version check in the doctor command

**Mitigation if needed:** Fall back to getting the docker bridge IP dynamically
(`docker network inspect bridge --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}'`)
and using that as a literal IP address instead of `host-gateway`.

---

## 6. Podman Version Requirements

**`host.containers.internal`** is automatically injected into container
`/etc/hosts` starting in Podman 4.0.

**`:U` volume flag** requires Podman 4.0+.

**Risk:** RHEL 8 ships Podman 3.x. Ubuntu 20.04 LTS ships an older version.
On these systems, Ollama would be silently unreachable and `:U` would error.

**To test:**
- `podman --version` — confirm ≥ 4.0
- On a Podman 3.x system (VM / container), verify what actually happens
- Consider adding a preflight version check in the doctor command

---

## 7. Networking — Ollama Connectivity

With the switch from `--network host` to bridge networking, Ollama access routes
through a host alias instead of the loopback.

**Docker/Linux:** `host-gateway` → resolves to host IP (requires Docker 20.10+)
**Podman/Linux:** `host.containers.internal` → auto-injected by Podman 4.0+
**macOS:** `host.docker.internal` → provided by Docker Desktop / needs verification on Podman Desktop

**To test:**
- Install with Ollama running → confirm the web UI can use Ollama models
- Install with Ollama NOT running → confirm the install succeeds with the
  expected warning, then start Ollama after and confirm it connects
- Check container logs for Ollama connection errors: `omnideck logs`

---

## 8. SELinux (RHEL / Fedora)

`:Z` on volume mounts relabels the directory for SELinux access. Without it,
the container can't read or write the mounted path on SELinux-enforcing systems.

**To test:**
- Install on Fedora/RHEL with SELinux enforcing
- Confirm no AVC denial in `journalctl -xe` or `ausearch -m avc`
- With the `:U` flag combined (`:Z,U`), confirm both flags apply correctly

---

## 9. macOS — Docker Desktop Port Behaviour

macOS `--network host` behaves differently in Docker Desktop (the container runs
in a Linux VM). The SPEC originally noted this as limited. Now that we use bridge
networking with explicit `-p` mapping, this should be more reliable.

**To test:**
- Install on macOS Docker Desktop → confirm port 2337 loads
- Second instance on port 2338 → confirm both load
- Confirm Ollama at `host.docker.internal:11434` is reachable from the container

---

## 10. Memory / SHM Defaults

The formula `M = max(1, min(floor(0.2 × HostRAM_GB), 8))` is applied at install
time. SHM = 50% of M.

**To test:**
- On a machine with < 5 GB RAM — confirm minimum 1g is suggested
- On a machine with 64+ GB RAM — confirm maximum 8g is capped
- Confirm the container actually starts with the specified memory limit
  (`docker inspect --format '{{.HostConfig.Memory}}' <container>`)
- Test that the user can override the default in the TUI and it takes effect

---

## 11. Uninstall — StateDir Inside SharedDir

`StateDir` is now `SharedDir/.state`. Deleting `SharedDir` in the uninstall loop
removes `.state` at the same time. The second loop iteration for `StateDir` will
hit `IsNotExist` and show "(already removed)".

**To test:**
- Confirm uninstall shows check for SharedDir and "(already removed)" for StateDir
- Confirm the resulting "(already removed)" warning is not alarming to users
  (may want to suppress it or skip StateDir if it's a subpath of SharedDir)

---

## 12. Backup Archive

`omnideck uninstall` optionally creates a `.tar.gz` backup before deletion.

**To test:**
- Answer yes to backup prompt — verify archive created in home directory
- Verify archive contains `shared/` and `state/` prefixes
- Verify archive can be extracted: `tar xzf omnideck-backup-*.tar.gz`
- Test with symlinks in the shared directory (code handles them but untested)
- Test with very large shared directories — does it block the terminal?
