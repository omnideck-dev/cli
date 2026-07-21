# omnideck — Testing & Open Questions

This file tracks known assumptions, untested configurations, and open questions
to validate before a broader release. Items marked ✅ have been verified.

---

## 1. Container Internal Port

**Current behavior:** The container app listens on port 8080 internally. The CLI
maps each instance's chosen host port (2337, 2338, …) to container port 8080.

**Risk:** If the image binds to a different port (e.g., 3000, 8000), the
`-p HOST:8080` mapping silently fails — the container starts but the web UI is
unreachable on any host port.

**To test:**
- `docker inspect --format '{{json .Config.ExposedPorts}}' <container>` — check
  which ports the image declares
- Run and confirm `http://localhost:2337` loads
- Check whether the app logs show it reading the `PORT` env var

---

## 2. Multi-Instance (Two Omnideck Installs)

**To test:**
- Set up a second instance (`omnideck setup`), confirm it picks port 2338
- Both containers running simultaneously — confirm port 2337 and 2338 both load
  correct web UIs
- `omnideck status` / `omnideck --name omnideck2 status` — correct instance shown
- `omnideck stop` / `omnideck --name omnideck2 stop` — stops correct instance only
- `omnideck uninstall` with two instances — picker appears, correct one removed

---

## 3. Named Volume Persistence / Uninstall

**Approach:** Docker and Podman use named volumes for `/home/omnideck` and
`/var/lib/omnideck`, so host filesystem ownership no longer affects uninstall.

**To test:**
- After install, inspect mounts and confirm `Type:"volume"`, not `Type:"bind"`
- Confirm volumes are named `{container}-home` and `{container}-state`
- Uninstall → delete data volumes → should succeed without `sudo`
- Test the backup path (answer yes to backup prompt) — tar.gz created and complete
- Test with a container that has written files before uninstall (not just empty volumes)

---

## 4. Engine × OS Matrix

Combinations to validate end-to-end (install → use → uninstall):

| Engine          | OS              | Notes                                      |
|-----------------|-----------------|---------------------------------------------|
| Docker          | Linux           | Primary target. `OLLAMA_HOST=http://host-gateway:11434` |
| Podman rootless | Linux (Fedora)  | Primary target. Built-in host aliases      |
| Podman rootless | Linux (Ubuntu)  | Podman version may differ                  |
| Podman rootful  | Linux           | Named volume behavior should match rootless |
| Docker Desktop  | macOS           | Volume ownership handled by Desktop        |
| Podman          | macOS           | Host aliases resolved by Podman machine DNS |

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

**Policy:** Do not reject or warn about Podman based only on its major version.
Podman 3.4 already documents `host.containers.internal`, while current Podman
documentation says the address can still be skipped when Podman cannot determine
the route back to the host. Version alone is not a reliable connectivity test.

**Risk:** RHEL 8 ships Podman 3.x. Ubuntu 20.04 LTS ships an older version.
On these systems, Ollama may be silently unreachable.

**To test:**
- On a Podman 3.x system (VM / container), verify installation and cloud AI use
- Verify local Ollama connectivity from inside the installed Omnideck container
- Confirm the wizard neither blocks nor warns based only on Podman's version

---

## 6a. Guided Runtime Bootstrap

Validate the new no-runtime and repair paths before promoting the preview:

| Host | Initial state | Expected default/action |
|---|---|---|
| Ubuntu/Debian | Neither installed | Recommend Podman; run `apt-get update` then `apt-get install -y podman` with narrow `sudo` |
| Fedora/RHEL | Neither installed | Recommend Podman via `dnf` |
| Linux | Docker socket permission denied | Explain that Docker access can give apps full control of the computer; do not change account groups |
| macOS | Podman installed, no machine | Run `podman machine init --now --update-connection=true`, then continue without a technical connection prompt |
| macOS | Docker Desktop stopped | Launch app, wait, then recheck |
| Windows | Neither installed | Recommend Docker Desktop/WSL 2; show the official Podman installer as an alternative |
| Windows | Podman machine stopped | Run `podman machine start`, then continue |
| WSL 2 | Neither usable | Recommend Windows Docker Desktop and WSL integration |
| Any | Runtime already healthy | Skip runtime setup entirely |

For every case, verify:

- The reason Omnideck needs a runtime is visible before any setup action.
- Every command is available under **technical details** before execution and uses direct arguments, not a shell pipeline.
- Omnideck itself runs as the current user. Only the computer's built-in software or background-app tool may ask for the user's account password.
- `--plain` prints guidance and exits non-zero without modifying the host.
- The first setup honors `--engine docker` or `--engine podman`; later setups keep the saved machine-wide runtime.

---

## 7. Networking — Ollama Connectivity

With the switch from `--network host` to bridge networking, Ollama access routes
through a host alias instead of the loopback.

The CLI always sets `OLLAMA_HOST` inside the Omnideck container. The app should
read that env var instead of hardcoding a host name.

**Docker/Linux:** `OLLAMA_HOST=http://host-gateway:11434` (requires Docker 20.10+)
**Podman/Linux:** `OLLAMA_HOST=http://host.containers.internal:11434`
**macOS/Windows Docker:** `OLLAMA_HOST=http://host.docker.internal:11434`

**To test:**
- Install with Ollama running → confirm the web UI can use Ollama models
- Install with Ollama NOT running → confirm the install succeeds with the
  expected warning, then start Ollama after and confirm it connects
- Check container logs for Ollama connection errors: `omnideck logs`

---

## 8. SELinux (RHEL / Fedora)

Named volumes do not require bind-mount relabel flags.

**To test:**
- Install on Fedora/RHEL with SELinux enforcing
- Confirm no AVC denial in `journalctl -xe` or `ausearch -m avc`

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

## 11. Uninstall — Named Volumes

`omnideck uninstall` removes Docker/Podman named volumes when the user confirms
data deletion. Host bind-mount directories are no longer removed by the CLI.

**To test:**
- Confirm uninstall prompts to delete `{container}-home` and `{container}-state`
- Confirm declining the prompt preserves both volumes
- Confirm accepting the prompt removes both volumes

---

## 12. Backup Archive

`omnideck uninstall` optionally creates a `.tar.gz` backup before deleting
volumes. The archive contains native volume exports as `home.tar` and
`state.tar`.

**To test:**
- Answer yes to backup prompt — verify archive created in home directory
- Verify archive contains `home.tar` and `state.tar`
- Verify archive can be extracted: `tar xzf omnideck-backup-*.tar.gz`
- Verify a nested export can be inspected with `tar tf home.tar`
- Test with very large shared directories — does it block the terminal?
