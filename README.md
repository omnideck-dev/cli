# omnideck

[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-7C3AED)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macos-6B7280)](#install)

CLI for installing, managing, and monitoring [Omnideck](https://github.com/omnideck-dev/computron) — a containerized AI assistant. Wraps Docker or Podman with a Bubble Tea TUI and guided install wizard.

---

![screenshot](omnideck-cli.png)
<!-- Replace with an actual recording once available. Suggested tool: vhs (https://github.com/charmbracelet/vhs) -->

---

## Features

- **Guided install wizard** — preflight checks, config form, confirm panel, spinner-driven install steps
- **Smart memory defaults** — suggests container RAM limits based on your system (20% of host RAM, 1–8 GB)
- **Update in place** — pulls the latest image and recreates the container, preserving config
- **Multi-instance** — run more than one Omnideck container on different ports from a single binary
- **Health check** — `doctor` runs parallel checks and prints a full pass/warn/fail report
- **Docker + Podman** — auto-detected; SELinux, volume ownership, and Ollama host differences handled per OS
- **Backup on uninstall** — optionally archives data directories to a `.tar.gz` before removal
- **`--no-color`** — safe to pipe; exits non-zero on actual failures, not on warnings

---

## Requirements

| Engine | Minimum version | Notes |
|--------|----------------|-------|
| Docker | 20.10+ | Required for `host-gateway` host alias (Ollama connectivity) |
| Podman | 4.0+ | Required for `host.containers.internal` and `:U` volume remapping |

Ollama is optional at install time — the wizard warns if it isn't reachable but proceeds anyway.

---

## Install

### Homebrew (macOS / Linux)

```sh
# tap not yet published — build from source in the meantime
```

### Build from source

Requires Go 1.21+ and Docker or Podman.

```sh
git clone https://github.com/omnideck-dev/cli
cd omnideck-cli
go build -ldflags="-s -w" -o omnideck .
sudo mv omnideck /usr/local/bin/
```

### Verify

```sh
omnideck --version
```

---

## Quickstart

```sh
# 1. Install Omnideck (interactive wizard)
omnideck install

# 2. Check everything is healthy
omnideck doctor

# 3. Open the web UI
#    http://localhost:2337
```

The wizard detects your container engine, checks Ollama reachability, suggests memory limits sized for your machine, and starts the container.

---

## Usage

```
omnideck <command> [flags]
```

### Commands

| Command | Description |
|---|---|
| `install` | Interactive TUI wizard — install or update an existing instance |
| `update` | Pull the latest image and recreate the container |
| `start` | Start a stopped container |
| `stop` | Gracefully stop the running container |
| `restart` | Stop then start |
| `status` | Print a status table (container, dirs, Ollama, web UI port) |
| `logs` | Tail container logs |
| `doctor` | Run parallel health checks and print a report |
| `config show` | Pretty-print the saved config |
| `config set <key> <value>` | Update a single config key |
| `config path` | Print the config file path |
| `uninstall` | Stop, remove container, optionally delete data dirs |

### Global flags

```
--config string   Config file path (default: ~/.config/omnideck-cli/config.yaml)
--name string     Instance name (e.g. omnideck, omnideck2)
--no-color        Disable color output
--debug           Print raw engine commands and stderr
--version         Print version and exit
```

### Install flags

```
--image string    Override the container image (for testing alternate builds)
```

### Examples

```sh
# Tail logs and follow
omnideck logs --follow --tail 100

# Manage a specific instance by name
omnideck --name omnideck2 status
omnideck --name omnideck2 stop

# Test an alternate image without changing the default
omnideck install --image ghcr.io/example/omnideck:dev

# Uninstall a specific instance
omnideck --name omnideck2 uninstall
```

### Multiple instances

If Omnideck is already installed, `omnideck install` asks whether to update the existing instance or install a new one. New instances get a unique container name (`omnideck2`, `omnideck3`, …), separate data directories, and an incremented port.

Commands that need an instance (e.g. `start`, `status`) show a picker when more than one instance exists, or accept `--name` to skip the prompt.

---

## Configuration

Config files live at:

```
~/.config/omnideck-cli/instances/<container-name>.yaml
```

```yaml
container_name: omnideck
shared_dir: /home/user/Omnideck
state_dir: /home/user/Omnideck/.state
memory: 3g
shm_size: 1536m
web_ui_port: "2337"
engine: docker
image: ghcr.io/omnideck-dev/omnideck:latest
installed_at: 2025-01-15T10:30:00Z
```

**`shared_dir`** is mounted into the container and visible to you as `~/Omnideck`.  
**`state_dir`** is a hidden subdirectory (`Omnideck/.state`) used for persistent container state — you won't normally interact with it.  
**`memory`** and **`shm_size`** are set by the wizard based on your system RAM and can be adjusted here or during install.

---

## Contributing

1. Fork and clone the repo
2. `go test ./...` — all tests must pass
3. `go vet ./...` — no vet errors
4. Open a PR against `main`

**Engine calls shell out intentionally** — no Docker SDK. Keep the binary dependency-free when adding engine features.

**Platform rules:** never add a Linux-only flag without a `runtime.GOOS` guard. Key differences between Linux and macOS:

| Concern | Linux | macOS |
|---------|-------|-------|
| Volume SELinux flag | `:Z` (Docker), `:Z,U` (Podman) | Omit |
| Volume ownership | `--user UID:GID` (Docker), `:U` (Podman) | Omit |
| Ollama host | `host-gateway` (Docker), `host.containers.internal` (Podman) | `host.docker.internal` |

See `CLAUDE.md` for the full platform table and architecture notes.

---

## License

MIT © [rlnorthcutt](https://github.com/rlnorthcutt)
